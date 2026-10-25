package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{Name: binaryName, Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExecuteReplacesOnlyAfterChecksumMatches(t *testing.T) {
	newBinary := []byte("new executable contents")
	archive := testArchive(t, newBinary)
	hash := sha256.Sum256(archive)
	archiveName := "rh-sc-mcp_darwin_arm64.tar.gz"
	var downloads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.3.0",
				"assets":   []map[string]string{{"name": archiveName}, {"name": "checksums.txt"}},
			})
		case "/download/v0.3.0/checksums.txt":
			downloads++
			_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(hash[:]), archiveName)
		case "/download/v0.3.0/" + archiveName:
			downloads++
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(target, []byte("old executable contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	u := &Updater{
		Client:          server.Client(),
		CurrentVersion:  "0.2.0",
		GOOS:            "darwin",
		GOARCH:          "arm64",
		ExecutablePath:  target,
		LatestURL:       server.URL + "/latest",
		DownloadBaseURL: server.URL + "/download",
		Output:          &out,
	}
	if err := u.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, newBinary) {
		t.Fatalf("updated executable: content=%q, err=%v", got, err)
	}
	if downloads != 2 || !strings.Contains(out.String(), "v0.3.0") {
		t.Fatalf("unexpected downloads/output: count=%d, output=%q", downloads, out.String())
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("updated executable mode: info=%v, err=%v", info, err)
	}
}

func TestExecuteRejectsChangedArchiveAndPreservesExecutable(t *testing.T) {
	archiveName := "rh-sc-mcp_linux_amd64.tar.gz"
	archive := testArchive(t, []byte("unexpected binary"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.3.0",
				"assets":   []map[string]string{{"name": archiveName}, {"name": "checksums.txt"}},
			})
		case "/download/v0.3.0/checksums.txt":
			_, _ = fmt.Fprintf(w, "%064x  %s\n", 0, archiveName)
		case "/download/v0.3.0/" + archiveName:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), binaryName)
	const oldBinary = "keep this binary"
	if err := os.WriteFile(target, []byte(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	u := &Updater{
		Client: server.Client(), CurrentVersion: "0.2.0", GOOS: "linux", GOARCH: "amd64",
		ExecutablePath: target, LatestURL: server.URL + "/latest",
		DownloadBaseURL: server.URL + "/download", Output: &bytes.Buffer{},
	}
	if err := u.Execute(context.Background()); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("checksum failure was not reported: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != oldBinary {
		t.Fatalf("original executable changed: content=%q, err=%v", got, err)
	}
}

func TestExecuteDoesNotDownloadWhenAlreadyCurrent(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/latest" {
			t.Errorf("unexpected download of %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"tag_name":"v0.2.0","assets":[]}`)
	}))
	defer server.Close()
	var out bytes.Buffer
	u := &Updater{Client: server.Client(), CurrentVersion: "0.2.0", GOOS: "linux", GOARCH: "amd64", LatestURL: server.URL + "/latest", Output: &out}
	if err := u.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !strings.Contains(out.String(), "up to date") {
		t.Fatalf("unexpected update check: requests=%d, output=%q", requests, out.String())
	}
}

func TestExecuteRejectsUnsupportedPlatformBeforeRequest(t *testing.T) {
	u := &Updater{GOOS: "windows", GOARCH: "amd64"}
	if err := u.Execute(context.Background()); err == nil || !strings.Contains(err.Error(), "go install") {
		t.Fatalf("unsupported OS should give an install fallback: %v", err)
	}
}

func TestChecksumAndVersionValidation(t *testing.T) {
	if _, err := checksumFor([]byte("bad  rh-sc-mcp_linux_amd64.tar.gz\n"), "rh-sc-mcp_linux_amd64.tar.gz"); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
	if _, err := checksumFor(nil, "rh-sc-mcp_linux_amd64.tar.gz"); err == nil {
		t.Fatal("missing checksum was accepted")
	}
	for _, tc := range []struct {
		latest, current string
		want            int
	}{
		{"v0.3.0", "0.2.9", 1},
		{"v0.2.0", "0.2.0", 0},
		{"v0.1.9", "0.2.0", -1},
	} {
		got, err := compareVersions(tc.latest, tc.current)
		if err != nil || got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, %v", tc.latest, tc.current, got, err)
		}
	}
	if _, err := compareVersions("v0.3.0/../../foo", "0.2.0"); err == nil {
		t.Fatal("unsafe release tag was accepted")
	}
}

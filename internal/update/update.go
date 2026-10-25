// Package update replaces the running binary with a verified GitHub Release.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	binaryName       = "rh-sc-mcp"
	latestReleaseURL = "https://api.github.com/repos/konono/rh-sc-mcp/releases/latest"
	releaseBaseURL   = "https://github.com/konono/rh-sc-mcp/releases/download"
	maxReleaseJSON   = 2 << 20
	maxChecksums     = 1 << 20
	maxArchive       = 100 << 20
	maxBinary        = 80 << 20
)

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// Updater holds the inputs to a single update. URL and executable overrides
// let tests exercise the full workflow without touching GitHub or this binary.
type Updater struct {
	Client          httpClient
	CurrentVersion  string
	GOOS            string
	GOARCH          string
	ExecutablePath  string
	LatestURL       string
	DownloadBaseURL string
	Output          io.Writer
}

// Run checks the latest public release and replaces the currently running
// executable if its version is newer. It needs no Red Hat credential or config.
func Run(ctx context.Context, currentVersion string, output io.Writer) error {
	u := &Updater{
		Client:          &http.Client{Timeout: 2 * time.Minute},
		CurrentVersion:  currentVersion,
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		LatestURL:       latestReleaseURL,
		DownloadBaseURL: releaseBaseURL,
		Output:          output,
	}
	return u.Execute(ctx)
}

// Execute runs the update, leaving the old binary untouched until the final
// atomic rename. Only the four OS/architecture pairs shipped by GoReleaser
// are supported.
func (u *Updater) Execute(ctx context.Context) error {
	if u.Output == nil {
		u.Output = io.Discard
	}
	if (u.GOOS != "linux" && u.GOOS != "darwin") || (u.GOARCH != "amd64" && u.GOARCH != "arm64") {
		return fmt.Errorf("no release binary for %s/%s; use go install to update", u.GOOS, u.GOARCH)
	}
	if u.Client == nil {
		return errors.New("update HTTP client is not configured")
	}

	data, err := u.get(ctx, u.LatestURL, maxReleaseJSON)
	if err != nil {
		return fmt.Errorf("check latest release: %w", err)
	}
	var latest release
	if err := json.Unmarshal(data, &latest); err != nil {
		return fmt.Errorf("decode latest release: %w", err)
	}
	comparison, err := compareVersions(latest.TagName, u.CurrentVersion)
	if err != nil {
		return fmt.Errorf("compare release versions: %w", err)
	}
	if comparison <= 0 {
		_, _ = fmt.Fprintf(u.Output, "%s %s is up to date (latest release: %s).\n", binaryName, u.CurrentVersion, latest.TagName)
		return nil
	}

	archiveName := fmt.Sprintf("%s_%s_%s.tar.gz", binaryName, u.GOOS, u.GOARCH)
	if !latest.hasAsset(archiveName) || !latest.hasAsset("checksums.txt") {
		return fmt.Errorf("release %s lacks %s or checksums.txt", latest.TagName, archiveName)
	}
	base := strings.TrimRight(u.DownloadBaseURL, "/") + "/" + latest.TagName + "/"
	checksums, err := u.get(ctx, base+"checksums.txt", maxChecksums)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	wantHash, err := checksumFor(checksums, archiveName)
	if err != nil {
		return err
	}
	archive, err := u.get(ctx, base+archiveName, maxArchive)
	if err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}
	gotHash := sha256.Sum256(archive)
	if gotHash != wantHash {
		return fmt.Errorf("SHA-256 mismatch for %s; executable was not changed", archiveName)
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return fmt.Errorf("extract release binary: %w", err)
	}
	path, err := u.executablePath()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	if err := replaceBinary(path, binary); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	_, _ = fmt.Fprintf(u.Output, "Updated %s from %s to %s at %s. Restart MCP clients to use it.\n", binaryName, u.CurrentVersion, latest.TagName, path)
	return nil
}

func (r release) hasAsset(name string) bool {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return true
		}
	}
	return false
}

func (u *Updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if url == u.LatestURL {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned HTTP %d", req.URL.Host+req.URL.Path, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", req.URL.Host+req.URL.Path, limit)
	}
	return data, nil
}

func checksumFor(data []byte, name string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	var found bool
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != name {
			continue
		}
		if found {
			return digest, fmt.Errorf("duplicate checksum for %s", name)
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return digest, fmt.Errorf("invalid SHA-256 checksum for %s", name)
		}
		copy(digest[:], decoded)
		found = true
	}
	if !found {
		return digest, fmt.Errorf("missing SHA-256 checksum for %s", name)
	}
	return digest, nil
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in archive", binaryName)
		}
		if err != nil {
			return nil, err
		}
		if header.Name != binaryName {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("%s is not a regular file", binaryName)
		}
		if header.Size <= 0 || header.Size > maxBinary {
			return nil, fmt.Errorf("%s has an invalid size", binaryName)
		}
		data, err := io.ReadAll(io.LimitReader(tarReader, maxBinary+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != header.Size {
			return nil, fmt.Errorf("%s is truncated", binaryName)
		}
		return data, nil
	}
}

func (u *Updater) executablePath() (string, error) {
	if u.ExecutablePath != "" {
		return u.ExecutablePath, nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func replaceBinary(path string, binary []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("current executable is not a regular file")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".rh-sc-mcp-update-*")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := file.Write(binary); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func compareVersions(latest, current string) (int, error) {
	left, err := parseVersion(latest)
	if err != nil {
		return 0, fmt.Errorf("latest release: %w", err)
	}
	right, err := parseVersion(current)
	if err != nil {
		return 0, fmt.Errorf("current binary: %w", err)
	}
	for i := range left {
		if left[i] > right[i] {
			return 1, nil
		}
		if left[i] < right[i] {
			return -1, nil
		}
	}
	return 0, nil
}

func parseVersion(value string) ([3]int, error) {
	var version [3]int
	text := strings.TrimPrefix(value, "v")
	parts := strings.Split(text, ".")
	if len(parts) != 3 {
		return version, fmt.Errorf("unsupported version %q (want X.Y.Z)", value)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || strconv.Itoa(n) != part {
			return version, fmt.Errorf("unsupported version %q (want X.Y.Z)", value)
		}
		version[i] = n
	}
	return version, nil
}

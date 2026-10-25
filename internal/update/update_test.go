package update

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The tests drive a real `go install` rather than a stub. The thing
// worth checking is that this code agrees with the toolchain about
// where a binary lands, and a fake toolchain cannot disagree with it.
// A throwaway module stands in for the published one, so nothing here
// touches the network.

func goBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	return path
}

// fixtureModule writes a module whose program prints a version line in
// the same shape as the real binary, and returns its directory.
func fixtureModule(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/rh-sc-mcp\n\ngo 1.25\n")
	write("main.go", `package main

import "fmt"

func main() { fmt.Println("rh-sc-mcp `+version+`") }
`)
	return dir
}

// installEnv points the toolchain at a scratch GOBIN and module cache.
func installEnv(t *testing.T, gobin string) []string {
	t.Helper()
	env := append(os.Environ(),
		"GOBIN="+gobin,
		"GOFLAGS=",
		"GOPATH="+t.TempDir(),
		"GOCACHE="+t.TempDir(),
	)
	return env
}

// installFixture builds the fixture into gobin under the real binary
// name, standing in for a copy installed earlier.
func installFixture(t *testing.T, dir, gobin string, env []string) string {
	t.Helper()
	target := filepath.Join(gobin, binaryName)
	cmd := exec.Command(goBin(t), "build", "-o", target, ".")
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fixture: %v\n%s", err, out)
	}
	return target
}

// ---- the toolchain path -----------------------------------------------

// The whole design rests on this code and `go install` agreeing on the
// destination, so the test installs for real and compares.
func TestExecuteInstallsOverTheRunningBinary(t *testing.T) {
	gobin := t.TempDir()
	env := installEnv(t, gobin)
	dir := fixtureModule(t, "0.9.0")
	target := installFixture(t, dir, gobin, env)

	var out bytes.Buffer
	u := &Updater{
		GoBin:          goBin(t),
		InstallSpec:    ".",
		CurrentVersion: "0.2.0",
		ExecutablePath: target,
		Env:            env,
		Output:         &out,
	}
	// `go install .` is resolved against the module directory.
	u.Env = append(u.Env, "PWD="+dir)
	if err := runIn(t, dir, u); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "0.9.0") {
		t.Errorf("the new version was not reported: %s", out.String())
	}
	if !strings.Contains(out.String(), target) {
		t.Errorf("the install location was not reported: %s", out.String())
	}
	// The binary at the target really is the newly installed one.
	got, err := exec.Command(target, "--version").Output()
	if err != nil {
		t.Fatalf("installed binary does not run: %v", err)
	}
	if !strings.Contains(string(got), "0.9.0") {
		t.Errorf("installed binary reports %q", got)
	}
}

// Installing something that is already current must not claim an
// upgrade happened.
func TestExecuteReportsNoChangeWhenAlreadyCurrent(t *testing.T) {
	gobin := t.TempDir()
	env := installEnv(t, gobin)
	dir := fixtureModule(t, "0.2.0")
	target := installFixture(t, dir, gobin, env)

	var out bytes.Buffer
	u := &Updater{
		GoBin: goBin(t), InstallSpec: ".", CurrentVersion: "0.2.0",
		ExecutablePath: target, Env: env, Output: &out,
	}
	if err := runIn(t, dir, u); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "already at 0.2.0") {
		t.Errorf("an unchanged version was reported as an update: %s", out.String())
	}
	if strings.Contains(out.String(), "Updated") {
		t.Errorf("claimed an update with no version change: %s", out.String())
	}
}

// ---- destination mismatch ---------------------------------------------

// The case the check exists for: the binary being run is not the one
// `go install` would overwrite. Nothing may be installed, and nothing
// may read as success.
func TestExecuteRefusesWhenInstallTargetIsNotTheRunningBinary(t *testing.T) {
	gobin := t.TempDir()
	env := installEnv(t, gobin)
	dir := fixtureModule(t, "0.9.0")

	// A copy somewhere else entirely, as if it had been moved to
	// /usr/local/bin.
	elsewhere := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(elsewhere, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	u := &Updater{
		GoBin: goBin(t), InstallSpec: ".", CurrentVersion: "0.2.0",
		ExecutablePath: elsewhere, Env: env, Output: &out,
	}
	err := runIn(t, dir, u)
	if err == nil {
		t.Fatal("an update targeting a different file was allowed")
	}
	var mismatch *MismatchError
	if !errorAs(err, &mismatch) {
		t.Fatalf("got %T, want *MismatchError: %v", err, err)
	}
	// Both paths and a way forward.
	for _, want := range []string{gobin, elsewhere, "go install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(out.String(), "Updated") {
		t.Errorf("a refusal printed success: %s", out.String())
	}
	// And it refused before installing anything.
	if _, statErr := os.Stat(filepath.Join(gobin, binaryName)); statErr == nil {
		t.Error("the refusal still installed a binary")
	}
}

// A symlink to the installed binary is the same file, so updating
// through it is fine and must not be refused.
func TestExecuteAcceptsASymlinkToTheInstalledBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows, which is not a release target")
	}
	gobin := t.TempDir()
	env := installEnv(t, gobin)
	dir := fixtureModule(t, "0.9.0")
	target := installFixture(t, dir, gobin, env)

	link := filepath.Join(t.TempDir(), binaryName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	u := &Updater{
		GoBin: goBin(t), InstallSpec: ".", CurrentVersion: "0.2.0",
		ExecutablePath: link, Env: env, Output: &bytes.Buffer{},
	}
	if err := runIn(t, dir, u); err != nil {
		t.Fatalf("updating through a symlink was refused: %v", err)
	}
}

// A hard link is the same inode as the installed binary but not the
// same path, and `go install` replaces its target by renaming a new
// file over it — measured: the installed path gets a fresh inode while
// the other link keeps the old one. So the running copy would be left
// on the previous binary while this reported success. It must refuse.
func TestExecuteRefusesAHardLinkToTheInstalledBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard link semantics differ on windows, which is not a release target")
	}
	gobin := t.TempDir()
	env := installEnv(t, gobin)
	dir := fixtureModule(t, "0.9.0")
	target := installFixture(t, dir, gobin, env)

	link := filepath.Join(t.TempDir(), binaryName)
	if err := os.Link(target, link); err != nil {
		t.Skipf("cannot create a hard link here: %v", err)
	}
	// Same inode, which is what makes os.SameFile the wrong test.
	ti, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	li, err := os.Stat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(ti, li) {
		t.Fatal("the fixture is not actually a hard link")
	}

	var out bytes.Buffer
	u := &Updater{
		GoBin: goBin(t), InstallSpec: ".", CurrentVersion: "0.2.0",
		ExecutablePath: link, Env: env, Output: &out,
	}
	err = runIn(t, dir, u)
	var mismatch *MismatchError
	if err == nil || !errorAs(err, &mismatch) {
		t.Fatalf("a hard link was accepted as the install target: %v", err)
	}
	if strings.Contains(out.String(), "Updated") {
		t.Errorf("a refusal printed success: %s", out.String())
	}
}

// ---- version reporting -------------------------------------------------

// @latest can resolve backwards, and by the time that is visible the
// binary has been replaced. A downgrade must not read as an update.
func TestExecuteReportsADowngradeAsAFailure(t *testing.T) {
	gobin := t.TempDir()
	env := installEnv(t, gobin)
	dir := fixtureModule(t, "0.1.0") // older than the running 0.2.0
	target := installFixture(t, dir, gobin, env)

	var out bytes.Buffer
	u := &Updater{
		GoBin: goBin(t), InstallSpec: ".", CurrentVersion: "0.2.0",
		ExecutablePath: target, Env: env, Output: &out,
	}
	err := runIn(t, dir, u)
	if err == nil {
		t.Fatal("a downgrade was reported as success")
	}
	for _, want := range []string{"OLDER", "0.1.0", "0.2.0", "already been replaced"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(out.String(), "Updated") {
		t.Errorf("a downgrade printed an update line: %s", out.String())
	}
}

// Anything but "rh-sc-mcp X.Y.Z" must not be mined for a version.
func TestInstalledVersionRejectsUnexpectedOutput(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"empty":        "",
		"no name":      "0.3.0",
		"wrong name":   "something-else 0.3.0",
		"extra words":  "rh-sc-mcp 0.3.0 (dirty)",
		"two lines":    "warning: ignore me\nrh-sc-mcp 0.3.0",
		"error output": "could not read config; see rh-sc-mcp 0.0.1",
	} {
		t.Run(name, func(t *testing.T) {
			script := filepath.Join(dir, "p-"+strings.ReplaceAll(name, " ", "_"))
			if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \""+body+"\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			u := &Updater{Env: os.Environ()}
			got, err := u.installedVersion(context.Background(), script)
			if err == nil {
				t.Fatalf("accepted %q as version %q", body, got)
			}
		})
	}

	ok := filepath.Join(dir, "good")
	if err := os.WriteFile(ok, []byte("#!/bin/sh\necho 'rh-sc-mcp 1.2.3'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := &Updater{Env: os.Environ()}
	got, err := u.installedVersion(context.Background(), ok)
	if err != nil || got != "1.2.3" {
		t.Fatalf("installedVersion = %q, %v; want 1.2.3", got, err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.2.0", 1},
		{"0.2.0", "0.3.0", -1},
		{"0.2.0", "0.2.0", 0},
		{"v1.0.0", "0.9.9", 1},
		{"0.2.10", "0.2.9", 1},
	} {
		got, err := compare(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("compare(%q,%q) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "1.2", "1.2.3.4", "a.b.c", "01.2.3", "1.2.-3"} {
		if _, err := compare(bad, "0.1.0"); err == nil {
			t.Errorf("compare accepted %q", bad)
		}
	}
}

// ---- toolchain missing -------------------------------------------------

// Install and update both need Go now, so its absence has to say so and
// give the command to run after installing it.
func TestExecuteExplainsAMissingToolchain(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	u := &Updater{CurrentVersion: "0.2.0", Output: &bytes.Buffer{}}
	err := u.Execute(context.Background())
	if err == nil {
		t.Fatal("a missing go toolchain was not reported")
	}
	for _, want := range []string{"Go toolchain is required", "go.dev/dl", installSpec} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// ---- install failure ---------------------------------------------------

// A failing build must surface the toolchain's own output and must not
// report an update.
func TestExecuteSurfacesInstallFailure(t *testing.T) {
	gobin := t.TempDir()
	env := installEnv(t, gobin)
	good := fixtureModule(t, "0.9.0")
	target := installFixture(t, good, gobin, env)

	// Now break the source so `go install` fails.
	broken := filepath.Join(good, "main.go")
	if err := os.WriteFile(broken, []byte("package main\nfunc main() { this is not go }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	u := &Updater{
		GoBin: goBin(t), InstallSpec: ".", CurrentVersion: "0.2.0",
		ExecutablePath: target, Env: env, Output: &out,
	}
	err := runIn(t, good, u)
	if err == nil {
		t.Fatal("a failing go install was reported as success")
	}
	if !strings.Contains(err.Error(), "go install") {
		t.Errorf("the error does not name the failing step: %v", err)
	}
	if strings.Contains(out.String(), "Updated") {
		t.Errorf("a failed install printed success: %s", out.String())
	}
	// The previously installed binary is still the working one.
	got, runErr := exec.Command(target, "--version").Output()
	if runErr != nil || !strings.Contains(string(got), "0.9.0") {
		t.Errorf("the existing binary was disturbed: %q %v", got, runErr)
	}
}

// ---- install target resolution ----------------------------------------

// GOPATH may hold a list and `go install` writes to the first entry.
// Joining the whole value to "bin" yields a path that does not exist,
// which is what the README used to tell people to do.
func TestInstallTargetUsesGOBINThenFirstGOPATHEntry(t *testing.T) {
	gb := goBin(t)
	ctx := context.Background()
	first, second := t.TempDir(), t.TempDir()

	u := &Updater{
		GoBin: gb,
		Env:   append(os.Environ(), "GOBIN=", "GOPATH="+first+string(filepath.ListSeparator)+second),
	}
	got, err := u.installTarget(ctx, gb)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(first, "bin", binaryName); got != want {
		t.Errorf("installTarget = %q, want %q (the head of GOPATH)", got, want)
	}

	gobin := t.TempDir()
	u.Env = append(os.Environ(), "GOBIN="+gobin, "GOPATH="+first)
	got, err = u.installTarget(ctx, gb)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(gobin, binaryName); got != want {
		t.Errorf("installTarget = %q, want %q (GOBIN wins)", got, want)
	}
}

// ---- helpers -----------------------------------------------------------

// runIn executes the updater with the process in dir, which is how
// `go install .` resolves the fixture module.
func runIn(t *testing.T, dir string, u *Updater) error {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(old) }()
	return u.Execute(context.Background())
}

func errorAs(err error, target **MismatchError) bool {
	for err != nil {
		if m, ok := err.(*MismatchError); ok {
			*target = m
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

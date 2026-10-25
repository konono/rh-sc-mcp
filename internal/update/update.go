// Package update re-runs `go install` to replace the running binary.
//
// Installation and update go through the same Go toolchain on purpose.
// Under Go's default settings a download is checked against the
// checksum database, so a second delivery path — fetching release
// tarballs and checking them against a checksums file served from the
// same origin — would add code for a weaker guarantee. That default can
// be turned off by the user (GOSUMDB, GOPRIVATE and friends), so it is
// the usual case rather than something this package enforces.
package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	binaryName    = "rh-sc-mcp"
	modulePackage = "github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp"
	installSpec   = modulePackage + "@latest"
)

// Updater holds the inputs to one update. The fields exist so tests can
// drive a real `go install` against a local module instead of the
// network, and can stand the process somewhere other than where the
// toolchain would install it.
type Updater struct {
	// GoBin is the go command. Resolved from PATH when empty.
	GoBin string
	// InstallSpec is the argument given to `go install`.
	InstallSpec string
	// CurrentVersion is what this binary reports today.
	CurrentVersion string
	// ExecutablePath is the running binary. Resolved from the process
	// when empty.
	ExecutablePath string
	// Env is the environment for the go commands. os.Environ() when nil.
	Env    []string
	Output io.Writer
}

// Run updates the running binary in place.
func Run(ctx context.Context, currentVersion string, output io.Writer) error {
	u := &Updater{
		InstallSpec:    installSpec,
		CurrentVersion: currentVersion,
		Output:         output,
	}
	return u.Execute(ctx)
}

// Execute resolves where `go install` would put the binary, refuses to
// act unless that is the binary now running, installs, and then asks the
// result for its version.
//
// The path check comes first and is not advisory. `go install` writes to
// GOBIN or GOPATH/bin regardless of where this process was started from,
// so for a copy living anywhere else the command would report a
// successful update of a file the user is not running — the one outcome
// worth going out of the way to prevent.
func (u *Updater) Execute(ctx context.Context) error {
	if u.Output == nil {
		u.Output = io.Discard
	}
	goBin, err := u.goCommand()
	if err != nil {
		return err
	}
	target, err := u.installTarget(ctx, goBin)
	if err != nil {
		return err
	}
	current, err := u.currentExecutable()
	if err != nil {
		return fmt.Errorf("find the running executable: %w", err)
	}
	if !samePath(target, current) {
		// Both resolved, so the two paths in the message are written
		// the same way. Otherwise one side can come back as /tmp/... and
		// the other as /private/tmp/..., and the cp line the message
		// suggests looks like it refers to a third file.
		return &MismatchError{Target: resolve(target), Current: current, Spec: u.spec()}
	}

	_, _ = fmt.Fprintf(u.Output, "Running go install %s\n", u.spec())
	cmd := exec.CommandContext(ctx, goBin, "install", u.spec())
	cmd.Env = u.env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install %s failed: %w\n%s", u.spec(), err,
			strings.TrimSpace(stderr.String()))
	}

	// Ask the installed binary what it is rather than assuming the
	// install produced something newer, or something that runs at all.
	version, err := u.installedVersion(ctx, target)
	if err != nil {
		return fmt.Errorf(
			"go install reported success but %s could not be run afterwards: %w", target, err)
	}
	if version == u.CurrentVersion {
		_, _ = fmt.Fprintf(u.Output,
			"%s is already at %s; %s has nothing newer.\n", binaryName, version, u.spec())
		return nil
	}

	// A different version is not necessarily a newer one. @latest can
	// resolve backwards — a tag withdrawn, a retraction, a proxy serving
	// an older view — and the binary has already been replaced by the
	// time we find out. Say which direction it went instead of calling
	// every change an update.
	direction, err := compare(version, u.CurrentVersion)
	if err != nil {
		return fmt.Errorf(
			"replaced %s, but the result reports version %q which cannot be compared with %q: %w\n"+
				"Check `%s --version` before relying on it",
			target, version, u.CurrentVersion, err, target)
	}
	if direction < 0 {
		return fmt.Errorf(
			"%s now holds %s, which is OLDER than the %s that was running.\n"+
				"The binary has already been replaced. To go back, install the version you want explicitly:\n"+
				"  go install %s@v%s",
			target, version, u.CurrentVersion, modulePackage, u.CurrentVersion)
	}
	_, _ = fmt.Fprintf(u.Output,
		"Updated %s from %s to %s at %s. Restart MCP clients to pick it up.\n",
		binaryName, u.CurrentVersion, version, target)
	return nil
}

// compare orders two X.Y.Z versions: 1 when a is newer than b, -1 when
// older, 0 when equal.
func compare(a, b string) (int, error) {
	left, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range left {
		switch {
		case left[i] > right[i]:
			return 1, nil
		case left[i] < right[i]:
			return -1, nil
		}
	}
	return 0, nil
}

func parseVersion(value string) ([3]int, error) {
	var version [3]int
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return version, fmt.Errorf("unsupported version %q (want X.Y.Z)", value)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		// strconv.Itoa round-trip rejects "01" and "+1", which Atoi
		// would otherwise accept as 1.
		if err != nil || n < 0 || strconv.Itoa(n) != part {
			return version, fmt.Errorf("unsupported version %q (want X.Y.Z)", value)
		}
		version[i] = n
	}
	return version, nil
}

// MismatchError says the toolchain would install somewhere other than
// the binary being run, and tells the user how to proceed by hand.
type MismatchError struct {
	Target  string
	Current string
	Spec    string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf(
		"not updating: go install would write to\n  %s\nbut the binary now running is\n  %s\n"+
			"These differ, so an update here would leave the running copy untouched while reporting success.\n"+
			"To update deliberately:\n"+
			"  go install %s          # refreshes the toolchain copy\n"+
			"  cp %s %s   # then replace this copy, if that is what you want\n"+
			"Point your MCP client at %s to avoid the split.",
		e.Target, e.Current, e.Spec, e.Target, e.Current, e.Target)
}

func (u *Updater) spec() string {
	if u.InstallSpec != "" {
		return u.InstallSpec
	}
	return installSpec
}

func (u *Updater) env() []string {
	if u.Env != nil {
		return u.Env
	}
	return os.Environ()
}

// goCommand locates the Go toolchain, which this command cannot work
// without now that install and update both go through it.
func (u *Updater) goCommand() (string, error) {
	if u.GoBin != "" {
		return u.GoBin, nil
	}
	path, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf(
			"the Go toolchain is required to update, and `go` was not found on PATH.\n"+
				"Install Go 1.25 or newer (https://go.dev/dl/), then run:\n"+
				"  go install %s", installSpec)
	}
	return path, nil
}

// installTarget is where `go install` would write the binary: GOBIN when
// set, otherwise bin/ under the first GOPATH entry.
//
// Only the first entry. GOPATH may hold a list, and `go install` writes
// to the head of it; joining the whole value to "/bin" produces a path
// that does not exist.
func (u *Updater) installTarget(ctx context.Context, goBin string) (string, error) {
	gobin, err := u.goEnv(ctx, goBin, "GOBIN")
	if err != nil {
		return "", err
	}
	if gobin != "" {
		return filepath.Join(gobin, binaryName), nil
	}
	gopath, err := u.goEnv(ctx, goBin, "GOPATH")
	if err != nil {
		return "", err
	}
	if gopath == "" {
		return "", errors.New("neither GOBIN nor GOPATH is set; cannot tell where go install would write")
	}
	first := filepath.SplitList(gopath)[0]
	if first == "" {
		return "", fmt.Errorf("GOPATH %q has an empty first entry", gopath)
	}
	return filepath.Join(first, "bin", binaryName), nil
}

func (u *Updater) goEnv(ctx context.Context, goBin, name string) (string, error) {
	cmd := exec.CommandContext(ctx, goBin, "env", name)
	cmd.Env = u.env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w\n%s", name, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func (u *Updater) currentExecutable() (string, error) {
	if u.ExecutablePath != "" {
		return resolve(u.ExecutablePath), nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolve(path), nil
}

// installedVersion runs the binary that was just installed and reads
// the version out of its --version line.
//
// The expected line is exactly "rh-sc-mcp X.Y.Z". Taking the last word
// of whatever came back would turn an error message, a warning on
// stdout, or a changed output format into a "version", and that string
// then decides whether this reports an update.
func (u *Updater) installedVersion(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = u.env()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "", errors.New("--version printed nothing")
	}
	if strings.ContainsAny(text, "\r\n") {
		return "", fmt.Errorf("--version printed more than one line: %q", clip(text))
	}
	fields := strings.Fields(text)
	if len(fields) != 2 || fields[0] != binaryName {
		return "", fmt.Errorf("--version printed %q, want %q followed by a version", clip(text), binaryName)
	}
	return fields[1], nil
}

func clip(s string) string {
	const n = 120
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// resolve expands symlinks so two names for one file compare equal. A
// path that cannot be resolved is returned cleaned, which keeps the
// comparison conservative: unresolvable names simply will not match.
func resolve(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// samePath reports whether two names refer to the same path once
// symlinks are followed.
//
// Deliberately not os.SameFile. That compares inodes, so it calls two
// hard links to one file the same — and they are, right up until the
// update, because `go install` replaces its target by renaming a new
// file over it. Measured: the installed path gets a fresh inode while
// the other link keeps the old one, so the running copy is left on the
// previous binary. Reporting success there is the exact failure this
// check exists to stop. Symlinks are different: they resolve to the
// same path, and the rename is visible through them.
func samePath(a, b string) bool {
	return resolve(a) == resolve(b)
}

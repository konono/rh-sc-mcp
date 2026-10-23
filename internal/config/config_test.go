package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts a config file in a temp dir and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// withToken sets the offline token for one test, since every valid
// config needs one.
func withToken(t *testing.T) {
	t.Helper()
	t.Setenv("RH_SC_OFFLINE_TOKEN", "offline-token-for-tests")
}

func TestDefaultsAreReadOnlyAndPointAtThePublishedEndpoint(t *testing.T) {
	withToken(t)
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsReadOnly() {
		t.Error("a config that says nothing about writes must not grant them")
	}
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want the published endpoint", c.BaseURL)
	}
	if c.APIVersion != "v3" {
		t.Errorf("apiVersion = %q", c.APIVersion)
	}
	if c.SSOTokenURL != DefaultSSOTokenURL || c.SSOClientID != DefaultSSOClientID {
		t.Errorf("SSO defaults are wrong: %q %q", c.SSOTokenURL, c.SSOClientID)
	}
	if c.LevelFor(ResCase) != LevelRead || c.LevelFor(ResComment) != LevelRead {
		t.Errorf("cases/comments should default to read, got %s/%s", c.LevelFor(ResCase), c.LevelFor(ResComment))
	}
}

// The knowledge-base surface is a different API from case management, so
// it stays off until it is named — even when the operator raises the
// default for everything else.
func TestKCSStaysOffUnlessNamed(t *testing.T) {
	withToken(t)
	c, err := Load(write(t, "readOnly: false\ndefaultPermission: write\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.LevelFor(ResKCS) != LevelNone {
		t.Fatalf("defaultPermission switched on KCS: %s", c.LevelFor(ResKCS))
	}

	c2, err := Load(write(t, "resources:\n  kcs:\n    permission: read\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c2.LevelFor(ResKCS) != LevelRead {
		t.Fatalf("an explicit kcs rule was ignored: %s", c2.LevelFor(ResKCS))
	}
}

// readOnly is the override of last resort: a resource granted write must
// still come back capped while it is on.
func TestReadOnlyCapsEveryResource(t *testing.T) {
	withToken(t)
	c, err := Load(write(t, "readOnly: true\nresources:\n  case:\n    permission: write\n  comment:\n    permission: write\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []Resource{ResCase, ResComment} {
		if c.LevelFor(r) != LevelRead {
			t.Errorf("%s = %s under readOnly, want read", r, c.LevelFor(r))
		}
		if c.Allows(r, LevelWrite) {
			t.Errorf("%s allows writes under readOnly", r)
		}
	}
}

func TestWritesRequireBothReadOnlyOffAndAGrant(t *testing.T) {
	withToken(t)
	// readOnly off, but no grant above the default read.
	c, err := Load(write(t, "readOnly: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Allows(ResCase, LevelWrite) {
		t.Error("turning readOnly off must not by itself grant writes")
	}

	c2, err := Load(write(t, "readOnly: false\nresources:\n  case:\n    permission: write\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !c2.Allows(ResCase, LevelWrite) {
		t.Error("an explicit write grant with readOnly off was not honoured")
	}
	if c2.Allows(ResComment, LevelWrite) {
		t.Error("a grant on case leaked into comment")
	}
}

func TestEnvOverridesConfigFile(t *testing.T) {
	withToken(t)
	t.Setenv("RH_SC_READ_ONLY", "false")
	t.Setenv("RH_SC_BASE_URL", "https://example.test/support")
	t.Setenv("RH_SC_API_VERSION", "v1")

	c, err := Load(write(t, "readOnly: true\nbaseURL: https://ignored.test/support\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.IsReadOnly() {
		t.Error("$RH_SC_READ_ONLY did not override the file")
	}
	if c.BaseURL != "https://example.test/support" {
		t.Errorf("baseURL = %q", c.BaseURL)
	}
	if c.APIVersion != "v1" {
		t.Errorf("apiVersion = %q", c.APIVersion)
	}
}

func TestMissingOfflineTokenIsRejectedWithAnActionableMessage(t *testing.T) {
	t.Setenv("RH_SC_OFFLINE_TOKEN", "")
	_, err := Load("")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "RH_SC_OFFLINE_TOKEN") {
		t.Fatalf("the error does not say how to fix it: %v", err)
	}
}

// A typo in a key must fail loudly: silently keeping the default would
// mean an operator who wrote `permisson: read` believes they granted
// something they did not, or revoked something they did not.
func TestUnknownKeysAreRejected(t *testing.T) {
	withToken(t)
	if _, err := Load(write(t, "reedOnly: true\n")); err == nil {
		t.Fatal("a misspelled key was accepted")
	}
}

func TestUnknownResourceIsRejected(t *testing.T) {
	withToken(t)
	_, err := Load(write(t, "resources:\n  attachment:\n    permission: read\n"))
	if err == nil {
		t.Fatal("an unknown resource was accepted")
	}
	if !strings.Contains(err.Error(), "attachment") {
		t.Fatalf("the error does not name the offending resource: %v", err)
	}
}

func TestInvalidValuesAreRejected(t *testing.T) {
	withToken(t)
	for name, body := range map[string]string{
		"bad permission level": "resources:\n  case:\n    permission: superuser\n",
		"bad api version":      "apiVersion: v2\n",
		"relative base url":    "baseURL: /support\n",
		"non-http base url":    "baseURL: ftp://example.test/support\n",
		"maxResults over cap":  "maxResults: 5000\n",
		"bad caseViewURL":      "caseViewURL: not-a-url\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestClampMaxResults(t *testing.T) {
	withToken(t)
	c, err := Load(write(t, "maxResults: 25\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want int }{
		{0, 25},   // unset means the configured default
		{10, 10},  // under the cap passes through
		{25, 25},  // at the cap
		{200, 25}, // over the cap is clamped
		{-1, 25},  // nonsense falls back rather than erroring here
	} {
		if got := c.ClampMaxResults(tc.in); got != tc.want {
			t.Errorf("ClampMaxResults(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// Without an operator-supplied console prefix there is no link to build.
// Inventing one would put a URL this server cannot vouch for in front of
// a user.
func TestCaseURLIsEmptyUntilConfigured(t *testing.T) {
	withToken(t)
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.CaseURL("04337263"); got != "" {
		t.Fatalf("CaseURL = %q, want empty when caseViewURL is unset", got)
	}

	c2, err := Load(write(t, "caseViewURL: https://console.test/cases/\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c2.CaseURL("04337263"); got != "https://console.test/cases/04337263" {
		t.Fatalf("CaseURL = %q", got)
	}
	if got := c2.CaseURL(""); got != "" {
		t.Fatalf("CaseURL(\"\") = %q, want empty", got)
	}
}

func TestWarningsCoverTheRiskyCombinations(t *testing.T) {
	withToken(t)
	c, err := Load(write(t, "readOnly: false\nresources:\n  comment:\n    permission: write\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Warnings()) == 0 {
		t.Fatal("a server that can post comments produced no warning")
	}

	c2, err := Load(write(t, "baseURL: https://example.test/support\n"))
	if err != nil {
		t.Fatal(err)
	}
	var sawEndpoint bool
	for _, w := range c2.Warnings() {
		if strings.Contains(w, "example.test") {
			sawEndpoint = true
		}
	}
	if !sawEndpoint {
		t.Fatalf("a non-default endpoint produced no warning: %v", c2.Warnings())
	}

	c3, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(c3.Warnings()) != 0 {
		t.Fatalf("the default read-only config warned about something: %v", c3.Warnings())
	}
}

func TestParseLevelSpellings(t *testing.T) {
	for in, want := range map[string]Level{
		"":           LevelNone,
		"none":       LevelNone,
		"read":       LevelRead,
		"read-only":  LevelRead,
		"READ":       LevelRead,
		"write":      LevelWrite,
		"read-write": LevelWrite,
	} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("admin"); err == nil {
		t.Error("admin was accepted, but this server has no admin level")
	}
}

func TestAPIRoot(t *testing.T) {
	withToken(t)
	c, err := Load(write(t, "baseURL: https://example.test/support/\napiVersion: v3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.APIRoot(); got != "https://example.test/support/v3" {
		t.Fatalf("APIRoot = %q", got)
	}
}

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

func TestResolvePathCreatesPrivateDefaultWithoutOverwritingIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultPath := filepath.Join(home, ".config", "rh-sc-mcp", "config.yaml")
	explicit := filepath.Join(home, "other.yaml")
	got, err := ResolvePath(explicit)
	if err != nil || got != explicit {
		t.Fatalf("explicit path: path=%q, err=%v; want %q", got, err, explicit)
	}
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Fatalf("explicit path unexpectedly created the default config: %v", err)
	}

	got, err = ResolvePath("")
	if err != nil || got != defaultPath {
		t.Fatalf("new default: path=%q, err=%v; want %q", got, err, defaultPath)
	}
	info, err := os.Stat(defaultPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("default config permissions: info=%v, err=%v", info, err)
	}
	dirInfo, err := os.Stat(filepath.Dir(defaultPath))
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("default config directory permissions: info=%v, err=%v", dirInfo, err)
	}
	contents, err := os.ReadFile(defaultPath)
	if err != nil || !strings.Contains(string(contents), "readOnly: true") || strings.Contains(string(contents), "\nofflineToken:") {
		t.Fatalf("unexpected initial config: err=%v, contents=%q", err, contents)
	}
	withToken(t)
	c, err := Load(got)
	if err != nil || !c.IsReadOnly() {
		t.Fatalf("initial config was not loaded read-only: config=%+v, err=%v", c, err)
	}
	custom := []byte("readOnly: false\nmaxResults: 25\n")
	if err := os.WriteFile(defaultPath, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = ResolvePath("")
	if err != nil || got != defaultPath {
		t.Fatalf("present default: path=%q, err=%v; want %q", got, err, defaultPath)
	}
	contents, err = os.ReadFile(defaultPath)
	if err != nil || string(contents) != string(custom) {
		t.Fatalf("existing default was overwritten: err=%v, contents=%q", err, contents)
	}
	if _, err := Load(got); err != nil {
		t.Fatal("existing default config failed to load")
	}
	if _, err := Load(explicit); err == nil {
		t.Fatal("a missing explicit config must fail")
	}
}

func TestDefaultFileAndEnvironmentMergeByKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultPath := filepath.Join(home, ".config", "rh-sc-mcp", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath, []byte("offlineToken: from-file\nreadOnly: false\nmaxResults: 25\nbaseURL: https://file.example/support\nhttp:\n  addr: 127.0.0.1:8080\n  token: file-http-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RH_SC_OFFLINE_TOKEN", "from-env")
	t.Setenv("RH_SC_READ_ONLY", "true")
	t.Setenv("RH_SC_MCP_HTTP_TOKEN", "env-http-token")

	path, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.OfflineToken != "from-env" || !c.IsReadOnly() || c.HTTP.Token != "env-http-token" {
		t.Fatal("environment values did not override their matching config keys")
	}
	if c.MaxResults != 25 || c.BaseURL != "https://file.example/support" || c.HTTP.Addr != "127.0.0.1:8080" {
		t.Fatal("unrelated file values were lost during environment override")
	}
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

// ---- HTTP transport -------------------------------------------------------

func TestHTTPIsOffUnlessAnAddressIsSet(t *testing.T) {
	withToken(t)
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Enabled() {
		t.Error("the HTTP transport is on without an address")
	}
	if len(c.Warnings()) != 0 {
		t.Errorf("a stdio-only config warned about something: %v", c.Warnings())
	}
}

// An open port has no operating-system gate in front of it, so the
// bearer token is not optional.
func TestHTTPAddressWithoutATokenIsRejected(t *testing.T) {
	withToken(t)
	_, err := Load(write(t, "http:\n  addr: \":8080\"\n"))
	if err == nil {
		t.Fatal("an HTTP listener with no bearer token was accepted")
	}
	for _, want := range []string{"RH_SC_MCP_HTTP_TOKEN", "openssl rand -hex 32"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say how to fix it (%q): %v", want, err)
		}
	}
}

// Reusing the offline token would make the string handed to every MCP
// client a working Red Hat credential, usable outside this server.
func TestHTTPTokenMustDifferFromTheOfflineToken(t *testing.T) {
	const shared = "the-very-same-string"
	t.Setenv("RH_SC_OFFLINE_TOKEN", shared)
	_, err := Load(write(t, "http:\n  addr: \":8080\"\n  token: "+shared+"\n"))
	if err == nil {
		t.Fatal("the offline token was accepted as the shared HTTP token")
	}
	if strings.Contains(err.Error(), shared) {
		t.Errorf("the error quoted the credential: %v", err)
	}
	if !strings.Contains(err.Error(), "must differ") {
		t.Errorf("the error does not explain the rule: %v", err)
	}
}

func TestHTTPEnvOverrides(t *testing.T) {
	withToken(t)
	t.Setenv("RH_SC_MCP_HTTP_ADDR", "127.0.0.1:9999")
	t.Setenv("RH_SC_MCP_HTTP_TOKEN", "from-the-environment")

	c, err := Load(write(t, "http:\n  addr: \":1111\"\n  token: from-the-file\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Addr != "127.0.0.1:9999" || c.HTTP.Token != "from-the-environment" {
		t.Fatalf("env did not win: addr=%q", c.HTTP.Addr)
	}
}

// --http is applied after Load has run, so the rules have to be checked
// again or the flag becomes a way around them.
func TestOverrideHTTPAddrRevalidates(t *testing.T) {
	t.Run("no token", func(t *testing.T) {
		withToken(t)
		c, err := Load("")
		if err != nil {
			t.Fatal(err)
		}
		if err := c.OverrideHTTPAddr(":8080"); err == nil {
			t.Fatal("--http opened a port with no bearer token configured")
		}
	})

	t.Run("token equal to the offline token", func(t *testing.T) {
		const shared = "identical-credential"
		t.Setenv("RH_SC_OFFLINE_TOKEN", shared)
		c, err := Load(write(t, "http:\n  token: "+shared+"\n"))
		if err != nil {
			// With no addr the config is inert, so it must load.
			t.Fatalf("a config with a token but no address should load: %v", err)
		}
		if err := c.OverrideHTTPAddr(":8080"); err == nil {
			t.Fatal("--http enabled a listener whose token is the offline token")
		}
	})

	t.Run("malformed address", func(t *testing.T) {
		withToken(t)
		c, err := Load(write(t, "http:\n  token: a-separate-token\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.OverrideHTTPAddr("not-an-address"); err == nil {
			t.Fatal("a listen address without a port was accepted")
		}
	})

	t.Run("valid", func(t *testing.T) {
		withToken(t)
		c, err := Load(write(t, "http:\n  token: a-separate-token\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.OverrideHTTPAddr(":8080"); err != nil {
			t.Fatalf("a valid override was rejected: %v", err)
		}
		if !c.HTTP.Enabled() || c.HTTP.Addr != ":8080" {
			t.Errorf("addr = %q", c.HTTP.Addr)
		}
	})

	t.Run("empty override leaves the config alone", func(t *testing.T) {
		withToken(t)
		c, err := Load(write(t, "http:\n  addr: \":1234\"\n  token: a-separate-token\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.OverrideHTTPAddr(""); err != nil {
			t.Fatal(err)
		}
		if c.HTTP.Addr != ":1234" {
			t.Errorf("an unset flag changed the address to %q", c.HTTP.Addr)
		}
	})
}

func TestHTTPRejectsBadValues(t *testing.T) {
	withToken(t)
	for name, body := range map[string]string{
		"negative session timeout": "http:\n  addr: \":8080\"\n  token: t0ken-distinct\n  sessionTimeoutSeconds: -1\n",
		"empty trusted origin":     "http:\n  addr: \":8080\"\n  token: t0ken-distinct\n  trustedOrigins: [\"\"]\n",
		"address without a port":   "http:\n  addr: \"localhost\"\n  token: t0ken-distinct\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// Serving on a port is worth saying out loud, since it is the one
// setting that makes this process reachable by anything but its parent.
func TestEnablingHTTPProducesAWarning(t *testing.T) {
	withToken(t)
	c, err := Load(write(t, "http:\n  addr: \":8080\"\n  token: a-separate-token\n"))
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, w := range c.Warnings() {
		if strings.Contains(w, "HTTP") {
			saw = true
		}
		if strings.Contains(w, "a-separate-token") {
			t.Errorf("a warning quoted the token: %s", w)
		}
	}
	if !saw {
		t.Errorf("enabling HTTP produced no warning: %v", c.Warnings())
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

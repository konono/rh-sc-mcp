// Package config loads and validates the MCP server's configuration.
//
// The operator — not the model — decides what this server can reach. Every
// permission is derived from a Config: tools above the granted level are
// never registered, so the model cannot see them in tools/list, and the
// handlers re-check the level before touching the API.
//
// The defaults are deliberately timid. A config that only supplies an
// offline token yields a read-only case client: readOnly is on unless the
// operator turns it off, and the KCS search surface stays off until it is
// named explicitly.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Default endpoints. These come from the published Case Management API
// document (Swagger 2.0, host api.access.redhat.com, basePath /support) and
// from Red Hat's documented SSO token exchange.
const (
	DefaultBaseURL     = "https://api.access.redhat.com/support"
	DefaultSSOTokenURL = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token"
	DefaultSSOClientID = "rhsm-api"

	// DefaultAPIVersion selects the case API major version. The document
	// carries both /v1 and /v3 case paths with the same shapes; v3 is the
	// current one and the only one this server is tested against.
	DefaultAPIVersion = "v3"
)

// Limits that are properties of the upstream API or of keeping a response
// small enough to put in a model's context.
const (
	// MaxResultsHardCap is the API's own documented ceiling for
	// FilterCasesV3Request.maxResults.
	MaxResultsHardCap = 200
	defaultMaxResults = 50
	defaultMaxComment = 50
	defaultTimeoutSec = 60
)

// Level is the permission granted on a resource: LevelNone < LevelRead <
// LevelWrite. There is no delete level because the case API this server
// covers exposes no destructive case operation.
type Level int

const (
	LevelNone Level = iota
	LevelRead
	LevelWrite
)

func (l Level) String() string {
	switch l {
	case LevelRead:
		return "read"
	case LevelWrite:
		return "write"
	default:
		return "none"
	}
}

// ParseLevel accepts the spellings an operator is likely to write.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "off", "deny":
		return LevelNone, nil
	case "read", "readonly", "read-only":
		return LevelRead, nil
	case "write", "readwrite", "read-write":
		return LevelWrite, nil
	default:
		return LevelNone, fmt.Errorf("unknown permission level %q (want none|read|write)", s)
	}
}

func (l *Level) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := ParseLevel(s)
	if err != nil {
		return err
	}
	*l = v
	return nil
}

// Resource names an API area that can be scoped independently.
type Resource string

const (
	// ResCase covers reading and writing the case record itself.
	ResCase Resource = "case"
	// ResComment covers case comments, separated from the case so that an
	// operator can allow commenting without allowing a status or severity
	// change — the common "let the agent reply, not re-triage" split.
	ResComment Resource = "comment"
	// ResKCS covers the knowledge-base search endpoint, which is a
	// different API surface (Solr) than case management.
	ResKCS Resource = "kcs"
)

// AllResources is the set of scopable resources, in the order rhcase_scope
// reports them.
var AllResources = []Resource{ResCase, ResComment, ResKCS}

// defaultLevels are the levels used when a resource has no explicit rule
// and no defaultPermission is configured.
//
// KCS is none rather than read because it reaches outside case management:
// an operator who configured this server to read their support cases has
// not thereby asked for a knowledge-base search tool. It must be named.
var defaultLevels = map[Resource]Level{
	ResCase:    LevelRead,
	ResComment: LevelRead,
	ResKCS:     LevelNone,
}

// ResourceRule is the per-resource scope.
type ResourceRule struct {
	// Permission is the maximum level granted on this resource.
	Permission Level `yaml:"permission"`
}

// Config is the full server configuration.
type Config struct {
	// BaseURL is the case API root, including the basePath. It defaults to
	// the published endpoint; override it for a different deployment or to
	// point tests at a local server.
	BaseURL string `yaml:"baseURL"`

	// APIVersion is the case API major version segment ("v3" or "v1").
	APIVersion string `yaml:"apiVersion"`

	// SSOTokenURL and SSOClientID configure the offline-token exchange.
	SSOTokenURL string `yaml:"ssoTokenURL"`
	SSOClientID string `yaml:"ssoClientID"`

	// OfflineToken is the Red Hat API offline token. Prefer
	// $RH_SC_OFFLINE_TOKEN so the credential can come from a secret store
	// rather than a file; when it is written here instead, keep the file
	// out of any repository and off a shared disk.
	OfflineToken string `yaml:"offlineToken"`

	// CaseViewURL is an optional console prefix for building a
	// human-readable link in tool output: the case number is appended to
	// it as a final path segment.
	//
	// There is no default, deliberately. The API document describes the
	// API and says nothing about the web console's URL layout, so any
	// built-in value would be this server guessing at a link and
	// presenting the guess as fact. Left unset, tool output carries only
	// the identifiers the API itself returned — the `uri` field on a case
	// and the `location` of a newly created one.
	CaseViewURL string `yaml:"caseViewURL"`

	// ReadOnly is a global kill switch: every resource is capped at read.
	// It is a pointer so that an absent key can default to true while
	// `readOnly: false` remains expressible.
	ReadOnly *bool `yaml:"readOnly"`

	// DefaultPermission applies to resources with no explicit rule. When it
	// is absent, each resource falls back to its own default: read for
	// cases and comments, none for KCS.
	DefaultPermission *Level `yaml:"defaultPermission"`

	Resources map[Resource]ResourceRule `yaml:"resources"`

	// MaxResults caps the number of cases a single search may return, so
	// one call cannot drag an entire account into the model's context.
	MaxResults int `yaml:"maxResults"`

	// MaxComments caps how many comments a listing returns. The API has no
	// count parameter for comments, so this is applied after the response
	// arrives; the tool reports when it truncated.
	MaxComments int `yaml:"maxComments"`

	// TimeoutSeconds bounds a single upstream HTTP call.
	TimeoutSeconds int `yaml:"timeoutSeconds"`
}

// Load reads a YAML config from path (pass "" for defaults plus
// environment) and applies environment overrides.
func Load(path string) (*Config, error) {
	c := &Config{}
	if path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
		dec := yaml.NewDecoder(strings.NewReader(string(b)))
		// Strict, so a typo'd key ("permisson") fails loudly instead of
		// silently leaving the default in place.
		dec.KnownFields(true)
		if err := dec.Decode(c); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	if v := os.Getenv("RH_SC_OFFLINE_TOKEN"); v != "" {
		c.OfflineToken = v
	}
	if v := os.Getenv("RH_SC_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("RH_SC_API_VERSION"); v != "" {
		c.APIVersion = v
	}
	if v := os.Getenv("RH_SC_SSO_TOKEN_URL"); v != "" {
		c.SSOTokenURL = v
	}
	if v := os.Getenv("RH_SC_SSO_CLIENT_ID"); v != "" {
		c.SSOClientID = v
	}
	if v, ok := os.LookupEnv("RH_SC_READ_ONLY"); ok {
		b, err := parseBool(v)
		if err != nil {
			return nil, fmt.Errorf("$RH_SC_READ_ONLY: %w", err)
		}
		c.ReadOnly = &b
	}

	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func parseBool(v string) (bool, error) {
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false, fmt.Errorf("%q is not a boolean (want true/false or 1/0)", v)
	}
	return b, nil
}

func (c *Config) applyDefaults() {
	c.BaseURL = strings.TrimSuffix(strings.TrimSpace(c.BaseURL), "/")
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	c.APIVersion = strings.Trim(strings.TrimSpace(c.APIVersion), "/")
	if c.APIVersion == "" {
		c.APIVersion = DefaultAPIVersion
	}
	if strings.TrimSpace(c.SSOTokenURL) == "" {
		c.SSOTokenURL = DefaultSSOTokenURL
	}
	if strings.TrimSpace(c.SSOClientID) == "" {
		c.SSOClientID = DefaultSSOClientID
	}
	c.CaseViewURL = strings.TrimSuffix(strings.TrimSpace(c.CaseViewURL), "/")
	if c.ReadOnly == nil {
		// Writing to a support case is visible to Red Hat and to everyone
		// on the account, and a comment cannot be unsent. Opt in.
		t := true
		c.ReadOnly = &t
	}
	if c.MaxResults <= 0 {
		c.MaxResults = defaultMaxResults
	}
	if c.MaxComments <= 0 {
		c.MaxComments = defaultMaxComment
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = defaultTimeoutSec
	}
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.OfflineToken) == "" {
		return fmt.Errorf("offline token is not set: set $RH_SC_OFFLINE_TOKEN, " +
			"or add `offlineToken:` to the config file (and keep that file private)")
	}
	if err := checkAbsoluteHTTPURL("baseURL", c.BaseURL); err != nil {
		return err
	}
	if err := checkAbsoluteHTTPURL("ssoTokenURL", c.SSOTokenURL); err != nil {
		return err
	}
	if c.CaseViewURL != "" {
		if err := checkAbsoluteHTTPURL("caseViewURL", c.CaseViewURL); err != nil {
			return err
		}
	}
	switch c.APIVersion {
	case "v1", "v3":
	default:
		return fmt.Errorf("apiVersion %q is not supported (want v3, or v1 for the legacy paths)", c.APIVersion)
	}
	if c.MaxResults > MaxResultsHardCap {
		return fmt.Errorf("maxResults is %d; the API accepts at most %d", c.MaxResults, MaxResultsHardCap)
	}
	for r := range c.Resources {
		if _, ok := defaultLevels[r]; !ok {
			return fmt.Errorf("unknown resource %q in config `resources:` (want %s)",
				r, joinResources(AllResources))
		}
	}
	return nil
}

func checkAbsoluteHTTPURL(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s %q is not a valid URL: %w", field, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s %q must be an absolute http or https URL", field, raw)
	}
	if u.Host == "" {
		return fmt.Errorf("%s %q must include a host", field, raw)
	}
	return nil
}

func joinResources(rs []Resource) string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, string(r))
	}
	return strings.Join(out, "|")
}

// IsReadOnly reports the global kill switch.
func (c *Config) IsReadOnly() bool { return c.ReadOnly == nil || *c.ReadOnly }

// LevelFor returns the effective permission on a resource, after applying
// the global read-only cap.
func (c *Config) LevelFor(r Resource) Level {
	l, known := defaultLevels[r]
	if !known {
		return LevelNone
	}
	if c.DefaultPermission != nil {
		l = *c.DefaultPermission
		// defaultPermission is a convenience for the resources an operator
		// is already using; it does not quietly switch on the KCS surface,
		// which has to be named. See defaultLevels.
		if r == ResKCS {
			l = LevelNone
		}
	}
	if rule, ok := c.Resources[r]; ok {
		l = rule.Permission
	}
	if c.IsReadOnly() && l > LevelRead {
		l = LevelRead
	}
	return l
}

// Allows reports whether a tool needing `need` on `r` may be registered.
func (c *Config) Allows(r Resource, need Level) bool {
	return c.LevelFor(r) >= need
}

// ClampMaxResults applies the configured ceiling to a requested page size.
func (c *Config) ClampMaxResults(n int) int {
	if n <= 0 {
		return c.MaxResults
	}
	if n > c.MaxResults {
		return c.MaxResults
	}
	return n
}

// CaseURL builds a console link for a case number by appending it to the
// configured caseViewURL.
//
// It returns "" when caseViewURL is unset or the number is empty, and
// callers omit the field in that case. An absent link is better than a
// plausible one that 404s: the model would hand the latter to a user as
// though this server knew it was right.
func (c *Config) CaseURL(caseNumber string) string {
	if c.CaseViewURL == "" || strings.TrimSpace(caseNumber) == "" {
		return ""
	}
	return c.CaseViewURL + "/" + url.PathEscape(caseNumber)
}

// APIRoot is the versioned prefix for case endpoints, e.g.
// "https://api.access.redhat.com/support/v3".
func (c *Config) APIRoot() string { return c.BaseURL + "/" + c.APIVersion }

// Warnings lists configurations that are legal but worth seeing, so the
// operator meets them in rhcase_scope and on stderr rather than in an
// audit log.
func (c *Config) Warnings() []string {
	var w []string
	if !c.IsReadOnly() {
		var writable []string
		for _, r := range AllResources {
			if c.LevelFor(r) >= LevelWrite {
				writable = append(writable, string(r))
			}
		}
		if len(writable) > 0 {
			w = append(w, fmt.Sprintf(
				"readOnly is off and %s may be written: this server can create cases and post comments that Red Hat and everyone on the account will see, and neither can be withdrawn",
				strings.Join(writable, ", ")))
		}
	}
	if c.BaseURL != DefaultBaseURL {
		w = append(w, fmt.Sprintf(
			"baseURL is %s, not the published %s: request and response shapes are only guaranteed for the published endpoint",
			c.BaseURL, DefaultBaseURL))
	}
	if c.APIVersion != DefaultAPIVersion {
		w = append(w, fmt.Sprintf(
			"apiVersion is %s: this server is written against %s, and the older paths may answer with different shapes",
			c.APIVersion, DefaultAPIVersion))
	}
	return w
}

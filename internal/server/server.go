// Package server wires the configuration to MCP tools.
//
// Scope is enforced twice, on purpose:
//
//  1. At registration — a tool whose required permission exceeds the
//     configured level is never added, so it is absent from tools/list and
//     the model cannot call it.
//  2. At call time — every handler re-checks before touching the API, so a
//     client holding a stale tool list, or a future code path that
//     registers conditionally, still cannot get through.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
	"github.com/konono/rh-sc-mcp/internal/version"
)

// Version is reported to MCP clients during initialize.
const Version = version.Version

type Server struct {
	cfg *config.Config
	api *redhat.Client
	mcp *mcp.Server

	registered []string
}

// New builds the MCP server and registers the tools the scope allows.
func New(cfg *config.Config) *Server {
	api := redhat.NewClient(
		cfg.BaseURL,
		cfg.APIVersion,
		cfg.SSOTokenURL,
		cfg.SSOClientID,
		cfg.OfflineToken,
		time.Duration(cfg.TimeoutSeconds)*time.Second,
	)
	return newWithClient(cfg, api)
}

// newWithClient is the seam the tests use to point the server at an
// httptest server without going through the SSO exchange twice.
func newWithClient(cfg *config.Config, api *redhat.Client) *Server {
	s := &Server{cfg: cfg, api: api}
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    "rh-sc-mcp",
		Title:   "Red Hat Support Cases",
		Version: Version,
	}, &mcp.ServerOptions{
		Instructions: s.instructions(),
	})
	s.registerAll()
	return s
}

// Registered returns the names of the tools that survived scope
// filtering, for the --print-scope diagnostic.
func (s *Server) Registered() []string { return s.registered }

// Run serves MCP over stdio.
func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

// MCP exposes the underlying server so tests can connect an in-memory
// transport to it.
func (s *Server) MCP() *mcp.Server { return s.mcp }

// ScopeReport is the --print-scope payload.
//
// It lives here rather than in main so it can be tested: its one hard
// requirement is that no credential appears in it, and that is worth a
// test rather than a careful reading. The HTTP token is reported as
// present or absent, never as a value — not even a prefix or a length,
// either of which would narrow a guess.
func (s *Server) ScopeReport() map[string]any {
	resources := map[string]string{}
	for _, r := range config.AllResources {
		resources[string(r)] = s.cfg.LevelFor(r).String()
	}
	warnings := s.cfg.Warnings()
	if warnings == nil {
		warnings = []string{}
	}
	httpInfo := map[string]any{
		"enabled":         s.cfg.HTTP.Enabled(),
		"tokenConfigured": s.cfg.HTTP.Token != "",
	}
	if s.cfg.HTTP.Enabled() {
		httpInfo["addr"] = s.cfg.HTTP.Addr
		httpInfo["path"] = MCPPath
		httpInfo["stateless"] = s.cfg.HTTP.IsStateless()
		httpInfo["trustedOrigins"] = s.cfg.HTTP.TrustedOrigins
		if !s.cfg.HTTP.IsStateless() {
			httpInfo["sessionTimeoutSeconds"] = s.cfg.HTTP.SessionTimeoutSeconds
		}
	}
	out := map[string]any{
		"endpoint":    s.cfg.BaseURL,
		"apiVersion":  s.cfg.APIVersion,
		"ssoTokenURL": s.cfg.SSOTokenURL,
		"ssoClientID": s.cfg.SSOClientID,
		"readOnly":    s.cfg.IsReadOnly(),
		"resources":   resources,
		"tools":       s.Registered(),
		"maxResults":  s.cfg.MaxResults,
		"maxComments": s.cfg.MaxComments,
		"timeoutSec":  s.cfg.TimeoutSeconds,
		"warnings":    warnings,
		"http":        httpInfo,
	}
	if s.cfg.CaseViewURL != "" {
		out["caseViewURL"] = s.cfg.CaseViewURL
	}
	return out
}

// AuthCheck is the result of --check-auth.
//
// It deliberately holds no credential, not even a fingerprint: the usual
// destination for this output is a bug report or a terminal someone is
// screen-sharing.
type AuthCheck struct {
	SSOTokenURL  string `json:"ssoTokenURL"`
	SSOClientID  string `json:"ssoClientID"`
	Endpoint     string `json:"endpoint"`
	TokenAcquire string `json:"tokenExchange"`
	APIProbe     string `json:"apiProbe"`
	// Error is the scrubbed failure, when there was one.
	Error string `json:"error,omitempty"`
}

// authProbeKeyword is the search term the API probe uses.
//
// Some criterion is required, because the service refuses a filter that
// only says how to page (see FilterCasesRequest.HasCriteria). This term
// is specific enough that it normally returns no cases: run against the
// live API on 2026-10-25 it answered 200 with totalCount 0. That is an
// observation about the data on that day, not a property of the term —
// nothing stops a future case from containing this string, and what the
// search costs the service to run was not measured either way.
//
// Which is why it is not what keeps case data out of the diagnostic.
// CheckAuth caps the request at one result and discards the response
// body entirely, so AuthCheck cannot carry case content whether the term
// matches or not.
const authProbeKeyword = "rhscmcpauthprobezzzzzz"

// OK reports whether every step that ran succeeded.
func (a *AuthCheck) OK() bool { return a.Error == "" }

// CheckAuth exchanges the offline token and, when cases are readable,
// makes the smallest possible API call to confirm the token is accepted
// by the API and not merely issued by SSO.
//
// The two steps are reported separately because they fail for different
// reasons and have different fixes: SSO rejects a revoked or mistyped
// offline token, while the API rejects a token whose account cannot see
// what was asked for.
func (s *Server) CheckAuth(ctx context.Context) *AuthCheck {
	out := &AuthCheck{
		SSOTokenURL: s.cfg.SSOTokenURL,
		SSOClientID: s.cfg.SSOClientID,
		Endpoint:    s.cfg.APIRoot(),
		APIProbe:    "skipped",
	}
	if err := s.api.VerifyCredentials(ctx); err != nil {
		out.TokenAcquire = "failed"
		out.Error = err.Error()
		return out
	}
	out.TokenAcquire = "ok"

	if !s.cfg.Allows(config.ResCase, config.LevelRead) {
		out.APIProbe = "skipped: cases are not readable in this configuration"
		return out
	}
	// The probe asks one question: does the API accept this credential.
	// So it caps the request at a single result and throws the response
	// away -- only the error matters. Nothing from the body can reach
	// AuthCheck, which is what makes this output safe to paste into a
	// bug report regardless of what the search happened to match.
	if _, err := s.api.FilterCases(ctx, &redhat.FilterCasesRequest{
		Keyword:    authProbeKeyword,
		MaxResults: 1,
	}); err != nil {
		out.APIProbe = "failed"
		out.Error = err.Error()
		return out
	}
	out.APIProbe = "ok"
	return out
}

func (s *Server) instructions() string {
	var b strings.Builder
	b.WriteString("Red Hat Support Case Management, scoped by the operator's configuration.\n")
	b.WriteString("Only the tools listed are permitted; anything not exposed is intentionally off-limits.\n")
	for _, r := range config.AllResources {
		fmt.Fprintf(&b, "- %s: %s\n", r, s.cfg.LevelFor(r))
	}
	if s.cfg.IsReadOnly() {
		b.WriteString("This server is read-only: it cannot create cases or post comments.\n")
	} else {
		b.WriteString("Writes reach Red Hat support and are visible to everyone on the account. " +
			"A posted comment cannot be withdrawn, so confirm the content with the user before sending it.\n")
	}
	fmt.Fprintf(&b, "Case numbers are digit strings with significant leading zeros (e.g. 04337263); pass them as strings.\n")
	fmt.Fprintf(&b, "Searches return at most %d cases per call; use offset to page.\n", s.cfg.MaxResults)
	return b.String()
}

// ---- registration helpers -------------------------------------------------

type toolDef[In any] struct {
	name         string
	title        string
	desc         string
	resource     config.Resource
	need         config.Level
	readOnlyHint bool
	handler      func(context.Context, In) (any, error)
}

// add registers a tool only if the configured scope permits it, and wraps
// the handler in the call-time re-check.
func add[In any](s *Server, d toolDef[In]) {
	if !s.cfg.Allows(d.resource, d.need) {
		return
	}
	destructive := d.need >= config.LevelWrite
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        d.name,
		Title:       d.title,
		Description: d.desc,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: d.readOnlyHint,
			// A case update or a comment is not destructive in the
			// delete sense, but it is externally visible and cannot be
			// taken back, which is the property a client's confirmation
			// prompt is actually there for.
			DestructiveHint: &destructive,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		if !s.cfg.Allows(d.resource, d.need) {
			return errorResult(scopeError(d.name, d.resource, d.need, s.cfg.LevelFor(d.resource))), nil, nil
		}
		out, err := d.handler(ctx, in)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return jsonResult(out), nil, nil
	})
	s.registered = append(s.registered, d.name)
}

func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Errorf("encode response: %w", err))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// errorResult reports a failure as tool output rather than as a protocol
// error, which is what lets the model read the message and correct a bad
// argument instead of only seeing that the call failed.
func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// scopeError is phrased to stop a retry loop: the answer will not change
// until a human edits the configuration.
func scopeError(tool string, r config.Resource, need, granted config.Level) error {
	return fmt.Errorf(
		"out of scope: %s needs %s access to %s but this server grants %s; "+
			"this is the operator's configuration, not a transient failure, so do not retry",
		tool, need, r, granted)
}

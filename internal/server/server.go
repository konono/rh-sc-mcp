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

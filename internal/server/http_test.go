package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
)

const (
	httpTestToken = "s3cr3t-shared-token-for-tests"
	// Distinct from the HTTP token on purpose: the two credentials must
	// never be the same string, and several tests turn on that.
	httpTestOfflineToken = "offline-token-for-http-tests"
)

// httpTestServer builds a server with the HTTP transport configured,
// pointed at the case-API stub so no real credential is involved.
func httpTestServer(t *testing.T, api *stubAPI, extraYAML string) *Server {
	t.Helper()
	sso := newSSO(t)

	body := "http:\n  addr: 127.0.0.1:0\n  token: " + httpTestToken + "\n" + extraYAML
	p := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RH_SC_OFFLINE_TOKEN", httpTestOfflineToken)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = api.URL
	cfg.SSOTokenURL = sso.URL
	return newWithClient(cfg, redhat.NewClient(cfg.BaseURL, cfg.APIVersion, cfg.SSOTokenURL,
		cfg.SSOClientID, cfg.OfflineToken, 5*time.Second))
}

func httpTestHandler(t *testing.T, api *stubAPI, extraYAML string) http.Handler {
	t.Helper()
	h, err := httpTestServer(t, api, extraYAML).Handler(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// initializeBody is the first message of the MCP handshake, which is
// enough to tell "the request reached the MCP handler" from "it was
// turned away".
func initializeBody() string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
		`"protocolVersion":"2025-06-18","capabilities":{},` +
		`"clientInfo":{"name":"test","version":"0"}}}`
}

func postMCP(t *testing.T, h http.Handler, token string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(initializeBody()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ---- authentication -------------------------------------------------------

// This is the whole reason the HTTP transport needs care: over stdio the
// operating system decides who can talk to the server, but an open port
// does not, and this process can reach an organisation's support cases.
func TestHTTPRequiresBearerToken(t *testing.T) {
	api := newStubAPI(t)
	h := httpTestHandler(t, api, "")

	for name, token := range map[string]string{
		"no token":                 "",
		"wrong token":              "not-the-token",
		"prefix of the real token": httpTestToken[:8],
		"the offline token":        httpTestOfflineToken,
	} {
		rec := postMCP(t, h, token, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "rhcase_") {
			t.Errorf("%s: an unauthenticated response disclosed tool names: %s", name, rec.Body.String())
		}
	}

	if rec := postMCP(t, h, httpTestToken, nil); rec.Code != http.StatusOK {
		t.Errorf("the correct token should be accepted, got %d: %s", rec.Code, rec.Body.String())
	}
	// Nothing rejected should have reached Red Hat.
	if n := api.requests.Load(); n != 0 {
		t.Errorf("%d requests reached the API during authentication tests", n)
	}
}

// A rejected request must not hint at the token's value or its length.
func TestUnauthorizedResponseDoesNotEchoTheToken(t *testing.T) {
	api := newStubAPI(t)
	rec := postMCP(t, httpTestHandler(t, api, ""), "guess", nil)
	body := rec.Body.String()
	if strings.Contains(body, httpTestToken) {
		t.Errorf("the response contained the expected token: %s", body)
	}
	if a := rec.Header().Get("WWW-Authenticate"); strings.Contains(a, httpTestToken) {
		t.Errorf("WWW-Authenticate contained the expected token: %s", a)
	}
}

// Serving without a token must be impossible to reach by construction,
// not merely rejected by config validation — a Config built in code
// never passes through that.
func TestHandlerRefusesToBuildWithoutAToken(t *testing.T) {
	api := newStubAPI(t)
	s := httpTestServer(t, api, "")
	s.cfg.HTTP.Token = ""
	if _, err := s.Handler(slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("Handler must refuse to build an unauthenticated MCP endpoint")
	}
}

// The same guard for the other way of getting it wrong. Config rejects
// this too; Handler repeats it because a hand-built Config does not go
// through Config's checks.
func TestHandlerRefusesToReuseTheOfflineToken(t *testing.T) {
	api := newStubAPI(t)
	s := httpTestServer(t, api, "")
	s.cfg.HTTP.Token = s.cfg.OfflineToken
	_, err := s.Handler(slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("Handler built an endpoint whose bearer token is the Red Hat offline token")
	}
	if strings.Contains(err.Error(), s.cfg.OfflineToken) {
		t.Errorf("the error quoted the credential: %v", err)
	}
}

// ---- probes ---------------------------------------------------------------

// Probes have to answer without a credential, or a kubelet cannot use
// them. They must disclose nothing, and — the point of the test — must
// not call Red Hat: a probe that did would spend a token exchange on
// every poll and flap on an upstream blip.
func TestProbesAreUnauthenticatedAndTouchNothing(t *testing.T) {
	api := newStubAPI(t)
	h := httpTestHandler(t, api, "")

	for _, path := range []string{HealthPath, ReadyPath} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, rec.Code)
		}
		for _, leak := range []string{httpTestToken, httpTestOfflineToken, "rhcase_", "redhat.com"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("%s leaked %q: %s", path, leak, rec.Body.String())
			}
		}
	}
	if n := api.requests.Load(); n != 0 {
		t.Fatalf("the probes made %d calls to the support API; they must not touch it", n)
	}
}

// ---- cross-origin ---------------------------------------------------------

// A browser page must not be able to drive this endpoint with whatever
// credentials the browser would attach, and a rebound DNS name must not
// reach a server bound to localhost.
func TestCrossOriginRequestsAreRejectedUnlessTrusted(t *testing.T) {
	api := newStubAPI(t)
	h := httpTestHandler(t, api, "")

	rec := postMCP(t, h, httpTestToken, map[string]string{"Origin": "https://evil.example"})
	if rec.Code == http.StatusOK {
		t.Fatalf("a cross-origin request was served: %d %s", rec.Code, rec.Body.String())
	}

	// The same request is served once the operator trusts that origin.
	trusted := httpTestHandler(t, api, "  trustedOrigins: [\"https://good.example\"]\n")
	ok := postMCP(t, trusted, httpTestToken, map[string]string{"Origin": "https://good.example"})
	if ok.Code != http.StatusOK {
		t.Fatalf("a trusted origin was rejected: %d %s", ok.Code, ok.Body.String())
	}
	// And a different origin still is not.
	bad := postMCP(t, trusted, httpTestToken, map[string]string{"Origin": "https://evil.example"})
	if bad.Code == http.StatusOK {
		t.Error("trusting one origin trusted all of them")
	}
}

// ---- sessions -------------------------------------------------------------

// Stateless is the default so several replicas can sit behind one
// Service without sticky routing.
func TestStatelessIsTheDefault(t *testing.T) {
	api := newStubAPI(t)
	if !httpTestServer(t, api, "").cfg.HTTP.IsStateless() {
		t.Error("HTTP mode should default to stateless")
	}
	off := httpTestServer(t, api, "  stateless: false\n  sessionTimeoutSeconds: 300\n")
	if off.cfg.HTTP.IsStateless() {
		t.Error("stateless: false should be honoured")
	}
	if off.cfg.HTTP.SessionTimeoutSeconds != 300 {
		t.Errorf("sessionTimeoutSeconds = %d", off.cfg.HTTP.SessionTimeoutSeconds)
	}
	if _, err := off.Handler(slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("stateful mode failed to build: %v", err)
	}
}

// ---- end to end over real HTTP --------------------------------------------

type bearerRoundTripper struct{ token string }

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// The transport changes; the policy does not. An authenticated client
// over HTTP must see exactly the scope-filtered toolset it would get
// over stdio, and calling a read tool must work end to end.
func TestHTTPServesTheSameScopedToolset(t *testing.T) {
	api := newStubAPI(t)
	s := httpTestServer(t, api, "")
	h, err := s.Handler(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:   ts.URL + MCPPath,
			HTTPClient: &http.Client{Transport: bearerRoundTripper{token: httpTestToken}},
		}, nil)
	if err != nil {
		t.Fatalf("connect over HTTP: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	if !got["rhcase_get"] || !got["rhcase_search"] {
		t.Error("the read tools should be exposed over HTTP")
	}
	// readOnly defaults to true, so these must be absent here exactly as
	// they are over stdio.
	for _, denied := range []string{"rhcase_create", "rhcase_update", "rhcase_add_comment"} {
		if got[denied] {
			t.Errorf("%s is exposed over HTTP despite the read-only default", denied)
		}
	}

	out, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "rhcase_get", Arguments: map[string]any{"caseNumber": "04337263"},
	})
	if err != nil {
		t.Fatalf("call tool over HTTP: %v", err)
	}
	if out.IsError {
		t.Fatalf("call failed: %v", out.Content)
	}
	// A denied tool must also be refused at call time over HTTP, not
	// merely hidden from the listing.
	denied, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "rhcase_add_comment",
		Arguments: map[string]any{
			"caseNumber": "04337263", "commentBody": "must not be posted",
		},
	})
	if err == nil && !denied.IsError {
		t.Fatal("a write tool was callable over HTTP on a read-only server")
	}
}

// Guards the response shape rather than only the status: a 200 that is
// not a JSON-RPC result would mean the handshake silently failed.
func TestInitializeReturnsAJSONRPCResult(t *testing.T) {
	api := newStubAPI(t)
	rec := postMCP(t, httpTestHandler(t, api, ""), httpTestToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	payload := rec.Body.String()
	// A streamed response arrives as SSE; take the data line if present.
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(payload, "\n") {
			if after, ok := strings.CutPrefix(line, "data: "); ok {
				payload = after
				break
			}
		}
	}
	var msg struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		t.Fatalf("response is not JSON-RPC: %v: %s", err, payload)
	}
	if msg.Result.ServerInfo.Name != "rh-sc-mcp" {
		t.Errorf("serverInfo.name = %q, want rh-sc-mcp", msg.Result.ServerInfo.Name)
	}
}

// ---- lifecycle ------------------------------------------------------------

func TestShutdownDrainsAndReturns(t *testing.T) {
	api := newStubAPI(t)
	s := httpTestServer(t, api, "")
	s.cfg.HTTP.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.ServeHTTP(ctx, slog.New(slog.DiscardHandler)) }()
	cancel()
	if err := <-done; err != nil {
		t.Errorf("shutdown returned %v, want nil", err)
	}
}

// ---- diagnostics ----------------------------------------------------------

// --print-scope output is pasted into bug reports, so it must describe
// the HTTP surface without disclosing the token.
func TestScopeReportDescribesHTTPWithoutTheToken(t *testing.T) {
	api := newStubAPI(t)
	s := httpTestServer(t, api, "")
	b, err := json.Marshal(s.ScopeReport())
	if err != nil {
		t.Fatal(err)
	}
	report := string(b)
	for _, secret := range []string{httpTestToken, httpTestOfflineToken} {
		if strings.Contains(report, secret) {
			t.Errorf("the scope report leaked a credential: %s", report)
		}
	}
	// Even a prefix would narrow a guess.
	if strings.Contains(report, httpTestToken[:8]) {
		t.Errorf("the scope report leaked a prefix of the token: %s", report)
	}
	var parsed struct {
		HTTP struct {
			Enabled         bool   `json:"enabled"`
			TokenConfigured bool   `json:"tokenConfigured"`
			Addr            string `json:"addr"`
			Path            string `json:"path"`
			Stateless       bool   `json:"stateless"`
		} `json:"http"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.HTTP.Enabled || !parsed.HTTP.TokenConfigured {
		t.Errorf("http section = %+v", parsed.HTTP)
	}
	if parsed.HTTP.Path != MCPPath || !parsed.HTTP.Stateless {
		t.Errorf("http section = %+v", parsed.HTTP)
	}
}

// With HTTP off, the report says so and still names no credential.
func TestScopeReportWithHTTPDisabled(t *testing.T) {
	api := newStubAPI(t)
	b, err := json.Marshal(testServer(t, api, nil).ScopeReport())
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		HTTP struct {
			Enabled         bool `json:"enabled"`
			TokenConfigured bool `json:"tokenConfigured"`
		} `json:"http"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.HTTP.Enabled || parsed.HTTP.TokenConfigured {
		t.Errorf("HTTP should be off by default: %+v", parsed.HTTP)
	}
}

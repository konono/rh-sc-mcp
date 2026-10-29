package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
)

// ---- harness --------------------------------------------------------------

// stubAPI is a fake Case Management API: one case, two comments, a
// filter endpoint and a KCS search. It records every path it was asked
// for, so a test can assert that a denied tool never reached the network.
type stubAPI struct {
	*httptest.Server
	hits     []string
	requests atomic.Int64
	// bodies maps "METHOD /path" to the last request body received.
	bodies map[string]string
}

func newStubAPI(t *testing.T) *stubAPI {
	t.Helper()
	s := &stubAPI{bodies: map[string]string{}}
	mux := http.NewServeMux()

	record := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s.requests.Add(1)
			s.hits = append(s.hits, r.Method+" "+r.URL.Path)
			if r.Body != nil {
				b, _ := io.ReadAll(r.Body)
				s.bodies[r.Method+" "+r.URL.Path] = string(b)
			}
			h(w, r)
		}
	}
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}

	mux.HandleFunc("/v3/cases/filter", record(func(w http.ResponseWriter, r *http.Request) {
		// Mirror the real service: a filter that only says how to page
		// is refused. Found by running against the live API on
		// 2026-10-25, and not described in the API document -- which is
		// why it has to be encoded here. A stub that accepts anything
		// let --check-auth ship with a probe the real API always
		// rejected.
		if !bodyHasFilterCriteria(s.bodies[r.Method+" "+r.URL.Path]) {
			reply(w, 400, map[string]any{
				"message": "No valid external filter criteria provided",
			})
			return
		}
		reply(w, 200, map[string]any{
			"totalCount": 3,
			"cases": []map[string]any{
				{
					"caseNumber": "04337263", "summary": "kernel panic on boot",
					"status": "Waiting on Red Hat", "severity": "2 (High)",
					"product": "Red Hat Enterprise Linux", "version": "9.4",
					"description": "a very long description that should not be in a search row",
					"comments":    []map[string]any{{"id": "c1", "commentBody": "also not in a search row"}},
				},
				{"caseNumber": "04337264", "summary": "slow I/O", "status": "Closed"},
			},
		})
	}))
	mux.HandleFunc("/v3/cases", record(func(w http.ResponseWriter, _ *http.Request) {
		reply(w, 201, map[string]any{
			"location": []string{"https://api.access.redhat.com/support/v3/cases/04400001"},
		})
	}))
	mux.HandleFunc("/v3/cases/04337263", record(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			reply(w, 200, map[string]any{
				"message":                   "Case updated",
				"fieldsNotSentToSalesforce": map[string]any{"hostname": "not forwarded"},
			})
			return
		}
		reply(w, 200, map[string]any{
			"caseNumber": "04337263", "summary": "kernel panic on boot",
			"description": "full description", "status": "Waiting on Red Hat",
			"severity": "2 (High)", "product": "Red Hat Enterprise Linux", "version": "9.4",
			"uri": "https://api.access.redhat.com/support/v3/cases/04337263",
			"comments": []map[string]any{
				{"id": "c1", "commentBody": "first", "createdBy": "alice"},
				{"id": "c2", "commentBody": "second", "createdBy": "bob"},
			},
		})
	}))
	mux.HandleFunc("/v3/cases/04337263/comments", record(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			reply(w, 201, map[string]any{
				"id": "c3", "caseNumber": "04337263", "commentBody": "posted",
				"contentType": "text/plain", "createdBy": "agent",
			})
			return
		}
		reply(w, 200, []map[string]any{
			{"id": "c1", "commentBody": "first"},
			{"id": "c2", "commentBody": "second"},
		})
	}))
	mux.HandleFunc("/v3/cases/04337263/comments/c1", record(func(w http.ResponseWriter, _ *http.Request) {
		reply(w, 200, map[string]any{"id": "c1", "commentBody": "first", "createdBy": "alice"})
	}))
	// The envelope and field set observed from the live endpoint on
	// 2026-10-25. The document models none of this ("200: Solr
	// Response"), so the stub is written from the measurement.
	mux.HandleFunc("/search/v2/kcs", record(func(w http.ResponseWriter, r *http.Request) {
		// Sending `exp` returns 500 from the real service, even though
		// the document lists it as required. Reproduced here so the
		// field can never come back.
		if strings.Contains(s.bodies[r.Method+" "+r.URL.Path], `"exp"`) {
			reply(w, 500, map[string]any{
				"message":       "Internal Server Error",
				"detailMessage": "unexpected parameter",
			})
			return
		}
		reply(w, 200, map[string]any{
			"responseHeader": map[string]any{"status": 0, "QTime": 12, "zkConnected": true},
			"spellcheck":     map[string]any{},
			"response": map[string]any{
				"numFound":      37134,
				"numFoundExact": true,
				"start":         0,
				"maxScore":      9.5,
				"docs": []map[string]any{{
					"id":               "7890",
					"publishedTitle":   "Configuring SELinux booleans",
					"allTitle":         "Configuring SELinux booleans",
					"title":            "Configuring SELinux booleans",
					"sortTitle":        "Configuring SELinux booleans",
					"documentKind":     "Solution",
					"language":         "en",
					"lastModifiedDate": "2026-09-01T00:00:00Z",
					"view_uri":         "https://access.redhat.com/solutions/7890",
					"resource_uri":     "https://api.access.redhat.com/support/solutions/7890",
					"uri":              "solutions/7890",
					// Declared next to the test that depends on both
					// its length and its encoding.
					"abstract": multiByteAbstract,
					// Internal fields the projection must drop.
					"_version_":                        1234567890,
					"granite_r2_vector_field_set":      true,
					"miniLM_vector_field_set":          true,
					"docs_text_store":                  strings.Repeat("x", 5000),
					"large_text_store":                 strings.Repeat("y", 5000),
					"vector_psfa_all-MiniLM-L6-v2_set": true,
				}},
			},
		})
	}))
	mux.HandleFunc("/", record(func(w http.ResponseWriter, r *http.Request) {
		reply(w, 404, map[string]any{"message": "no handler for " + r.URL.Path})
	}))

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// pagingOnlyFilterFields are the keys the real service does not count as
// search criteria: a body containing nothing else is rejected.
var pagingOnlyFilterFields = map[string]bool{
	"maxResults": true, "offset": true, "sortField": true, "sortOrder": true,
}

// bodyHasFilterCriteria reports whether a recorded filter body narrows
// the search, as opposed to only shaping the results.
func bodyHasFilterCriteria(body string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return false
	}
	for k := range m {
		if !pagingOnlyFilterFields[k] {
			return true
		}
	}
	return false
}

func (s *stubAPI) sawPath(p string) bool {
	for _, h := range s.hits {
		if strings.HasSuffix(h, " "+p) {
			return true
		}
	}
	return false
}

// newSSO returns a stub SSO realm so no real credential is needed.
func newSSO(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-access-token","expires_in":900}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testServer builds a Server against the stubs, with the config the test
// describes in YAML-equivalent fields.
func testServer(t *testing.T, api *stubAPI, mutate func(*config.Config)) *Server {
	t.Helper()
	t.Setenv("RH_SC_OFFLINE_TOKEN", "offline-token-for-tests")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	sso := newSSO(t)
	cfg.BaseURL = api.URL
	cfg.SSOTokenURL = sso.URL
	if mutate != nil {
		mutate(cfg)
	}
	client := redhat.NewClient(cfg.BaseURL, cfg.APIVersion, cfg.SSOTokenURL, cfg.SSOClientID,
		cfg.OfflineToken, time.Duration(cfg.TimeoutSeconds)*time.Second)
	return newWithClient(cfg, client)
}

// readWrite is the mutation that opens both write surfaces.
func readWrite(c *config.Config) {
	f := false
	c.ReadOnly = &f
	c.Resources = map[config.Resource]config.ResourceRule{
		config.ResCase:    {Permission: config.LevelWrite},
		config.ResComment: {Permission: config.LevelWrite},
	}
}

func connect(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	go func() { _ = s.mcp.Run(ctx, st) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, tl := range res.Tools {
		m[tl.Name] = true
	}
	return m
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// decodeResult parses a successful tool result into out, failing the
// test if the tool reported an error instead.
func decodeResult(t *testing.T, res *mcp.CallToolResult, out any) {
	t.Helper()
	text := resultText(t, res)
	if res.IsError {
		t.Fatalf("tool returned an error: %s", text)
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		t.Fatalf("decode result %q: %v", text, err)
	}
}

// ---- scope ----------------------------------------------------------------

// A read-only server must not merely refuse writes; the write tools must
// be absent from tools/list, so the model never proposes them.
func TestReadOnlyServerExposesNoWriteTools(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))
	names := toolNames(t, cs)

	for _, want := range []string{"rhcase_scope", "rhcase_search", "rhcase_get", "rhcase_list_comments", "rhcase_get_comment"} {
		if !names[want] {
			t.Errorf("read tool %s is missing", want)
		}
	}
	for _, unwanted := range []string{"rhcase_create", "rhcase_update", "rhcase_add_comment"} {
		if names[unwanted] {
			t.Errorf("write tool %s is exposed by a read-only server", unwanted)
		}
	}
	if names["rhkcs_search"] {
		t.Error("the KCS tool is exposed without being granted")
	}
}

func TestWriteToolsAppearWhenGranted(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))
	names := toolNames(t, cs)
	for _, want := range []string{"rhcase_create", "rhcase_update", "rhcase_add_comment"} {
		if !names[want] {
			t.Errorf("%s is missing even though writes are granted", want)
		}
	}
}

// Granting comment writes must not also grant case writes: the split
// exists so an operator can let an agent reply without letting it
// re-triage.
func TestCommentWriteDoesNotGrantCaseWrite(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, func(c *config.Config) {
		f := false
		c.ReadOnly = &f
		c.Resources = map[config.Resource]config.ResourceRule{
			config.ResComment: {Permission: config.LevelWrite},
		}
	}))
	names := toolNames(t, cs)
	if !names["rhcase_add_comment"] {
		t.Error("rhcase_add_comment is missing")
	}
	if names["rhcase_create"] || names["rhcase_update"] {
		t.Error("a comment grant leaked into the case tools")
	}
}

func TestKCSToolAppearsWhenGranted(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, func(c *config.Config) {
		c.Resources = map[config.Resource]config.ResourceRule{config.ResKCS: {Permission: config.LevelRead}}
	}))
	if !toolNames(t, cs)["rhkcs_search"] {
		t.Fatal("rhkcs_search is missing after being granted")
	}
}

// Registration is the first gate; this is the second. A client holding a
// stale tool list must still be refused, and the refusal must not reach
// the API.
func TestCallTimeCheckRefusesARevokedTool(t *testing.T) {
	api := newStubAPI(t)
	srv := testServer(t, api, readWrite)
	cs := connect(t, srv)

	// The operator tightens the configuration while the session is live.
	tr := true
	srv.cfg.ReadOnly = &tr

	res := call(t, cs, "rhcase_add_comment", map[string]any{
		"caseNumber": "04337263", "commentBody": "should never be posted",
	})
	if !res.IsError {
		t.Fatal("the call succeeded after the permission was withdrawn")
	}
	if !strings.Contains(resultText(t, res), "out of scope") {
		t.Errorf("unhelpful refusal: %s", resultText(t, res))
	}
	if api.sawPath("/v3/cases/04337263/comments") {
		t.Fatal("the refused call still reached the API")
	}
}

func TestScopeToolReportsTheConfiguration(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	var got scopeResult
	decodeResult(t, call(t, cs, "rhcase_scope", nil), &got)

	if !got.ReadOnly {
		t.Error("readOnly was reported false")
	}
	if got.Permissions["case"] != "read" || got.Permissions["kcs"] != "none" {
		t.Errorf("permissions = %v", got.Permissions)
	}
	for _, name := range got.Tools {
		if name == "rhcase_create" {
			t.Error("the tool list includes a tool that is not registered")
		}
	}
}

// ---- read tools -----------------------------------------------------------

func TestSearchReturnsCompactRows(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	var got searchResult
	decodeResult(t, call(t, cs, "rhcase_search", map[string]any{"keyword": "kernel panic"}), &got)

	if got.TotalCount != 3 || got.Returned != 2 {
		t.Fatalf("totalCount=%d returned=%d", got.TotalCount, got.Returned)
	}
	if got.Cases[0].CaseNumber != "04337263" || got.Cases[0].Summary != "kernel panic on boot" {
		t.Fatalf("first row = %#v", got.Cases[0])
	}
	// The stub returns a description and comments on each row; neither
	// belongs in a search result.
	raw := resultText(t, call(t, cs, "rhcase_search", map[string]any{"keyword": "kernel panic"}))
	for _, leaked := range []string{"should not be in a search row", "also not in a search row"} {
		if strings.Contains(raw, leaked) {
			t.Errorf("a search row carried the full case body: %s", raw)
		}
	}
	// More matched than were returned, so the caller is told how to page.
	if len(got.Notes) == 0 || !strings.Contains(strings.Join(got.Notes, " "), "offset") {
		t.Errorf("no paging note: %v", got.Notes)
	}
}

func TestSearchSendsTheCriteriaItWasGiven(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	call(t, cs, "rhcase_search", map[string]any{
		"keyword":       "disk",
		"statuses":      []string{"Waiting on Red Hat", "Waiting on Customer"},
		"includeClosed": true,
		"sortOrder":     "asc",
		"offset":        10,
	})
	body := api.bodies["POST /v3/cases/filter"]
	for _, want := range []string{`"keyword":"disk"`, `"includeClosed":true`, `"sortOrder":"ASC"`, `"offset":10`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body %s is missing %s", body, want)
		}
	}
}

func TestSearchClampsMaxResults(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, func(c *config.Config) { c.MaxResults = 5 }))

	var got searchResult
	decodeResult(t, call(t, cs, "rhcase_search", map[string]any{
		"keyword": "disk", "maxResults": 500,
	}), &got)

	if !strings.Contains(api.bodies["POST /v3/cases/filter"], `"maxResults":5`) {
		t.Fatalf("maxResults was not clamped in the request: %s", api.bodies["POST /v3/cases/filter"])
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "reduced") {
		t.Errorf("the clamp was silent: %v", got.Notes)
	}
}

func TestGetReturnsTheFullCase(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	var got getResult
	decodeResult(t, call(t, cs, "rhcase_get", map[string]any{"caseNumber": "04337263"}), &got)

	if got.Case.Description != "full description" {
		t.Errorf("description = %q", got.Case.Description)
	}
	if len(got.Case.Comments) != 2 {
		t.Errorf("comments = %d, want 2", len(got.Case.Comments))
	}
	// caseViewURL is unset in this config, so no invented link.
	if got.CaseURL != "" {
		t.Errorf("caseUrl = %q, want empty when no console prefix is configured", got.CaseURL)
	}
}

func TestGetCanOmitComments(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	var got getResult
	decodeResult(t, call(t, cs, "rhcase_get", map[string]any{
		"caseNumber": "04337263", "includeComments": false,
	}), &got)

	if len(got.Case.Comments) != 0 {
		t.Fatalf("comments were returned despite includeComments=false")
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "rhcase_list_comments") {
		t.Errorf("the omission did not say where to find them: %v", got.Notes)
	}
}

func TestGetTruncatesALongCommentHistory(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, func(c *config.Config) { c.MaxComments = 1 }))

	var got getResult
	decodeResult(t, call(t, cs, "rhcase_get", map[string]any{"caseNumber": "04337263"}), &got)

	if len(got.Case.Comments) != 1 {
		t.Fatalf("comments = %d, want the configured cap of 1", len(got.Case.Comments))
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "of 2 comments") {
		t.Errorf("truncation was silent or vague: %v", got.Notes)
	}
}

func TestCaseURLAppearsWhenAConsolePrefixIsConfigured(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, func(c *config.Config) {
		c.CaseViewURL = "https://console.test/support/cases"
	}))

	var got getResult
	decodeResult(t, call(t, cs, "rhcase_get", map[string]any{"caseNumber": "04337263"}), &got)
	if got.CaseURL != "https://console.test/support/cases/04337263" {
		t.Fatalf("caseUrl = %q", got.CaseURL)
	}
}

func TestListAndGetComment(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	var list listCommentsResult
	decodeResult(t, call(t, cs, "rhcase_list_comments", map[string]any{"caseNumber": "04337263"}), &list)
	if list.Count != 2 || list.Comments[0].ID != "c1" {
		t.Fatalf("list = %#v", list)
	}

	var one redhat.Comment
	decodeResult(t, call(t, cs, "rhcase_get_comment", map[string]any{
		"caseNumber": "04337263", "commentId": "c1",
	}), &one)
	if one.CreatedBy != "alice" {
		t.Fatalf("comment = %#v", one)
	}
}

// ---- write tools ----------------------------------------------------------

func TestCreateReportsTheCaseNumberAndLocation(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	var got createResult
	decodeResult(t, call(t, cs, "rhcase_create", map[string]any{
		"summary": "disk full", "description": "/var is full", "product": "RHEL", "version": "9.4",
	}), &got)

	if got.CaseNumber != "04400001" {
		t.Errorf("caseNumber = %q", got.CaseNumber)
	}
	if len(got.Location) != 1 {
		t.Errorf("location = %v", got.Location)
	}
	if got.HTTPStatus != 201 {
		t.Errorf("httpStatus = %d", got.HTTPStatus)
	}
	// The channel marker lets a human tell an agent-filed case apart.
	if !strings.Contains(api.bodies["POST /v3/cases"], `"origin":"API"`) {
		t.Errorf("origin was not set: %s", api.bodies["POST /v3/cases"])
	}
}

// When the number cannot be read from the location, the tool must say so
// rather than leave the model to invent one.
func TestCreateWithoutAReadableNumberSaysSo(t *testing.T) {
	api := newStubAPI(t)
	srv := testServer(t, api, readWrite)
	// Point the client at a server whose location is opaque.
	opaque := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"location":["https://api.access.redhat.com/support/v3/cases/5003a00001AbCd"]}`))
	}))
	defer opaque.Close()
	sso := newSSO(t)
	srv.api = redhat.NewClient(opaque.URL, "v3", sso.URL, "rhsm-api", "offline", 5*time.Second)
	cs := connect(t, srv)

	var got createResult
	decodeResult(t, call(t, cs, "rhcase_create", map[string]any{
		"summary": "s", "description": "d", "product": "p", "version": "v",
	}), &got)

	if got.CaseNumber != "" {
		t.Fatalf("an opaque id was reported as a case number: %q", got.CaseNumber)
	}
	if len(got.Location) != 1 {
		t.Fatalf("the location was dropped: %v", got.Location)
	}
	joined := strings.Join(got.Notes, " ")
	if !strings.Contains(joined, "could not be read") {
		t.Errorf("the gap was not explained: %v", got.Notes)
	}
}

func TestCreateRequiresTheDocumentedFields(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	for _, missing := range []string{"summary", "description", "product", "version"} {
		args := map[string]any{"summary": "s", "description": "d", "product": "p", "version": "v"}
		args[missing] = "  " // whitespace is not a value
		res := call(t, cs, "rhcase_create", args)
		if !res.IsError {
			t.Errorf("create succeeded with an empty %s", missing)
			continue
		}
		if !strings.Contains(resultText(t, res), missing) {
			t.Errorf("the error does not name %s: %s", missing, resultText(t, res))
		}
	}
	if api.sawPath("/v3/cases") {
		t.Fatal("an invalid create still reached the API")
	}
}

func TestUpdateSendsOnlyWhatWasGiven(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	var got updateResult
	decodeResult(t, call(t, cs, "rhcase_update", map[string]any{
		"caseNumber": "04337263", "status": "Closed", "notes": "",
	}), &got)

	body := api.bodies["PUT /v3/cases/04337263"]
	if !strings.Contains(body, `"status":"Closed"`) {
		t.Errorf("status missing from %s", body)
	}
	if !strings.Contains(body, `"notes":""`) {
		t.Errorf("an explicit clear was dropped from %s", body)
	}
	if strings.Contains(body, `"severity"`) {
		t.Errorf("an untouched field was sent: %s", body)
	}
	// The API said it did not forward hostname; that must not be silent.
	if got.FieldsNotSentToSalesforce["hostname"] == nil {
		t.Error("fieldsNotSentToSalesforce was dropped")
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "not applied") {
		t.Errorf("the partial update was not flagged: %v", got.Notes)
	}
}

// A PUT with nothing in it would report success while changing nothing.
func TestUpdateWithNoFieldsIsRefused(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	res := call(t, cs, "rhcase_update", map[string]any{"caseNumber": "04337263"})
	if !res.IsError {
		t.Fatal("an empty update was accepted")
	}
	if api.sawPath("/v3/cases/04337263") {
		t.Fatal("an empty update still reached the API")
	}
}

func TestUpdateRefusesToClearTheSummary(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	res := call(t, cs, "rhcase_update", map[string]any{"caseNumber": "04337263", "summary": ""})
	if !res.IsError || !strings.Contains(resultText(t, res), "summary") {
		t.Fatalf("clearing the summary was allowed: %v", resultText(t, res))
	}
}

func TestAddCommentDefaultsContentTypeAndReturnsTheComment(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	var got addCommentResult
	decodeResult(t, call(t, cs, "rhcase_add_comment", map[string]any{
		"caseNumber": "04337263", "commentBody": "we are investigating",
	}), &got)

	body := api.bodies["POST /v3/cases/04337263/comments"]
	if !strings.Contains(body, `"contentType":"plaintext"`) {
		t.Errorf("contentType was not defaulted: %s", body)
	}
	if got.Comment == nil || got.Comment.ID != "c3" {
		t.Fatalf("comment = %#v", got.Comment)
	}
}

func TestAddCommentValidatesEnums(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	for _, args := range []map[string]any{
		{"caseNumber": "04337263", "commentBody": "x", "contentType": "markdown"},
		{"caseNumber": "04337263", "commentBody": "x", "createdByType": "Robot"},
		{"caseNumber": "04337263", "commentBody": "   "},
	} {
		res := call(t, cs, "rhcase_add_comment", args)
		if !res.IsError {
			t.Errorf("invalid arguments were accepted: %v", args)
		}
	}
	if api.sawPath("/v3/cases/04337263/comments") {
		t.Fatal("an invalid comment still reached the API")
	}
}

// Case-insensitive enum input is normalised to the documented spelling
// rather than rejected.
func TestEnumsAreCaseInsensitive(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	res := call(t, cs, "rhcase_add_comment", map[string]any{
		"caseNumber": "04337263", "commentBody": "x", "contentType": "HTML", "createdByType": "customer",
	})
	if res.IsError {
		t.Fatalf("a differently-cased enum was rejected: %s", resultText(t, res))
	}
	body := api.bodies["POST /v3/cases/04337263/comments"]
	if !strings.Contains(body, `"contentType":"html"`) || !strings.Contains(body, `"createdByType":"Customer"`) {
		t.Fatalf("enums were not normalised: %s", body)
	}
}

// ---- argument validation at the MCP boundary -------------------------------

func TestBadCaseNumbersAreRefusedByEveryTool(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))

	bad := []string{"../../accounts/current", "04337263/comments", "abc", "", "04337263?x=1"}
	tools := map[string]func(string) map[string]any{
		"rhcase_get":           func(n string) map[string]any { return map[string]any{"caseNumber": n} },
		"rhcase_list_comments": func(n string) map[string]any { return map[string]any{"caseNumber": n} },
		"rhcase_get_comment":   func(n string) map[string]any { return map[string]any{"caseNumber": n, "commentId": "c1"} },
		"rhcase_update":        func(n string) map[string]any { return map[string]any{"caseNumber": n, "status": "Closed"} },
		"rhcase_add_comment":   func(n string) map[string]any { return map[string]any{"caseNumber": n, "commentBody": "x"} },
	}
	before := api.requests.Load()
	for tool, mk := range tools {
		for _, n := range bad {
			res := call(t, cs, tool, mk(n))
			if !res.IsError {
				t.Errorf("%s accepted case number %q", tool, n)
			}
		}
	}
	if after := api.requests.Load(); after != before {
		t.Fatalf("%d requests reached the API during validation failures", after-before)
	}
}

func TestInvalidDatesAndSortsAreRefused(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	for name, args := range map[string]map[string]any{
		"search startDate":   {"startDate": "last tuesday"},
		"search sortOrder":   {"sortOrder": "sideways"},
		"search sortField":   {"sortField": "CreatedDate; DROP TABLE"},
		"search offset":      {"offset": -5},
		"comments startDate": {"caseNumber": "04337263", "startDate": "2026-13-45"},
		"comments sortField": {"caseNumber": "04337263", "sortField": "nonsense"},
		"comments sortOrder": {"caseNumber": "04337263", "sortOrder": "up"},
	} {
		tool := "rhcase_search"
		if strings.HasPrefix(name, "comments") {
			tool = "rhcase_list_comments"
		}
		res := call(t, cs, tool, args)
		if !res.IsError {
			t.Errorf("%s: invalid argument accepted (%v)", name, args)
		}
	}
}

func TestAcceptedDateFormats(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	for _, d := range []string{"2026-01-31", "2026-01-31T00:00:00Z", "2026-01-31T00:00:00+09:00", "2026-01-31T00:00:00"} {
		res := call(t, cs, "rhcase_search", map[string]any{"startDate": d})
		if res.IsError {
			t.Errorf("startDate %q was rejected: %s", d, resultText(t, res))
		}
	}
}

// ---- failure surfaces -----------------------------------------------------

func TestUpstreamErrorsReachTheModelWithoutCredentials(t *testing.T) {
	sso := newSSO(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = fmt.Fprintf(w, `{"message":"Account mismatch.","detailMessage":"sent %s"}`,
			r.Header.Get("Authorization"))
	}))
	defer api.Close()

	t.Setenv("RH_SC_OFFLINE_TOKEN", "offline-token-for-tests")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = api.URL
	cfg.SSOTokenURL = sso.URL
	srv := newWithClient(cfg, redhat.NewClient(api.URL, "v3", sso.URL, "rhsm-api",
		"offline-token-for-tests", 5*time.Second))
	cs := connect(t, srv)

	res := call(t, cs, "rhcase_get", map[string]any{"caseNumber": "04337263"})
	if !res.IsError {
		t.Fatal("a 403 was reported as success")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Account mismatch.") {
		t.Errorf("the upstream message was lost: %s", text)
	}
	if strings.Contains(text, "test-access-token") {
		t.Errorf("the bearer token leaked to the model: %s", text)
	}
}

// A 2xx carrying an HTML page is the case the scrubbing of error
// responses does not cover, and a proxy sign-in page is precisely where
// a reflected Authorization header shows up. These two tests walk the
// production path — real SSO exchange, real client, real MCP round trip
// — and assert that neither credential reaches the model.
const (
	liveAccessToken  = "live-access-token-ABCDEFGHIJ"
	liveOfflineToken = "live-offline-token-KLMNOPQRST"
)

// reflectingAPI answers every request with a 200 HTML page that quotes
// the Authorization header it received and the offline token.
func reflectingAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w,
			"<html><body>Session expired, please sign in.<!-- request had %s ; offline=%s --></body></html>",
			r.Header.Get("Authorization"), liveOfflineToken)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serverAgainst builds a Server whose client really exchanges the
// offline token with a stub SSO realm, so the access token in play is a
// value the test knows and can search the output for.
func serverAgainst(t *testing.T, apiURL string, mutate func(*config.Config)) *Server {
	t.Helper()
	sso := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"expires_in":900}`, liveAccessToken)
	}))
	t.Cleanup(sso.Close)

	t.Setenv("RH_SC_OFFLINE_TOKEN", liveOfflineToken)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = apiURL
	cfg.SSOTokenURL = sso.URL
	if mutate != nil {
		mutate(cfg)
	}
	return newWithClient(cfg, redhat.NewClient(cfg.BaseURL, cfg.APIVersion, cfg.SSOTokenURL,
		cfg.SSOClientID, cfg.OfflineToken, 5*time.Second))
}

func assertNoCredentials(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, liveAccessToken) {
		t.Errorf("the access token reached the model: %s", text)
	}
	if strings.Contains(text, liveOfflineToken) {
		t.Errorf("the offline token reached the model: %s", text)
	}
}

func TestHTMLSuccessPageOnGetIsAnErrorWithoutCredentials(t *testing.T) {
	cs := connect(t, serverAgainst(t, reflectingAPI(t).URL, nil))

	res := call(t, cs, "rhcase_get", map[string]any{"caseNumber": "04337263"})
	text := resultText(t, res)
	if !res.IsError {
		t.Fatalf("an HTML sign-in page was reported as a case: %s", text)
	}
	assertNoCredentials(t, text)
	if !strings.Contains(text, "Session expired") {
		t.Errorf("the diagnostic was lost entirely: %s", text)
	}
}

// The same page on a create must not read as "created, location
// unknown": the model's next move would be to hunt for a case that does
// not exist, or to open a second one.
func TestHTMLSuccessPageOnCreateIsAnErrorWithoutCredentials(t *testing.T) {
	cs := connect(t, serverAgainst(t, reflectingAPI(t).URL, readWrite))

	res := call(t, cs, "rhcase_create", map[string]any{
		"summary": "disk full", "description": "/var is full", "product": "RHEL", "version": "9.4",
	})
	text := resultText(t, res)
	if !res.IsError {
		t.Fatalf("an HTML sign-in page was reported as a created case: %s", text)
	}
	assertNoCredentials(t, text)
}

// The same page on a comment post, which is the write that cannot be
// undone and so the one where a false success is worst.
func TestHTMLSuccessPageOnAddCommentIsAnErrorWithoutCredentials(t *testing.T) {
	cs := connect(t, serverAgainst(t, reflectingAPI(t).URL, readWrite))

	res := call(t, cs, "rhcase_add_comment", map[string]any{
		"caseNumber": "04337263", "commentBody": "we are investigating",
	})
	text := resultText(t, res)
	if !res.IsError {
		t.Fatalf("an HTML sign-in page was reported as a posted comment: %s", text)
	}
	assertNoCredentials(t, text)
}

// A 404 reads as "no such case", which is a different thing from a
// failure the model should retry.
func TestMissingCaseIsReportedAsSuch(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	res := call(t, cs, "rhcase_get", map[string]any{"caseNumber": "09999999"})
	if !res.IsError {
		t.Fatal("a missing case was reported as success")
	}
	if !strings.Contains(resultText(t, res), "404") {
		t.Errorf("the status is not visible: %s", resultText(t, res))
	}
}

// ---- --check-auth ---------------------------------------------------------

func TestCheckAuthSucceedsAgainstAWorkingEndpoint(t *testing.T) {
	api := newStubAPI(t)
	got := testServer(t, api, nil).CheckAuth(context.Background())

	if !got.OK() {
		t.Fatalf("check failed: %+v", got)
	}
	if got.TokenAcquire != "ok" || got.APIProbe != "ok" {
		t.Fatalf("tokenExchange=%q apiProbe=%q", got.TokenAcquire, got.APIProbe)
	}
	// The probe must stay small; it exists to prove the token works,
	// not to fetch anything.
	body := api.bodies["POST /v3/cases/filter"]
	if !strings.Contains(body, `"maxResults":1`) {
		t.Errorf("the probe asked for more than one case: %s", body)
	}
	// And it must narrow the search, or the real service refuses it.
	if !bodyHasFilterCriteria(body) {
		t.Errorf("the probe sent no filter criteria, which the real API rejects: %s", body)
	}
}

// The probe's search term has to be well formed: long enough to be
// specific, and plain enough to survive being used as a search term.
//
// This asserts the shape only. Whether the term matches anything is a
// fact about the data on a given day, not something a test can pin --
// and it is not what keeps case content out of the diagnostic either.
// That is CheckAuth capping the request at one result and discarding
// the response, which TestCheckAuthOutputCarriesNoCredential and the
// probe's own assertions cover.
func TestAuthProbeKeywordIsWellFormed(t *testing.T) {
	if len(authProbeKeyword) < 16 {
		t.Fatalf("probe keyword %q is too short to be a specific search term", authProbeKeyword)
	}
	for _, r := range authProbeKeyword {
		lower := r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'
		if !lower && !digit {
			t.Fatalf("probe keyword %q contains %q; the keyword becomes a SOSL FIND term, "+
				"so it must stay plain alphanumeric", authProbeKeyword, r)
		}
	}
}

// What actually keeps case content out of the diagnostic: the response
// is discarded. The stub answers the probe with cases regardless of the
// search term, so this exercises the matching case directly rather than
// relying on the term finding nothing.
func TestCheckAuthDiscardsCaseDataEvenWhenTheProbeMatches(t *testing.T) {
	api := newStubAPI(t)
	got := testServer(t, api, nil).CheckAuth(context.Background())

	if !got.OK() {
		t.Fatalf("check failed: %+v", got)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	// Everything the stub would have returned for this probe.
	for _, leaked := range []string{
		"kernel panic", "04337263", "04337264", "Waiting on Red Hat",
		"a very long description", "slow I/O",
	} {
		if strings.Contains(string(b), leaked) {
			t.Errorf("case data reached the diagnostic output (%q): %s", leaked, b)
		}
	}
}

// The real service refuses a filter that only says how to page, with a
// message that does not name the missing argument. This is pinned
// because --check-auth shipped with exactly that body and the stub,
// being more permissive than the service, accepted it.
func TestFilterWithoutCriteriaIsRejectedTheWayTheRealAPIRejectsIt(t *testing.T) {
	api := newStubAPI(t)
	srv := testServer(t, api, nil)

	// Straight at the client, bypassing the tool-level guard, to show
	// what the body the probe used to send actually gets back.
	_, err := srv.api.FilterCases(context.Background(), &redhat.FilterCasesRequest{MaxResults: 1})
	if err == nil {
		t.Fatal("a criteria-less filter was accepted; the stub no longer matches the real service")
	}
	if !strings.Contains(err.Error(), "No valid external filter criteria") {
		t.Fatalf("unexpected error: %v", err)
	}

	// And the request the probe really sends is accepted.
	if _, err := srv.api.FilterCases(context.Background(), &redhat.FilterCasesRequest{
		Keyword: authProbeKeyword, MaxResults: 1,
	}); err != nil {
		t.Fatalf("the probe's own request was rejected: %v", err)
	}
}

// A search with nothing to search on is refused before it is sent, with
// a message that says what to add -- rather than passing the service's
// opaque 400 back to the model.
func TestSearchWithoutCriteriaIsRefusedLocally(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, nil))

	for _, args := range []map[string]any{
		{},
		{"maxResults": 10},
		{"offset": 20, "sortOrder": "ASC", "sortField": "CreatedDate"},
	} {
		res := call(t, cs, "rhcase_search", args)
		if !res.IsError {
			t.Errorf("a criteria-less search was accepted: %v", args)
			continue
		}
		if !strings.Contains(resultText(t, res), "keyword") {
			t.Errorf("the error does not say what to add: %s", resultText(t, res))
		}
	}
	if api.sawPath("/v3/cases/filter") {
		t.Error("a criteria-less search still reached the API")
	}
}

// SSO and API failures have different causes and different fixes, so
// they must not collapse into one "auth failed".
func TestCheckAuthDistinguishesSSOFromAPIFailure(t *testing.T) {
	t.Run("SSO rejects the offline token", func(t *testing.T) {
		api := newStubAPI(t)
		srv := testServer(t, api, nil)
		dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid refresh token"}`))
		}))
		defer dead.Close()
		srv.api = redhat.NewClient(api.URL, "v3", dead.URL, "rhsm-api", liveOfflineToken, 5*time.Second)

		got := srv.CheckAuth(context.Background())
		if got.OK() {
			t.Fatal("a rejected offline token was reported as working")
		}
		if got.TokenAcquire != "failed" {
			t.Errorf("tokenExchange = %q", got.TokenAcquire)
		}
		if got.APIProbe != "skipped" {
			t.Errorf("the API was probed after the exchange failed: %q", got.APIProbe)
		}
		if !strings.Contains(got.Error, "invalid_grant") {
			t.Errorf("the cause is not visible: %q", got.Error)
		}
		assertNoCredentials(t, got.Error)
	})

	t.Run("API rejects the issued token", func(t *testing.T) {
		forbidding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Account mismatch."}`))
		}))
		defer forbidding.Close()

		got := serverAgainst(t, forbidding.URL, nil).CheckAuth(context.Background())
		if got.OK() {
			t.Fatal("a forbidden account was reported as working")
		}
		if got.TokenAcquire != "ok" {
			t.Errorf("the exchange should have succeeded: %q", got.TokenAcquire)
		}
		if got.APIProbe != "failed" {
			t.Errorf("apiProbe = %q", got.APIProbe)
		}
		if !strings.Contains(got.Error, "Account mismatch.") {
			t.Errorf("the cause is not visible: %q", got.Error)
		}
	})
}

// The whole point of this output is that it can be pasted into a bug
// report, so nothing credential-shaped may appear anywhere in it.
func TestCheckAuthOutputCarriesNoCredential(t *testing.T) {
	// A failing upstream that reflects the Authorization header is the
	// worst case for this.
	got := serverAgainst(t, reflectingAPI(t).URL, nil).CheckAuth(context.Background())

	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCredentials(t, string(b))
}

func TestCheckAuthSkipsTheProbeWhenCasesAreUnreadable(t *testing.T) {
	api := newStubAPI(t)
	got := testServer(t, api, func(c *config.Config) {
		c.Resources = map[config.Resource]config.ResourceRule{config.ResCase: {Permission: config.LevelNone}}
	}).CheckAuth(context.Background())

	if !got.OK() {
		t.Fatalf("check failed: %+v", got)
	}
	if got.TokenAcquire != "ok" {
		t.Errorf("tokenExchange = %q", got.TokenAcquire)
	}
	if !strings.HasPrefix(got.APIProbe, "skipped") {
		t.Errorf("apiProbe = %q, want a skip with a reason", got.APIProbe)
	}
	if api.sawPath("/v3/cases/filter") {
		t.Error("the probe ran even though cases are not readable")
	}
}

// ---- KCS ------------------------------------------------------------------

// kcsServer is a server with the KCS surface granted.
func kcsServer(t *testing.T, api *stubAPI) *mcp.ClientSession {
	t.Helper()
	return connect(t, testServer(t, api, func(c *config.Config) {
		c.Resources = map[config.Resource]config.ResourceRule{config.ResKCS: {Permission: config.LevelRead}}
	}))
}

func TestKCSSearchProjectsDocuments(t *testing.T) {
	api := newStubAPI(t)
	cs := kcsServer(t, api)

	var got kcsSearchResult
	decodeResult(t, call(t, cs, "rhkcs_search", map[string]any{"query": "selinux", "rows": 5}), &got)

	// The search endpoint has no version segment.
	if !api.sawPath("/search/v2/kcs") {
		t.Fatalf("the KCS request went somewhere unexpected: %v", api.hits)
	}
	if !strings.Contains(api.bodies["POST /search/v2/kcs"], `"rows":5`) {
		t.Errorf("rows was not sent: %s", api.bodies["POST /search/v2/kcs"])
	}
	if got.NumFound != 37134 || got.Returned != 1 {
		t.Fatalf("numFound=%d returned=%d", got.NumFound, got.Returned)
	}
	if got.NumFoundExact == nil || !*got.NumFoundExact {
		t.Errorf("numFoundExact was not carried through: %v", got.NumFoundExact)
	}
	d := got.Documents[0]
	if d.ID != "7890" || d.DocumentKind != "Solution" || d.Language != "en" {
		t.Errorf("document = %#v", d)
	}
	// One title, not the four the backend sends.
	if d.Title != "Configuring SELinux booleans" {
		t.Errorf("title = %q", d.Title)
	}
	// The API's own value, passed through.
	if d.ViewURI != "https://access.redhat.com/solutions/7890" {
		t.Errorf("viewUri = %q", d.ViewURI)
	}
	// The paging note should fire: numFound 37134, 1 returned. It is
	// phrased as what numFound says rather than as a claim that more
	// results exist, because the backend may report numFound as an
	// estimate and does not always say when it did.
	notes := strings.Join(got.Notes, " ")
	if !strings.Contains(notes, "start=1") {
		t.Errorf("no paging note: %v", got.Notes)
	}
	if strings.Contains(notes, "more results exist") {
		t.Errorf("the paging note asserts more results exist, which numFound does not establish: %v", got.Notes)
	}
	// Nor should the truncation note promise that viewUri reaches the
	// full text; neither that it opens nor what it leads to is verified.
	if strings.Contains(notes, "full text") {
		t.Errorf("the truncation note promises the full text: %v", got.Notes)
	}
}

// multiByteAbstract is the abstract the KCS stub serves. Two properties
// of it matter, and the test below asserts on both.
//
// It is longer than the snippet limit, as the live abstract was, so
// every response exercises truncation.
//
// Its characters are not all one byte. The limit is counted in runes
// (redhat.KCSSnippetRunes), and against an all-ASCII abstract the
// rune-count assertion would pass whether the implementation counted
// runes or bytes, because there the two are the same number. The ASCII
// prefix keeps the string legible at a glance; the katakana is what
// makes a byte-based cut come up short.
var multiByteAbstract = strings.Repeat("multibyte-マルチバイト-", 60)

// The measured problem: one live document was 10 KB, 93% of it the
// abstract, and the backend's default of ten rows made a 39 KB result.
// The projection has to drop the bulk and say that it did.
func TestKCSSearchDropsBulkFieldsAndCutsTheSnippet(t *testing.T) {
	api := newStubAPI(t)
	cs := kcsServer(t, api)

	res := call(t, cs, "rhkcs_search", map[string]any{"query": "selinux"})
	text := resultText(t, res)
	if res.IsError {
		t.Fatalf("search failed: %s", text)
	}

	// Internal fields carry most of the weight and none of the meaning.
	for _, dropped := range []string{
		"docs_text_store", "large_text_store", "_version_",
		"vector_field_set", "resource_uri", "sortTitle", "maxScore",
	} {
		if strings.Contains(text, dropped) {
			t.Errorf("%q survived the projection: result is %d bytes", dropped, len(text))
		}
	}

	var got kcsSearchResult
	decodeResult(t, res, &got)
	d := got.Documents[0]
	if !d.SnippetTruncated {
		t.Fatalf("an abstract of %d runes was not reported as truncated",
			utf8.RuneCountInString(multiByteAbstract))
	}
	// In runes, not bytes -- see multiByteAbstract above.
	if n := utf8.RuneCountInString(d.Snippet); n != redhat.KCSSnippetRunes() {
		t.Errorf("snippet is %d runes, want %d", n, redhat.KCSSnippetRunes())
	}
	if !utf8.ValidString(d.Snippet) {
		t.Error("the snippet was cut mid-rune")
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "cut to") {
		t.Errorf("truncation was silent: %v", got.Notes)
	}
}

// `exp` is listed as required by the document and returns 500 from the
// service. It must never appear in a request.
func TestKCSSearchNeverSendsExp(t *testing.T) {
	api := newStubAPI(t)
	cs := kcsServer(t, api)

	res := call(t, cs, "rhkcs_search", map[string]any{
		"query": "selinux", "expression": "sort=score%20DESC",
	})
	if res.IsError {
		t.Fatalf("search failed: %s", resultText(t, res))
	}
	body := api.bodies["POST /search/v2/kcs"]
	if strings.Contains(body, `"exp"`) {
		t.Fatalf("the request carried exp, which the real service answers with 500: %s", body)
	}
	if !strings.Contains(body, `"expression"`) {
		t.Errorf("expression was not sent: %s", body)
	}
}

// The response is unspecified, so an unfamiliar shape is a foreseeable
// outcome. It must be reported precisely and without the body.
func TestKCSSearchReportsAnUnrecognizedShapeWithoutTheBody(t *testing.T) {
	secret := "confidential-article-text-that-must-not-be-echoed"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Plausible but not the envelope: no response.docs.
		_, _ = fmt.Fprintf(w, `{"results":[{"body":%q}],"meta":{"total":1}}`, secret)
	}))
	defer backend.Close()

	srv := serverAgainst(t, backend.URL, func(c *config.Config) {
		c.Resources = map[config.Resource]config.ResourceRule{config.ResKCS: {Permission: config.LevelRead}}
	})
	cs := connect(t, srv)

	res := call(t, cs, "rhkcs_search", map[string]any{"query": "selinux"})
	text := resultText(t, res)
	if res.IsError {
		t.Fatalf("an unrecognized shape should not be an error: %s", text)
	}
	if strings.Contains(text, secret) {
		t.Fatalf("the unrecognized body was echoed back: %s", text)
	}

	var got kcsSearchResult
	decodeResult(t, res, &got)
	if got.Unrecognized == nil {
		t.Fatal("an unfamiliar shape was projected as if it were understood")
	}
	if len(got.Documents) != 0 {
		t.Errorf("documents were invented from an unknown shape: %#v", got.Documents)
	}
	// Enough for a human to go and look, and nothing more.
	if got.Unrecognized.SizeBytes == 0 {
		t.Error("no size reported")
	}
	want := map[string]bool{"results": true, "meta": true}
	for _, k := range got.Unrecognized.TopLevelKeys {
		delete(want, k)
	}
	if len(want) != 0 {
		t.Errorf("topLevelKeys = %v, missing %v", got.Unrecognized.TopLevelKeys, want)
	}
}

func TestKCSSearchClampsRows(t *testing.T) {
	api := newStubAPI(t)
	cs := kcsServer(t, api)

	var got kcsSearchResult
	decodeResult(t, call(t, cs, "rhkcs_search", map[string]any{"query": "selinux", "rows": 500}), &got)

	if !strings.Contains(api.bodies["POST /search/v2/kcs"], fmt.Sprintf(`"rows":%d`, kcsMaxRows)) {
		t.Fatalf("rows was not clamped to %d: %s", kcsMaxRows, api.bodies["POST /search/v2/kcs"])
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "reduced") {
		t.Errorf("the clamp was silent: %v", got.Notes)
	}
}

func TestKCSSearchRequiresAQuery(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, func(c *config.Config) {
		c.Resources = map[config.Resource]config.ResourceRule{config.ResKCS: {Permission: config.LevelRead}}
	}))
	if res := call(t, cs, "rhkcs_search", map[string]any{"query": "  "}); !res.IsError {
		t.Fatal("an empty query was accepted")
	}
}

// ---- protocol surface -----------------------------------------------------

// The instructions are the only place the model learns the boundary
// before it starts guessing, so they have to state it.
func TestInstructionsStateTheBoundary(t *testing.T) {
	api := newStubAPI(t)
	ro := testServer(t, api, nil).instructions()
	if !strings.Contains(ro, "read-only") {
		t.Errorf("read-only instructions do not say so: %q", ro)
	}
	rw := testServer(t, api, readWrite).instructions()
	if !strings.Contains(rw, "cannot be withdrawn") {
		t.Errorf("writable instructions do not warn about visibility: %q", rw)
	}
}

// Every tool has to carry a schema and a description, or the model is
// choosing between names alone.
func TestToolsAreDescribedAndTyped(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("no tools")
	}
	for _, tl := range res.Tools {
		if len(tl.Description) < 20 {
			t.Errorf("%s has no useful description", tl.Name)
		}
		if tl.InputSchema == nil {
			t.Errorf("%s has no input schema", tl.Name)
		}
		if tl.Annotations == nil {
			t.Errorf("%s has no annotations", tl.Name)
			continue
		}
		// A write tool must not claim to be read-only: a client uses
		// that hint to decide whether to ask the user first.
		switch tl.Name {
		case "rhcase_create", "rhcase_update", "rhcase_add_comment":
			if tl.Annotations.ReadOnlyHint {
				t.Errorf("%s is annotated read-only", tl.Name)
			}
		default:
			if !tl.Annotations.ReadOnlyHint {
				t.Errorf("%s is not annotated read-only", tl.Name)
			}
		}
	}
}

// caseNumber is required, so the generated schema must say so — a client
// that validates against it should catch the omission before the call.
func TestRequiredFieldsAppearInTheSchema(t *testing.T) {
	api := newStubAPI(t)
	cs := connect(t, testServer(t, api, readWrite))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"rhcase_get":         {"caseNumber"},
		"rhcase_get_comment": {"caseNumber", "commentId"},
		"rhcase_create":      {"summary", "description", "product", "version"},
		"rhcase_add_comment": {"caseNumber", "commentBody"},
		"rhkcs_search":       nil,
	}
	for _, tl := range res.Tools {
		req, ok := want[tl.Name]
		if !ok || req == nil {
			continue
		}
		// InputSchema is carried as an opaque value, so read it the way
		// a client would: as JSON.
		raw, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Fatalf("%s: marshal schema: %v", tl.Name, err)
		}
		var schema struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: decode schema: %v", tl.Name, err)
		}
		have := map[string]bool{}
		for _, r := range schema.Required {
			have[r] = true
		}
		for _, r := range req {
			if !have[r] {
				t.Errorf("%s: %q is not marked required (required=%v)", tl.Name, r, schema.Required)
			}
		}
	}
}

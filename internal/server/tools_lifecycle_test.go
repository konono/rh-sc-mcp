package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
)

const lifecycleStubBody = `{
  "data": [{
    "name": "Red Hat Ansible Automation Platform",
    "is_retired": false,
    "link": "https://access.redhat.com/support/policy/updates/ansible-automation-platform",
    "versions": [
      {"name": "2.7", "type": "Full Support", "phases": [
        {"name": "General availability", "start_date": "N/A", "start_date_format": "string",
         "end_date": "2026-06-02T00:00:00.000Z", "end_date_format": "date"},
        {"name": "Full support", "start_date": "2026-06-02T00:00:00.000Z", "start_date_format": "date",
         "end_date": "2027-06-02T00:00:00.000Z", "end_date_format": "date"}]},
      {"name": "2.6", "type": "Full Support", "phases": [
        {"name": "Full support", "start_date": "2025-10-01T00:00:00.000Z", "start_date_format": "date",
         "end_date": "2026-10-01T00:00:00.000Z", "end_date_format": "date"},
        {"name": "Maintenance Support 1", "start_date": "2026-10-02T00:00:00.000Z", "start_date_format": "date",
         "end_date": "2027-10-01T00:00:00.000Z", "end_date_format": "date"}]}
    ]
  }],
  "metadata": {"regeneratedAt": "2026-11-11T06:00:06.526Z"}
}`

type lifecycleStub struct {
	*httptest.Server
	hits     atomic.Int64
	lastAuth atomic.Value
}

// newLifecycleStub answers for the one product it knows, with an empty
// data array for anything else (which is what the live API does).
func newLifecycleStub(t *testing.T, body string) *lifecycleStub {
	t.Helper()
	ls := &lifecycleStub{}
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ls.hits.Add(1)
		ls.lastAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("name") != "Red Hat Ansible Automation Platform" {
			_, _ = w.Write([]byte(`{"data":[],"metadata":{}}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ls.Close)
	return ls
}

func grantLifecycle(c *config.Config) {
	c.Resources = map[config.Resource]config.ResourceRule{
		config.ResLifecycle: {Permission: config.LevelRead},
	}
}

func lifecycleServer(t *testing.T, body string) (*Server, *lifecycleStub) {
	t.Helper()
	ls := newLifecycleStub(t, body)
	srv := testServer(t, newStubAPI(t), grantLifecycle)
	srv.lifecycle = redhat.NewLifecycleClient(ls.URL, 5*time.Second)
	srv.now = func() time.Time { return time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC) }
	return srv, ls
}

func TestLifecycleToolAppearsOnlyWhenGranted(t *testing.T) {
	if toolNames(t, connect(t, testServer(t, newStubAPI(t), nil)))["rhlifecycle_get"] {
		t.Fatal("rhlifecycle_get exposed by default")
	}
	srv, _ := lifecycleServer(t, lifecycleStubBody)
	if !toolNames(t, connect(t, srv))["rhlifecycle_get"] {
		t.Fatal("rhlifecycle_get missing after being granted")
	}
}

func TestLifecycleGetComputesCurrentPhaseAndSendsNoToken(t *testing.T) {
	srv, ls := lifecycleServer(t, lifecycleStubBody)
	cs := connect(t, srv)
	var got lifecycleResult
	decodeResult(t, call(t, cs, "rhlifecycle_get", map[string]any{
		"product": "Red Hat Ansible Automation Platform",
	}), &got)

	if got.AsOf != "2026-11-10" || len(got.Products) != 1 || len(got.Products[0].Versions) != 2 {
		t.Fatalf("unexpected result: %+v", got)
	}
	v26 := got.Products[0].Versions[1]
	if v26.Name != "2.6" || v26.CurrentPhase != "Maintenance Support 1" || v26.ReportedType != "Full Support" {
		t.Fatalf("2.6 should be computed as Maintenance 1 while the API says Full Support: %+v", v26)
	}
	if auth, _ := ls.lastAuth.Load().(string); auth != "" {
		t.Fatalf("an Authorization header reached the life cycle API: %q", auth)
	}
}

func TestLifecycleGetFiltersByVersion(t *testing.T) {
	srv, _ := lifecycleServer(t, lifecycleStubBody)
	cs := connect(t, srv)
	var got lifecycleResult
	decodeResult(t, call(t, cs, "rhlifecycle_get", map[string]any{
		"product": "Red Hat Ansible Automation Platform", "version": " 2.7 ",
	}), &got)
	vs := got.Products[0].Versions
	if len(vs) != 1 || vs[0].Name != "2.7" || vs[0].CurrentPhase != "Full support" {
		t.Fatalf("version filter not applied: %+v", vs)
	}

	var miss lifecycleResult
	decodeResult(t, call(t, cs, "rhlifecycle_get", map[string]any{
		"product": "Red Hat Ansible Automation Platform", "version": "9.9",
	}), &miss)
	if len(miss.Products[0].Versions) != 0 || !strings.Contains(strings.Join(miss.Notes, " "), "2.7, 2.6") {
		t.Fatalf("an unknown version should list the available ones: %+v", miss)
	}
}

func TestLifecycleGetExplainsExactMatch(t *testing.T) {
	srv, _ := lifecycleServer(t, lifecycleStubBody)
	var got lifecycleResult
	decodeResult(t, call(t, connect(t, srv), "rhlifecycle_get", map[string]any{"product": "Ansible"}), &got)
	if len(got.Products) != 0 || !strings.Contains(strings.Join(got.Notes, " "), "full product name") {
		t.Fatalf("a partial name should be explained: %+v", got)
	}
}

func TestLifecycleGetReportsUnrecognizedShape(t *testing.T) {
	const secret = "do-not-echo"
	wrap := func(versions string) string {
		return `{"data":[{"name":"Red Hat Ansible Automation Platform","footnote":"` + secret + `","versions":` + versions + `}]}`
	}
	for name, body := range map[string]string{
		"unknown envelope": `{"items":[{"secret":"` + secret + `"}]}`,
		"versions missing": `{"data":[{"name":"Red Hat Ansible Automation Platform","footnote":"` + secret + `"}]}`,
		"phases missing":   wrap(`[{"name":"2.7","type":"` + secret + `"}]`),
		"formats missing": wrap(`[{"name":"2.7","phases":[{"name":"Full support",` +
			`"start_date":"2026-01-01T00:00:00Z","end_date":"2026-12-31T00:00:00Z","additional_text":"` + secret + `"}]}]`),
		"format unknown": wrap(`[{"name":"2.7","phases":[{"name":"Full support",` +
			`"start_date":"2026-01-01T00:00:00Z","start_date_format":"epoch",` +
			`"end_date":"2026-12-31T00:00:00Z","end_date_format":"date"}]}]`),
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := lifecycleServer(t, body)
			res := call(t, connect(t, srv), "rhlifecycle_get", map[string]any{"product": "Red Hat Ansible Automation Platform"})
			text := resultText(t, res)
			if res.IsError {
				t.Fatalf("an unrecognised shape should be a result, not an error: %s", text)
			}
			var got lifecycleResult
			decodeResult(t, res, &got)
			if got.Unrecognized == nil || len(got.Products) != 0 {
				t.Fatalf("expected unrecognizedShape and no products: %s", text)
			}
			if strings.Contains(text, secret) {
				t.Fatalf("part of the body was echoed: %s", text)
			}
		})
	}
}

func TestLifecycleGetValidatesProduct(t *testing.T) {
	srv, ls := lifecycleServer(t, lifecycleStubBody)
	cs := connect(t, srv)
	for _, p := range []string{"  ", strings.Repeat("x", lifecycleMaxProductLen+1)} {
		if res := call(t, cs, "rhlifecycle_get", map[string]any{"product": p}); !res.IsError {
			t.Fatalf("product %q accepted", p[:min(len(p), 10)])
		}
	}
	if ls.hits.Load() != 0 {
		t.Fatal("an invalid argument reached the API")
	}
}

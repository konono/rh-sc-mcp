package redhat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// lifecycleFixture follows the shape the live API returned on 2026-11-11
// for Red Hat Ansible Automation Platform, trimmed to two versions.
const lifecycleFixture = `{
  "data": [{
    "uuid": "d2c793b1",
    "name": "Red Hat Ansible Automation Platform",
    "former_names": ["Ansible Tower"],
    "is_retired": false,
    "footnote": "",
    "link": "https://access.redhat.com/support/policy/updates/ansible-automation-platform",
    "policies": "https://access.redhat.com/ansible-top-support-policies",
    "versions": [
      {"name": "2.7", "type": "Full Support", "additional_text": "", "openshift_compatibility": null,
       "phases": [
        {"name": "General availability", "start_date": "N/A", "start_date_format": "string",
         "end_date": "2026-06-02T00:00:00.000Z", "end_date_format": "date", "date": "2026-06-02T00:00:00.000Z", "date_format": "date", "additional_text": ""},
        {"name": "Full support", "start_date": "2026-06-02T00:00:00.000Z", "start_date_format": "date",
         "end_date": "2027-06-02T00:00:00.000Z", "end_date_format": "date", "additional_text": ""},
        {"name": "Maintenance Support 1", "start_date": "2027-06-03T00:00:00.000Z", "start_date_format": "date",
         "end_date": "2028-06-02T00:00:00.000Z", "end_date_format": "date", "additional_text": ""},
        {"name": "Extended update support", "start_date": "N/A", "start_date_format": "string",
         "end_date": "N/A", "end_date_format": "string", "additional_text": "EUS add-on is available for purchase."}
       ]},
      {"name": "2.6", "type": "Full Support", "additional_text": "",
       "phases": [
        {"name": "Full support", "start_date": "2025-10-01T00:00:00.000Z", "start_date_format": "date",
         "end_date": "2026-10-01T00:00:00.000Z", "end_date_format": "date"},
        {"name": "Maintenance Support 1", "start_date": "2026-10-02T00:00:00.000Z", "start_date_format": "date",
         "end_date": "2027-10-01T00:00:00.000Z", "end_date_format": "date"}
       ]}
    ]
  }],
  "metadata": {"timestamp": 1794376806526, "regeneratedAt": "2026-11-11T06:00:06.526Z"}
}`

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t.Add(15 * time.Hour) // mid-day, to show the time of day does not matter
}

func TestLifecycleParsesTheObservedShape(t *testing.T) {
	res := parseLifecycleResponse([]byte(lifecycleFixture))
	if res.Unrecognized != nil {
		t.Fatalf("fixture reported as unrecognised: %+v", res.Unrecognized)
	}
	if len(res.Products) != 1 || res.RegeneratedAt != "2026-11-11T06:00:06.526Z" {
		t.Fatalf("unexpected result: %+v", res)
	}
	p := res.Products[0]
	if p.Name != "Red Hat Ansible Automation Platform" || len(p.FormerNames) != 1 || len(p.Versions) != 2 {
		t.Fatalf("product projected wrongly: %+v", p)
	}
	ga := p.Versions[0].Phases[0]
	if ga.Start != "" || ga.StartText != "N/A" || ga.End != "2026-06-02" || ga.EndText != "" {
		t.Fatalf("GA boundary projected wrongly: %+v", ga)
	}
	eus := p.Versions[0].Phases[3]
	if eus.Start != "" || eus.End != "" || eus.StartText != "N/A" || eus.EndText != "N/A" {
		t.Fatalf("a string-format boundary was read as a date: %+v", eus)
	}
	if eus.AdditionalText == "" {
		t.Fatal("additional_text was dropped")
	}
	if p.Versions[0].ReportedType != "Full Support" {
		t.Fatalf("type not kept as reportedType: %+v", p.Versions[0])
	}
}

func TestLifecycleDateMarkedStringIsNeverADate(t *testing.T) {
	// A value that looks like a timestamp but is marked as a string stays
	// text: the format field is the API's own statement of what it is.
	raw := `{"data":[{"name":"X","versions":[{"name":"1","phases":[
	  {"name":"P","start_date":"2026-01-01T00:00:00.000Z","start_date_format":"string",
	   "end_date":"N/A","end_date_format":"string"}]}]}]}`
	res := parseLifecycleResponse([]byte(raw))
	if res.Unrecognized != nil {
		t.Fatalf("unexpected unrecognised: %+v", res.Unrecognized)
	}
	ph := res.Products[0].Versions[0].Phases[0]
	if ph.Start != "" || ph.StartText != "2026-01-01T00:00:00.000Z" || ph.EndText != "N/A" {
		t.Fatalf("string-format start was read as a date: %+v", ph)
	}
}

func TestLifecycleUnrecognisedShapes(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":             `<html>sign in</html>`,
		"no data":              `{"products":[]}`,
		"data not an array":    `{"data":{"name":"X"}}`,
		"entry without name":   `{"data":[{"versions":[]}]}`,
		"versions not array":   `{"data":[{"name":"X","versions":"2.7"}]}`,
		"versions missing":     `{"data":[{"name":"X"}]}`,
		"versions null":        `{"data":[{"name":"X","versions":null}]}`,
		"version without name": `{"data":[{"name":"X","versions":[{"phases":[]}]}]}`,
		"phases missing":       `{"data":[{"name":"X","versions":[{"name":"1"}]}]}`,
		"phases null":          `{"data":[{"name":"X","versions":[{"name":"1","phases":null}]}]}`,
		"phase without name":   `{"data":[{"name":"X","versions":[{"name":"1","phases":[{"start_date":"2026-01-01T00:00:00Z","start_date_format":"date","end_date":"2027-01-01T00:00:00Z","end_date_format":"date"}]}]}]}`,
		"formats missing":      `{"data":[{"name":"X","versions":[{"name":"1","phases":[{"name":"Full support","start_date":"2026-01-01T00:00:00Z","end_date":"2026-12-31T00:00:00Z"}]}]}]}`,
		"start date missing":   `{"data":[{"name":"X","versions":[{"name":"1","phases":[{"name":"P","start_date_format":"date","end_date":"2027-01-01T00:00:00Z","end_date_format":"date"}]}]}]}`,
		"end format unknown":   `{"data":[{"name":"X","versions":[{"name":"1","phases":[{"name":"P","start_date":"2026-01-01T00:00:00Z","start_date_format":"date","end_date":"2027-01-01","end_date_format":"datetime"}]}]}]}`,
		"date does not parse":  `{"data":[{"name":"X","versions":[{"name":"1","phases":[{"name":"P","start_date":"2026-01-01T00:00:00Z","start_date_format":"date","end_date":"soon","end_date_format":"date"}]}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			res := parseLifecycleResponse([]byte(raw))
			if res.Unrecognized == nil {
				t.Fatalf("expected unrecognised, got %+v", res)
			}
			if res.Products == nil || len(res.Products) != 0 {
				t.Fatalf("products should be an empty list: %+v", res.Products)
			}
		})
	}
}

func TestLifecycleEmptyDataIsNotUnrecognised(t *testing.T) {
	res := parseLifecycleResponse([]byte(`{"data":[],"metadata":{}}`))
	if res.Unrecognized != nil || len(res.Products) != 0 {
		t.Fatalf("an empty listing is a valid answer: %+v", res)
	}
}

func TestCurrentPhase(t *testing.T) {
	v := parseLifecycleResponse([]byte(lifecycleFixture)).Products[0].Versions[0]
	cases := []struct {
		on, phase, note string
	}{
		{"2026-11-10", "Full support", ""},
		{"2026-06-02", "Full support", ""},          // first day; GA has no start so it does not compete
		{"2027-06-02", "Full support", ""},          // end date is inclusive
		{"2027-06-03", "Maintenance Support 1", ""}, // next phase's first day
		{"2028-06-02", "Maintenance Support 1", ""},
		{"2028-06-03", "", "every dated phase has ended; the last ended on 2028-06-02"},
		{"2026-01-01", "", "the first dated phase starts on 2026-06-02"},
	}
	for _, c := range cases {
		got := v.CurrentPhase(day(c.on))
		if got.Phase != c.phase || got.Note != c.note {
			t.Errorf("%s: got %+v, want phase %q note %q", c.on, got, c.phase, c.note)
		}
	}
}

func TestCurrentPhaseIgnoresTheReportedType(t *testing.T) {
	// 2.6 is labelled "Full Support" by the API, but its dates say
	// Maintenance 1 on 2026-11-10 — the case that motivated computing it.
	v := parseLifecycleResponse([]byte(lifecycleFixture)).Products[0].Versions[1]
	if got := v.CurrentPhase(day("2026-11-10")); got.Phase != "Maintenance Support 1" {
		t.Fatalf("got %+v", got)
	}
}

func TestCurrentPhaseGapAndUndated(t *testing.T) {
	raw := `{"data":[{"name":"X","versions":[
	 {"name":"gap","phases":[
	   {"name":"A","start_date":"2026-01-01T00:00:00.000Z","start_date_format":"date","end_date":"2026-02-01T00:00:00.000Z","end_date_format":"date"},
	   {"name":"B","start_date":"2026-03-01T00:00:00.000Z","start_date_format":"date","end_date":"2026-04-01T00:00:00.000Z","end_date_format":"date"}]},
	 {"name":"undated","phases":[{"name":"A","start_date":"N/A","start_date_format":"string","end_date":"N/A","end_date_format":"string"}]}]}]}`
	vs := parseLifecycleResponse([]byte(raw)).Products[0].Versions
	if got := vs[0].CurrentPhase(day("2026-02-15")); got.Phase != "" || !strings.Contains(got.Note, "between") {
		t.Fatalf("gap: %+v", got)
	}
	if got := vs[1].CurrentPhase(day("2026-02-15")); got.Phase != "" || !strings.Contains(got.Note, "no phase has both") {
		t.Fatalf("undated: %+v", got)
	}
}

func TestLifecycleClientSendsNoCredentialAndOnlyTheName(t *testing.T) {
	var gotQuery string
	var gotAuth, gotCookie []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Values("Authorization")
		gotCookie = r.Header.Values("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(lifecycleFixture))
	}))
	t.Cleanup(srv.Close)

	c := NewLifecycleClient(srv.URL+"/product-life-cycles/api/v1/products", 5*time.Second)
	res, err := c.GetProducts(context.Background(), "Red Hat Ansible Automation Platform")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Products) != 1 {
		t.Fatalf("got %+v", res)
	}
	if len(gotAuth) != 0 || len(gotCookie) != 0 {
		t.Fatalf("a credential was sent: Authorization=%q Cookie=%q", gotAuth, gotCookie)
	}
	if gotQuery != "name=Red+Hat+Ansible+Automation+Platform" {
		t.Fatalf("unexpected query %q (version must not be sent; the API ignores it)", gotQuery)
	}
}

func TestLifecycleClientHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("upstream down\nsecond line"))
	}))
	t.Cleanup(srv.Close)
	_, err := NewLifecycleClient(srv.URL, 5*time.Second).GetProducts(context.Background(), "X")
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "second line") {
		t.Fatalf("error should quote only the first line: %v", err)
	}
}

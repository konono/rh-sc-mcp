package redhat

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
)

// apiStub records what reached it and replies with whatever the test set.
type apiStub struct {
	*httptest.Server
	mux      *http.ServeMux
	requests atomic.Int64
	// seen is the last request line and auth header, for assertions.
	lastPath string
	lastAuth string
	lastBody string
}

func newAPIStub(t *testing.T) *apiStub {
	t.Helper()
	s := &apiStub{mux: http.NewServeMux()}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		s.lastPath = r.URL.Path
		s.lastAuth = r.Header.Get("Authorization")
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			s.lastBody = string(b)
		}
		s.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *apiStub) handle(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

// newTestClient wires a Client to a stub API and a stub SSO realm.
func newTestClient(t *testing.T, api *apiStub) (*Client, *ssoStub) {
	t.Helper()
	sso := newSSOStub(t)
	c := NewClient(api.URL, "v3", sso.URL, "rhsm-api", fakeOffline, 5*time.Second)
	return c, sso
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---- 2xx variety ----------------------------------------------------------

// The create endpoint is documented to answer 201 with a location array,
// but a deployment may send the Location header instead, both, or
// neither. All four have to produce a usable result.
func TestCreateCaseAcceptsEveryDocumentedSuccessShape(t *testing.T) {
	tests := []struct {
		name       string
		reply      func(w http.ResponseWriter)
		wantNumber string
		wantLocs   int
	}{
		{
			name: "201 with the documented body",
			reply: func(w http.ResponseWriter) {
				writeJSON(w, http.StatusCreated, map[string]any{
					"location": []string{"https://api.access.redhat.com/support/v3/cases/04337263"},
				})
			},
			wantNumber: "04337263",
			wantLocs:   1,
		},
		{
			name: "201 with only a Location header",
			reply: func(w http.ResponseWriter) {
				w.Header().Set("Location", "https://api.access.redhat.com/support/v3/cases/04337264")
				w.WriteHeader(http.StatusCreated)
			},
			wantNumber: "04337264",
			wantLocs:   1,
		},
		{
			name: "201 with header and body agreeing",
			reply: func(w http.ResponseWriter) {
				w.Header().Set("Location", "https://api.access.redhat.com/support/v3/cases/04337265")
				writeJSON(w, http.StatusCreated, map[string]any{
					"location": []string{"https://api.access.redhat.com/support/v3/cases/04337265"},
				})
			},
			wantNumber: "04337265",
			wantLocs:   1, // de-duplicated
		},
		{
			name: "200 with an empty body",
			reply: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusOK)
			},
			wantNumber: "",
			wantLocs:   0,
		},
		{
			name: "204 with no content at all",
			reply: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusNoContent)
			},
			wantNumber: "",
			wantLocs:   0,
		},
		{
			name: "201 whose location does not end in a case number",
			reply: func(w http.ResponseWriter) {
				writeJSON(w, http.StatusCreated, map[string]any{
					"location": []string{"https://api.access.redhat.com/support/v3/cases/5003a00001AbCdEfGHI"},
				})
			},
			wantNumber: "", // an opaque id is not a case number
			wantLocs:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := newAPIStub(t)
			api.handle("/v3/cases", func(w http.ResponseWriter, _ *http.Request) { tc.reply(w) })
			c, _ := newTestClient(t, api)

			res, err := c.CreateCase(context.Background(), &CreateCaseRequest{
				Summary: "s", Description: "d", Product: "p", Version: "v",
			})
			if err != nil {
				t.Fatalf("CreateCase: %v", err)
			}
			if res.CaseNumber != tc.wantNumber {
				t.Errorf("caseNumber = %q, want %q", res.CaseNumber, tc.wantNumber)
			}
			if len(res.Location) != tc.wantLocs {
				t.Errorf("location = %v, want %d entries", res.Location, tc.wantLocs)
			}
		})
	}
}

// A JSON body that is not labelled application/json still has to be
// decoded. The handler below sets no Content-Type, which means Go's
// sniffer labels it text/plain — exactly what a misconfigured gateway
// produces, and not a reason to fail the call.
func TestMislabelledJSONBodyIsStillDecoded(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"caseNumber":"04337263","summary":"disk full"}`))
	})
	c, _ := newTestClient(t, api)

	got, err := c.GetCase(context.Background(), "04337263")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "disk full" {
		t.Fatalf("summary = %q", got.Summary)
	}
}

// An HTML body with a 200 is a proxy interception, not a case. Saying so
// beats decoding it into an empty struct and reporting a blank case.
func TestNonJSONSuccessBodyIsAnError(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Sign in</body></html>"))
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil {
		t.Fatal("expected an error for an HTML body")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("error does not explain the problem: %v", err)
	}
}

// The diagnostic excerpt in the non-JSON error quotes the body, so a 2xx
// that reflects a credential must not carry it into the message. This is
// the success path, which the non-2xx scrubbing never touches.
func TestNonJSONSuccessBodyIsScrubbed(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		// A sign-in page from a proxy that echoes what it received.
		_, _ = fmt.Fprintf(w,
			"<html><body>Session expired. Request carried %s and offline token %s</body></html>",
			r.Header.Get("Authorization"), fakeOffline)
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil {
		t.Fatal("expected an error for an HTML body")
	}
	msg := err.Error()
	if strings.Contains(msg, "access-token-1") {
		t.Errorf("the bearer token leaked through the 2xx path: %q", msg)
	}
	if strings.Contains(msg, fakeOffline) {
		t.Errorf("the offline token leaked through the 2xx path: %q", msg)
	}
	if !strings.Contains(msg, "Session expired") {
		t.Errorf("scrubbing destroyed the diagnostic: %q", msg)
	}
}

// A create answered with a 200 HTML page is a failure, not a case with
// an unknown number: reporting success there sends the caller looking
// for a case that does not exist, or opening a second one.
func TestCreateRejectsANonJSONBody(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "<html><body>Sign in. Received %s</body></html>", r.Header.Get("Authorization"))
	})
	c, _ := newTestClient(t, api)

	res, err := c.CreateCase(context.Background(), &CreateCaseRequest{
		Summary: "s", Description: "d", Product: "p", Version: "v",
	})
	if err == nil {
		t.Fatalf("an HTML sign-in page was reported as a created case: %#v", res)
	}
	if strings.Contains(err.Error(), "access-token-1") {
		t.Errorf("the bearer token leaked: %q", err.Error())
	}
}

// An update that returns 200 with no body still succeeded.
func TestUpdateWithEmptyBodyReportsSuccess(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newTestClient(t, api)

	res, err := c.UpdateCase(context.Background(), "04337263", &UpdateCaseRequest{Status: strptr("Closed")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Message == "" {
		t.Fatal("an empty 200 produced an empty result, which reads like a failure")
	}
}

func TestUpdateSurfacesFieldsNotSentToSalesforce(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"message":                   "Case updated",
			"fieldsNotSentToSalesforce": map[string]any{"hostname": "ignored-by-backend"},
		})
	})
	c, _ := newTestClient(t, api)

	res, err := c.UpdateCase(context.Background(), "04337263", &UpdateCaseRequest{Hostname: strptr("host1")})
	if err != nil {
		t.Fatal(err)
	}
	if res.FieldsNotSentToSalesforce["hostname"] != "ignored-by-backend" {
		t.Fatalf("fieldsNotSentToSalesforce was dropped: %#v", res.FieldsNotSentToSalesforce)
	}
}

// A case with no comments can answer with an empty body rather than "[]".
func TestEmptyCommentListingIsNotAnError(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newTestClient(t, api)

	got, err := c.ListComments(context.Background(), "04337263", CommentQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d comments", len(got))
	}
}

// ---- errors ---------------------------------------------------------------

func TestAPIErrorDecodesTheDocumentedErrorShape(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"internalErrorCode": "ACCOUNT_MISMATCH",
			"message":           "Account mismatch.",
			"detailMessage":     "The case belongs to another account.",
			"recommendations":   []string{"Check the account number"},
		})
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("got %T, want *APIError", err)
	}
	if apiErr.Status != http.StatusForbidden {
		t.Errorf("status = %d", apiErr.Status)
	}
	for _, want := range []string{"403", "ACCOUNT_MISMATCH", "Account mismatch.", "another account", "Check the account number"} {
		if !strings.Contains(apiErr.Error(), want) {
			t.Errorf("error %q is missing %q", apiErr.Error(), want)
		}
	}
}

func TestNotFoundIsDistinguishable(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Case not found"})
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.NotFound() {
		t.Fatalf("got %v, want a 404 APIError", err)
	}
}

// An upstream that quotes the Authorization header back in its error must
// not put the bearer token into the message the model reads.
func TestAPIErrorIsScrubbedOfCredentials(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"message":       "rejected request",
			"detailMessage": "received header " + r.Header.Get("Authorization") + " and offline token " + fakeOffline,
		})
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "access-token-1") {
		t.Errorf("the bearer token leaked into the error: %q", msg)
	}
	if strings.Contains(msg, fakeOffline) {
		t.Errorf("the offline token leaked into the error: %q", msg)
	}
	if !strings.Contains(msg, "rejected request") {
		t.Errorf("scrubbing destroyed the message: %q", msg)
	}
}

// A huge error body must not be pasted whole into a transcript.
func TestAPIErrorBodyIsTruncated(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("A", 200000)))
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(err.Error()) > maxErrorBody+300 {
		t.Fatalf("error is %d bytes; it was not truncated", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Error("truncation was silent")
	}
}

func TestEmptyErrorBodyStillExplainsItself(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("got %v, want an error naming the status", err)
	}
}

// ---- token lifecycle through the client -----------------------------------

func TestBearerTokenIsSentAndRefreshedOn401(t *testing.T) {
	api := newAPIStub(t)
	var calls atomic.Int64
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, r *http.Request) {
		// Reject the first token as though it had just been revoked.
		if calls.Add(1) == 1 {
			if r.Header.Get("Authorization") != "Bearer access-token-1" {
				t.Errorf("first call sent %q", r.Header.Get("Authorization"))
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "expired"})
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token-2" {
			t.Errorf("retry sent %q, want the refreshed token", got)
		}
		writeJSON(w, http.StatusOK, map[string]any{"caseNumber": "04337263", "summary": "ok"})
	})
	c, sso := newTestClient(t, api)

	got, err := c.GetCase(context.Background(), "04337263")
	if err != nil {
		t.Fatalf("GetCase: %v", err)
	}
	if got.Summary != "ok" {
		t.Fatalf("summary = %q", got.Summary)
	}
	if n := sso.exchanges.Load(); n != 2 {
		t.Fatalf("SSO exchanges = %d, want 2 (initial + refresh)", n)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("API calls = %d, want 2", n)
	}
}

// The retry happens once. A server that always answers 401 must produce
// an error, not a loop.
func TestPersistent401FailsAfterOneRetry(t *testing.T) {
	api := newAPIStub(t)
	var calls atomic.Int64
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "nope"})
	})
	c, _ := newTestClient(t, api)

	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("API calls = %d, want exactly 2 (one attempt, one retry)", n)
	}
}

// A 403 is an authorization decision, not a stale token; retrying it
// would double every forbidden request for nothing.
func TestForbiddenIsNotRetried(t *testing.T) {
	api := newAPIStub(t)
	var calls atomic.Int64
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "Account mismatch."})
	})
	c, _ := newTestClient(t, api)

	if _, err := c.GetCase(context.Background(), "04337263"); err == nil {
		t.Fatal("expected an error")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("API calls = %d, want 1", n)
	}
}

func TestTimeoutIsEnforced(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{"caseNumber": "04337263"})
	})
	sso := newSSOStub(t)
	c := NewClient(api.URL, "v3", sso.URL, "rhsm-api", fakeOffline, 50*time.Millisecond)

	start := time.Now()
	_, err := c.GetCase(context.Background(), "04337263")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %v; the timeout was not applied", elapsed)
	}
}

// ---- path safety ----------------------------------------------------------

// A case number that could change the request path is refused before any
// request is made, so a traversal attempt never reaches the network.
func TestMaliciousCaseNumbersAreRejectedBeforeAnyRequest(t *testing.T) {
	bad := []string{
		"../../v3/accounts/current",
		"04337263/comments",
		"04337263?x=1",
		"04337263#frag",
		"0433 7263",
		"04337263;drop",
		"%2e%2e%2fadmin",
		"abc",
		"",
		"   ",
		strings.Repeat("9", 33),
	}
	api := newAPIStub(t)
	api.handle("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"caseNumber": "whatever"})
	})
	c, _ := newTestClient(t, api)

	for _, n := range bad {
		t.Run(fmt.Sprintf("%q", n), func(t *testing.T) {
			if _, err := c.GetCase(context.Background(), n); err == nil {
				t.Fatalf("case number %q was accepted", n)
			}
		})
	}
	if got := api.requests.Load(); got != 0 {
		t.Fatalf("%d requests reached the API; validation must happen first", got)
	}
}

func TestValidCaseNumbersAreAccepted(t *testing.T) {
	for _, n := range []string{"1", "04337263", "0000000000000000000000000000001"} {
		if _, err := ValidateCaseNumber(n); err != nil {
			t.Errorf("ValidateCaseNumber(%q) = %v", n, err)
		}
	}
	// Surrounding whitespace is the model's, not the user's.
	if got, err := ValidateCaseNumber("  04337263 "); err != nil || got != "04337263" {
		t.Errorf("ValidateCaseNumber trimmed badly: %q, %v", got, err)
	}
}

func TestCommentIDValidation(t *testing.T) {
	if _, err := ValidateCommentID("a0a3a00000ABCDEAA5"); err != nil {
		t.Errorf("a Salesforce id was rejected: %v", err)
	}
	for _, bad := range []string{"", "../../cases", "a/b", "id with space", "a?b"} {
		if _, err := ValidateCommentID(bad); err == nil {
			t.Errorf("comment id %q was accepted", bad)
		}
	}
}

// Path building escapes its segments, so even a value that slipped past
// validation could not add a path element.
func TestVersionedPathEscapesSegments(t *testing.T) {
	c := &Client{apiVersion: "v3"}
	if got := c.VersionedPath("cases", "a/b"); got != "/v3/cases/a%2Fb" {
		t.Fatalf("VersionedPath = %q", got)
	}
	if got := c.VersionedPath("cases", "04337263", "comments"); got != "/v3/cases/04337263/comments" {
		t.Fatalf("VersionedPath = %q", got)
	}
}

// The case number reaches the API as the path, with its leading zeros.
func TestRequestPathUsesTheCaseNumberVerbatim(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"caseNumber": "04337263"})
	})
	c, _ := newTestClient(t, api)

	if _, err := c.GetCase(context.Background(), "04337263"); err != nil {
		t.Fatal(err)
	}
	if api.lastPath != "/v3/cases/04337263" {
		t.Fatalf("path = %q; leading zeros must survive", api.lastPath)
	}
}

// ---- request shaping ------------------------------------------------------

// The document allows caseNumber in the comment body but requires it to
// match the path, so the client fills it from the validated path value.
func TestAddCommentPinsTheCaseNumberToThePath(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263/comments", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "c1", "commentBody": "hi"})
	})
	c, _ := newTestClient(t, api)

	res, err := c.AddComment(context.Background(), "04337263", &CreateCommentRequest{
		CommentBody: "hi",
		ContentType: "plaintext",
		// A body that disagrees with the path must not win.
		CaseNumber: "09999999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(api.lastBody, `"caseNumber":"04337263"`) {
		t.Fatalf("request body did not carry the path's case number: %s", api.lastBody)
	}
	if res.Comment == nil || res.Comment.ID != "c1" {
		t.Fatalf("comment = %#v", res.Comment)
	}
}

func TestListCommentsSendsTheQueryFilters(t *testing.T) {
	api := newAPIStub(t)
	var query string
	api.handle("/v3/cases/04337263/comments", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeJSON(w, http.StatusOK, []map[string]any{{"id": "c1"}})
	})
	c, _ := newTestClient(t, api)

	_, err := c.ListComments(context.Background(), "04337263", CommentQuery{
		StartDate: "2026-01-01", EndDate: "2026-02-01", SortField: "createdDate", SortOrder: "asc",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"startDate=2026-01-01", "endDate=2026-02-01", "sortField=createdDate", "sortOrder=asc"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q is missing %q", query, want)
		}
	}
}

func TestFilterCasesPostsTheCriteriaAndReadsTheEnvelope(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/filter", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"totalCount": 7,
			"cases": []map[string]any{
				{"caseNumber": "04337263", "summary": "one", "status": "Waiting on Red Hat"},
				{"caseNumber": "04337264", "summary": "two"},
			},
		})
	})
	c, _ := newTestClient(t, api)

	res, err := c.FilterCases(context.Background(), &FilterCasesRequest{Keyword: "kernel panic", MaxResults: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalCount != 7 || len(res.Cases) != 2 {
		t.Fatalf("totalCount=%d cases=%d", res.TotalCount, len(res.Cases))
	}
	if !strings.Contains(api.lastBody, `"keyword":"kernel panic"`) {
		t.Fatalf("criteria were not sent: %s", api.lastBody)
	}
	// Unset optional fields must be omitted, not sent as empty values
	// that the backend would read as "match the empty string".
	if strings.Contains(api.lastBody, `"status"`) {
		t.Fatalf("an unset filter was sent anyway: %s", api.lastBody)
	}
}

// An absent update field must not appear in the payload at all, or the
// backend would read it as "clear this".
func TestUpdateSendsOnlyTheFieldsGiven(t *testing.T) {
	api := newAPIStub(t)
	api.handle("/v3/cases/04337263", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
	})
	c, _ := newTestClient(t, api)

	_, err := c.UpdateCase(context.Background(), "04337263", &UpdateCaseRequest{
		Status: strptr("Closed"),
		Notes:  strptr(""), // explicitly cleared
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(api.lastBody, `"status":"Closed"`) {
		t.Errorf("status was not sent: %s", api.lastBody)
	}
	if !strings.Contains(api.lastBody, `"notes":""`) {
		t.Errorf("an explicit clear was dropped: %s", api.lastBody)
	}
	if strings.Contains(api.lastBody, `"summary"`) {
		t.Errorf("an untouched field was sent: %s", api.lastBody)
	}
}

func strptr(s string) *string { return &s }

func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

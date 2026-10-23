package redhat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxResponseBody bounds what is read from a single response. The largest
// thing the case API returns is a case with its full comment history; a
// few megabytes covers that and still refuses to buffer an endless stream.
const maxResponseBody = 8 << 20

// Client is a thin HTTP client for the Red Hat Case Management API.
//
// It owns three things the tool layer should not have to think about:
// bearer tokens (fetched and refreshed through the TokenSource), the
// variety of shapes a 2xx can take, and making sure no credential reaches
// an error string.
type Client struct {
	baseURL    string // e.g. "https://api.access.redhat.com/support", no trailing slash
	apiVersion string // "v3"
	tokens     *TokenSource
	redactor   *Redactor
	http       *http.Client
}

// NewClient builds a client. baseURL must include the API basePath.
func NewClient(baseURL, apiVersion, ssoTokenURL, ssoClientID, offlineToken string, timeout time.Duration) *Client {
	hc := &http.Client{Timeout: timeout}
	ts := NewTokenSource(ssoTokenURL, ssoClientID, offlineToken, hc)
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiVersion: strings.Trim(apiVersion, "/"),
		tokens:     ts,
		redactor:   NewRedactor(ts.Secrets),
		http:       hc,
	}
}

// Redactor exposes the credential scrubber so callers that build their own
// messages can run them through it too.
func (c *Client) Redactor() *Redactor { return c.redactor }

// APIError is a non-2xx response.
//
// The upstream error body is a LegacyApiErrorResponse, so the fields are
// decoded rather than dumped: the model gets the message and the
// recommendations, and nothing that happened to share the envelope.
type APIError struct {
	Status      int
	Method      string
	Path        string
	Code        string
	Message     string
	Detail      string
	Suggestions []string
	// Raw is the scrubbed, truncated body, kept only when it could not be
	// decoded as the documented error shape.
	Raw string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Red Hat API %s %s returned HTTP %d", e.Method, e.Path, e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	switch {
	case e.Message != "" && e.Detail != "" && e.Detail != e.Message:
		fmt.Fprintf(&b, ": %s — %s", e.Message, e.Detail)
	case e.Message != "":
		fmt.Fprintf(&b, ": %s", e.Message)
	case e.Detail != "":
		fmt.Fprintf(&b, ": %s", e.Detail)
	case e.Raw != "":
		fmt.Fprintf(&b, ": %s", e.Raw)
	}
	if len(e.Suggestions) > 0 {
		fmt.Fprintf(&b, " [suggested: %s]", strings.Join(e.Suggestions, "; "))
	}
	return b.String()
}

// NotFound reports whether the API said the object does not exist.
func (e *APIError) NotFound() bool { return e.Status == http.StatusNotFound }

// legacyError mirrors LegacyApiErrorResponse from the API document.
type legacyError struct {
	InternalErrorCode string   `json:"internalErrorCode"`
	Message           string   `json:"message"`
	DetailMessage     string   `json:"detailMessage"`
	Recommendations   []string `json:"recommendations"`
}

// Response is a successful (2xx) reply, kept raw so callers can cope with
// the several shapes the API uses for success.
type Response struct {
	Status int
	Header http.Header
	Body   []byte

	// redactor scrubs any part of the body that is quoted back in an
	// error. A Response carries its own rather than leaving that to the
	// caller: the body is the thing most likely to contain a reflected
	// credential, and a diagnostic built from it is exactly where one
	// would slip out. Nil is safe and still applies the patterns.
	redactor *Redactor
}

// Location returns the created-resource URI, preferring the Location
// header and falling back to the documented body form.
//
// Both exist: CreateCaseLocationsResponse puts the URI in the body as
// {"location": [...]}, while a 201 conventionally also carries a Location
// header, and deployments differ in which they send.
func (r *Response) Location() []string {
	var out []string
	if h := strings.TrimSpace(r.Header.Get("Location")); h != "" {
		out = append(out, h)
	}
	var body struct {
		Location []string `json:"location"`
	}
	if r.isJSON() && json.Unmarshal(r.Body, &body) == nil {
		for _, l := range body.Location {
			if l = strings.TrimSpace(l); l != "" && !contains(out, l) {
				out = append(out, l)
			}
		}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// DecodeJSON decodes the body into out.
//
// It reports whether there was a body to decode at all, because success
// without one is normal here: a 204, or a 200 whose only payload was the
// Location header. A caller that needs the body treats false as an error;
// a caller that does not simply carries on.
func (r *Response) DecodeJSON(out any) (bool, error) {
	if len(bytes.TrimSpace(r.Body)) == 0 {
		return false, nil
	}
	if !r.isJSON() {
		// The excerpt is what makes this diagnosable — "expected JSON"
		// alone does not distinguish a proxy sign-in page from a plain
		// text error. It is scrubbed before it is clipped, because a
		// 200 is exactly where an upstream is most likely to reflect
		// the Authorization header back at us, and clipping first could
		// split a token past the point where the patterns match it.
		return false, fmt.Errorf(
			"expected a JSON body but the API answered HTTP %d with %q",
			r.Status, firstLine(r.redactor.Scrub(string(r.Body)), 120))
	}
	if err := json.Unmarshal(r.Body, out); err != nil {
		// A json error quotes the offending input in some cases, so it
		// goes through the scrubber too.
		return false, fmt.Errorf("decode HTTP %d response body: %s",
			r.Status, r.redactor.Scrub(err.Error()))
	}
	return true, nil
}

// isJSON decides by content type, falling back to the shape of the body.
//
// The fallback is not redundant. A gateway that returns JSON labelled
// text/plain, or with no Content-Type for Go's sniffer to replace, is
// common enough that refusing to parse a body which plainly is JSON would
// turn a cosmetic header problem into an outage. The shape test is narrow
// — a leading '{' or '[' — so the case this is really guarding against,
// an HTML sign-in page served with a 200, still fails loudly.
func (r *Response) isJSON() bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "application/json", "text/json", "application/problem+json":
		return true
	}
	b := bytes.TrimSpace(r.Body)
	return len(b) > 0 && (b[0] == '{' || b[0] == '[')
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// VersionedPath builds a path under the configured API version, e.g.
// VersionedPath("cases", "04337263") -> "/v3/cases/04337263".
//
// Every segment is escaped here rather than at the call sites, so a value
// that came from the model cannot climb out of the path it was meant to
// fill. The tool layer validates those values as well; this is the second
// of the two.
func (c *Client) VersionedPath(segments ...string) string {
	parts := make([]string, 0, len(segments)+1)
	parts = append(parts, c.apiVersion)
	for _, s := range segments {
		parts = append(parts, url.PathEscape(s))
	}
	return "/" + strings.Join(parts, "/")
}

// UnversionedPath builds a path directly under the basePath, for the
// endpoints that carry no version segment (the search surface).
func (c *Client) UnversionedPath(segments ...string) string {
	parts := make([]string, 0, len(segments))
	for _, s := range segments {
		parts = append(parts, url.PathEscape(s))
	}
	return "/" + strings.Join(parts, "/")
}

// Get performs an authenticated GET.
func (c *Client) Get(ctx context.Context, path string, q url.Values) (*Response, error) {
	return c.do(ctx, http.MethodGet, path, q, nil)
}

// PostJSON performs an authenticated POST with a JSON body.
func (c *Client) PostJSON(ctx context.Context, path string, body any) (*Response, error) {
	return c.do(ctx, http.MethodPost, path, nil, body)
}

// PutJSON performs an authenticated PUT with a JSON body.
func (c *Client) PutJSON(ctx context.Context, path string, body any) (*Response, error) {
	return c.do(ctx, http.MethodPut, path, nil, body)
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any) (*Response, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
	}

	resp, token, err := c.attempt(ctx, method, path, q, encoded)
	if err != nil {
		return nil, err
	}

	// One retry, and only on 401. An access token can expire between the
	// expiry check and the server reading it, or be revoked mid-session;
	// both look like this and both are fixed by fetching a new one. The
	// retry is safe for the non-idempotent verbs too, because a 401 means
	// the request was rejected before it was acted on.
	if resp.Status == http.StatusUnauthorized {
		c.tokens.Invalidate(token)
		resp, _, err = c.attempt(ctx, method, path, q, encoded)
		if err != nil {
			return nil, err
		}
	}

	if resp.Status < 200 || resp.Status >= 300 {
		return nil, c.apiError(method, path, resp)
	}
	return resp, nil
}

// attempt performs one round trip and returns the token it used, so the
// caller can invalidate exactly that token rather than whatever is cached
// by the time the response comes back.
func (c *Client) attempt(ctx context.Context, method, path string, q url.Values, body []byte) (*Response, string, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, "", err
	}

	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, "", fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	hr, err := c.http.Do(req)
	if err != nil {
		// url.Error embeds the request URL, which carries no credential
		// (the token travels in a header) — but scrub anyway, so that a
		// future query parameter cannot quietly become an exception.
		return nil, "", fmt.Errorf("%s %s failed: %s", method, path, c.redactor.Scrub(err.Error()))
	}
	defer func() { _ = hr.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(hr.Body, maxResponseBody))
	if err != nil {
		return nil, "", fmt.Errorf("read %s %s response: %s", method, path, c.redactor.Scrub(err.Error()))
	}
	return &Response{Status: hr.StatusCode, Header: hr.Header, Body: raw, redactor: c.redactor}, token, nil
}

func (c *Client) apiError(method, path string, resp *Response) error {
	e := &APIError{Status: resp.Status, Method: method, Path: path}
	var le legacyError
	if resp.isJSON() && json.Unmarshal(resp.Body, &le) == nil &&
		(le.Message != "" || le.DetailMessage != "" || le.InternalErrorCode != "") {
		e.Code = c.redactor.Scrub(le.InternalErrorCode)
		e.Message = c.redactor.ScrubAndTruncate(le.Message)
		e.Detail = c.redactor.ScrubAndTruncate(le.DetailMessage)
		for _, r := range le.Recommendations {
			e.Suggestions = append(e.Suggestions, c.redactor.ScrubAndTruncate(r))
		}
		return e
	}
	// Anything else — an HTML error page from a proxy, a plain string, an
	// empty body — is reported as text, scrubbed and clipped.
	if body := strings.TrimSpace(string(resp.Body)); body != "" {
		e.Raw = c.redactor.ScrubAndTruncate(body)
	} else {
		e.Raw = "(empty response body)"
	}
	return e
}

package redhat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// expiryMargin is subtracted from the lifetime the SSO server reports, so
// a token is replaced before it expires rather than after a request has
// already failed with it.
const expiryMargin = 60 * time.Second

// TokenSource exchanges a Red Hat offline token for a short-lived access
// token and caches it until shortly before it expires.
//
// The offline token is an OAuth refresh token: it is presented to the SSO
// realm with grant_type=refresh_token, and the realm answers with a bearer
// token for the API. The offline token itself never leaves this process
// except in that one request.
type TokenSource struct {
	tokenURL     string
	clientID     string
	offlineToken string
	http         *http.Client

	// now is injectable so tests can age a token without sleeping.
	now func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewTokenSource builds a TokenSource. The http.Client supplies the
// timeout; pass the same bound used for API calls.
func NewTokenSource(tokenURL, clientID, offlineToken string, hc *http.Client) *TokenSource {
	return &TokenSource{
		tokenURL:     tokenURL,
		clientID:     clientID,
		offlineToken: offlineToken,
		http:         hc,
		now:          time.Now,
	}
}

// Secrets returns the credentials currently held, for the Redactor.
//
// It takes the lock because the access token is rewritten by refreshes on
// other goroutines, and reading it unsynchronised is a data race — on a
// value whose whole purpose is to be found in text that is about to be
// shown to someone.
func (t *TokenSource) Secrets() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, 2)
	if t.offlineToken != "" {
		out = append(out, t.offlineToken)
	}
	if t.token != "" {
		out = append(out, t.token)
	}
	return out
}

// Token returns a valid access token, refreshing when the cached one is
// missing or close to expiry.
//
// The lock is held across the HTTP call. That serialises concurrent
// callers into one refresh instead of letting each start its own, which
// matters because every extra refresh is another transmission of the
// offline token.
func (t *TokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" && t.now().Before(t.expiry) {
		return t.token, nil
	}
	return t.refreshLocked(ctx)
}

// Invalidate drops the cached token if it is still the one the caller
// used.
//
// The comparison is what makes this safe to call after a 401: by the time
// a failed request reports back, another goroutine may already have
// replaced the token, and clearing unconditionally would throw away a
// good credential and force a needless refresh.
func (t *TokenSource) Invalidate(used string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if used != "" && t.token == used {
		t.token = ""
		t.expiry = time.Time{}
	}
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type ssoError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (t *TokenSource) refreshLocked(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {t.clientID},
		"refresh_token": {t.offlineToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build SSO token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		// A transport error can quote the URL but never the body, so there
		// is nothing credential-shaped to scrub here.
		return "", fmt.Errorf("SSO token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read SSO token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("SSO token exchange failed with HTTP %d: %s",
			resp.StatusCode, t.describeSSOFailure(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("decode SSO token response: unexpected body (HTTP %d)", resp.StatusCode)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("SSO token response contained no access_token (HTTP %d)", resp.StatusCode)
	}

	t.token = tr.AccessToken
	t.expiry = t.expiryFrom(tr.ExpiresIn)
	return t.token, nil
}

// expiryFrom converts the reported lifetime into an absolute deadline,
// kept short of the real one so a token is never used at the edge.
//
// A missing or tiny expires_in is treated as a very short life rather than
// an unlimited one: assuming the token lasts forever would turn a quirk in
// the response into an authentication failure on every later call.
func (t *TokenSource) expiryFrom(expiresIn int64) time.Time {
	now := t.now()
	if expiresIn <= 0 {
		return now.Add(expiryMargin)
	}
	life := time.Duration(expiresIn) * time.Second
	if life <= expiryMargin {
		return now.Add(life / 2)
	}
	return now.Add(life - expiryMargin)
}

// describeSSOFailure renders an SSO error body without leaking anything.
//
// A failed exchange still returns structured OAuth fields, and those are
// the useful part — "invalid_grant: token is not active" is what tells an
// operator their offline token was revoked. The raw body is not passed
// through, because a realm is free to echo the request back.
func (t *TokenSource) describeSSOFailure(body []byte) string {
	red := NewRedactor(t.secretsLocked)
	var e ssoError
	if err := json.Unmarshal(body, &e); err == nil && (e.Error != "" || e.ErrorDescription != "") {
		parts := make([]string, 0, 2)
		if e.Error != "" {
			parts = append(parts, e.Error)
		}
		if e.ErrorDescription != "" {
			parts = append(parts, e.ErrorDescription)
		}
		return red.ScrubAndTruncate(strings.Join(parts, ": "))
	}
	if len(body) == 0 {
		return "(empty response body)"
	}
	return red.ScrubAndTruncate(string(body))
}

// secretsLocked is the Secrets view for callers that already hold the
// lock; calling Secrets there would deadlock.
func (t *TokenSource) secretsLocked() []string {
	out := make([]string, 0, 2)
	if t.offlineToken != "" {
		out = append(out, t.offlineToken)
	}
	if t.token != "" {
		out = append(out, t.token)
	}
	return out
}

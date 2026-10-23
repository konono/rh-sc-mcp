package redhat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ssoStub is a stand-in for the Red Hat SSO realm. It counts exchanges so
// a test can tell a cache hit from a refresh, and hands out a different
// token each time so a stale one is visible in a request.
type ssoStub struct {
	*httptest.Server
	exchanges atomic.Int64
	// expiresIn is what the stub reports; tests change it to age a token.
	expiresIn int64
	// lastForm is the form the client posted, for asserting the grant.
	lastForm map[string]string
}

func newSSOStub(t *testing.T) *ssoStub {
	t.Helper()
	s := &ssoStub{expiresIn: 900, lastForm: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		for k := range r.PostForm {
			s.lastForm[k] = r.PostForm.Get(k)
		}
		n := s.exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("access-token-%d", n),
			"expires_in":   s.expiresIn,
			"token_type":   "Bearer",
		})
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestTokenSource(t *testing.T, sso *ssoStub) *TokenSource {
	t.Helper()
	return NewTokenSource(sso.URL, "rhsm-api", fakeOffline, &http.Client{Timeout: 5 * time.Second})
}

func TestTokenExchangeUsesRefreshTokenGrant(t *testing.T) {
	sso := newSSOStub(t)
	ts := newTestTokenSource(t, sso)

	tok, err := ts.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "access-token-1" {
		t.Fatalf("got token %q", tok)
	}
	if got := sso.lastForm["grant_type"]; got != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", got)
	}
	if got := sso.lastForm["client_id"]; got != "rhsm-api" {
		t.Errorf("client_id = %q, want rhsm-api", got)
	}
	if got := sso.lastForm["refresh_token"]; got != fakeOffline {
		t.Errorf("refresh_token was not the offline token")
	}
}

func TestTokenIsCachedUntilExpiry(t *testing.T) {
	sso := newSSOStub(t)
	ts := newTestTokenSource(t, sso)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := ts.Token(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := sso.exchanges.Load(); n != 1 {
		t.Fatalf("exchanged %d times, want 1: the token is not being cached", n)
	}
}

func TestTokenRefreshesAfterExpiry(t *testing.T) {
	sso := newSSOStub(t)
	ts := newTestTokenSource(t, sso)
	ctx := context.Background()

	// A clock the test controls, so expiry can be reached without
	// sleeping through a real 15 minutes.
	now := time.Now()
	ts.now = func() time.Time { return now }

	first, err := ts.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Still inside the lifetime minus the margin.
	now = now.Add(800 * time.Second)
	again, err := ts.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("token changed before expiry: %q then %q", first, again)
	}
	// Past expiry-minus-margin (900s - 60s).
	now = now.Add(100 * time.Second)
	third, err := ts.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("token was not refreshed after expiry")
	}
	if n := sso.exchanges.Load(); n != 2 {
		t.Fatalf("exchanged %d times, want 2", n)
	}
}

// A token whose reported lifetime is shorter than the safety margin must
// not come out with an expiry in the past, which would make every call
// refresh.
func TestShortLivedTokenGetsAFutureExpiry(t *testing.T) {
	sso := newSSOStub(t)
	sso.expiresIn = 30
	ts := newTestTokenSource(t, sso)

	now := time.Now()
	ts.now = func() time.Time { return now }
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ts.expiry.After(now) {
		t.Fatalf("expiry %v is not after %v", ts.expiry, now)
	}
	if ts.expiry.After(now.Add(30 * time.Second)) {
		t.Fatalf("expiry %v outlives the token's reported lifetime", ts.expiry)
	}
}

// A response with no expires_in must not be treated as an eternal token.
func TestMissingExpiresInIsTreatedAsShort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"no-expiry-token"}`))
	}))
	defer srv.Close()

	ts := NewTokenSource(srv.URL, "rhsm-api", fakeOffline, srv.Client())
	now := time.Now()
	ts.now = func() time.Time { return now }
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ts.expiry.After(now) || ts.expiry.After(now.Add(2*expiryMargin)) {
		t.Fatalf("expiry %v is not a short, finite lifetime from %v", ts.expiry, now)
	}
}

func TestInvalidateOnlyClearsTheTokenTheCallerUsed(t *testing.T) {
	sso := newSSOStub(t)
	ts := newTestTokenSource(t, sso)
	ctx := context.Background()

	tok, err := ts.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A late 401 from a request that used an older token must not throw
	// away the token another goroutine has since fetched.
	ts.Invalidate("some-older-token")
	if again, _ := ts.Token(ctx); again != tok {
		t.Fatalf("a stale invalidation discarded the live token")
	}
	if n := sso.exchanges.Load(); n != 1 {
		t.Fatalf("exchanged %d times, want 1", n)
	}

	ts.Invalidate(tok)
	if again, _ := ts.Token(ctx); again == tok {
		t.Fatal("invalidating the live token did not force a refresh")
	}
}

// An SSO failure is the error an operator reads when their offline token
// is revoked, so it has to carry the OAuth fields — and nothing else.
func TestSSOFailureIsInformativeAndScrubbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		// A realm that echoes the submitted token back in its error.
		_, _ = fmt.Fprintf(w, `{"error":"invalid_grant","error_description":"Token is not active: %s"}`, fakeOffline)
	}))
	defer srv.Close()

	ts := NewTokenSource(srv.URL, "rhsm-api", fakeOffline, srv.Client())
	_, err := ts.Token(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if strings.Contains(msg, fakeOffline) {
		t.Fatalf("the offline token leaked into the error: %q", msg)
	}
	for _, want := range []string{"400", "invalid_grant", "Token is not active"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

// A realm that answers an error with HTML, or with nothing, still has to
// produce a usable message rather than a panic or an empty string.
func TestSSOFailureWithNonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>gateway is down</html>"))
	}))
	defer srv.Close()

	ts := NewTokenSource(srv.URL, "rhsm-api", fakeOffline, srv.Client())
	_, err := ts.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("got %v, want an error mentioning 502", err)
	}
}

// A 200 that carries no access_token is a failure, not a token of "".
func TestSSOSuccessWithoutTokenIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"expires_in":900}`))
	}))
	defer srv.Close()

	ts := NewTokenSource(srv.URL, "rhsm-api", fakeOffline, srv.Client())
	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("expected an error for a response with no access_token")
	}
}

// Secrets must include the live access token, or the Redactor cannot
// remove it from an upstream error that quotes the header.
func TestSecretsIncludesTheLiveAccessToken(t *testing.T) {
	sso := newSSOStub(t)
	ts := newTestTokenSource(t, sso)
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sawAccess, sawOffline bool
	for _, s := range ts.Secrets() {
		if s == "access-token-1" {
			sawAccess = true
		}
		if s == fakeOffline {
			sawOffline = true
		}
	}
	if !sawAccess || !sawOffline {
		t.Fatalf("Secrets() = %v, want both the offline and the access token", ts.Secrets())
	}
}

package redhat

import (
	"strings"
	"testing"
)

const (
	fakeOffline = "offline-tok-AAAAAAAAAAAAAAAAAAAA"
	fakeAccess  = "access-tok-BBBBBBBBBBBBBBBBBBBB"
)

func testRedactor() *Redactor {
	return NewRedactor(func() []string { return []string{fakeOffline, fakeAccess} })
}

func TestScrubRemovesHeldSecrets(t *testing.T) {
	r := testRedactor()
	in := "upstream said: refused credential " + fakeOffline + " and " + fakeAccess
	got := r.Scrub(in)
	if strings.Contains(got, fakeOffline) || strings.Contains(got, fakeAccess) {
		t.Fatalf("held secret survived scrubbing: %q", got)
	}
	if !strings.Contains(got, redacted) {
		t.Fatalf("expected a redaction marker, got %q", got)
	}
}

// A token this process never held still has to go: an SSO error body can
// carry a freshly minted one, and an upstream error can quote a header.
func TestScrubRemovesUnseenCredentials(t *testing.T) {
	cases := []struct {
		name string
		in   string
		gone string
	}{
		{
			name: "bearer header echoed back",
			in:   `{"error":"rejected request with Authorization: Bearer abcdefghijklmnopqrstuvwxyz"}`,
			gone: "abcdefghijklmnopqrstuvwxyz",
		},
		{
			name: "access_token in a JSON body",
			in:   `{"access_token":"zzzzzzzzzzzzzzzzzzzzzzzz","error":"boom"}`,
			gone: "zzzzzzzzzzzzzzzzzzzzzzzz",
		},
		{
			name: "refresh_token in a form echo",
			in:   "grant_type=refresh_token&refresh_token=qqqqqqqqqqqqqqqqqqqqqqqq",
			gone: "qqqqqqqqqqqqqqqqqqqqqqqq",
		},
		{
			name: "a JWT anywhere",
			in:   "token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payloadpayload.sig failed",
			gone: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
	}
	// No held secrets: the patterns alone must carry this.
	r := NewRedactor(nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Scrub(tc.in)
			if strings.Contains(got, tc.gone) {
				t.Fatalf("credential survived scrubbing: %q", got)
			}
		})
	}
}

// The field name is worth keeping — "invalid access_token" tells an
// operator which credential to go and look at.
func TestScrubKeepsTheFieldName(t *testing.T) {
	got := NewRedactor(nil).Scrub(`{"access_token":"zzzzzzzzzzzzzzzzzzzz"}`)
	if !strings.Contains(got, "access_token") {
		t.Fatalf("field name was lost: %q", got)
	}
}

// Truncating before scrubbing could cut a token in half and leave the
// first half in the output, where the patterns no longer match it.
func TestScrubAndTruncateScrubsBeforeCutting(t *testing.T) {
	r := testRedactor()
	// Put the secret past the truncation point.
	in := strings.Repeat("x", maxErrorBody-5) + fakeAccess
	got := r.ScrubAndTruncate(in)
	if strings.Contains(got, fakeAccess[:20]) {
		t.Fatalf("a prefix of the secret survived truncation: %q", got[len(got)-80:])
	}
	if len(got) > maxErrorBody+len("…(truncated)") {
		t.Fatalf("output was not truncated: %d bytes", len(got))
	}
}

// A short string would match far too much text; replacing it would mangle
// messages without protecting a real credential.
func TestScrubIgnoresShortSecrets(t *testing.T) {
	r := NewRedactor(func() []string { return []string{"abc"} })
	if got := r.Scrub("abc is not a token"); got != "abc is not a token" {
		t.Fatalf("short secret was treated as a credential: %q", got)
	}
}

package redhat

import (
	"regexp"
	"strings"
)

// redacted is what replaces anything that looks like a credential.
const redacted = "[REDACTED]"

// Error bodies are quoted back to the model, which means they end up in a
// transcript. Keep them short enough to be useful and no longer.
const maxErrorBody = 2000

// Patterns that catch credential-shaped text even when it is not a token
// this process is holding — an upstream error that echoes the request, a
// proxy that quotes a header, an SSO body that returns a fresh token
// alongside the failure.
var redactPatterns = []*regexp.Regexp{
	// Authorization: Bearer <token>, and the bare "Bearer <token>" form.
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{8,}`),
	// JSON or form fields that carry a credential by name.
	regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|id_token|client_secret|offline_token|authorization)"?\s*[:=]\s*"?)([^"'&,\s}]{8,})`),
	// JWTs, which both the access token and the offline token are.
	regexp.MustCompile(`eyJ[A-Za-z0-9._~+/=-]{16,}`),
}

// Redactor removes credentials from text that is about to be shown to a
// model or written to a log.
//
// It works in two layers because either one alone leaks. Exact secrets
// catch a token echoed in a form this process can recognise; the patterns
// catch a token this process has never seen, such as the newly issued one
// in an SSO response that also carried an error. A Redactor with no
// secrets still applies the patterns.
//
// All methods are safe on a nil *Redactor, which then applies the
// patterns only. That matters because this type guards a leak: a code
// path that forgot to wire one up should still be scrubbed, rather than
// panicking in some cases and passing a token through in others.
type Redactor struct {
	// secrets returns the literal credential strings currently held. It is
	// a function because the access token is replaced on every refresh,
	// and a stale snapshot would miss the live one.
	secrets func() []string
}

// NewRedactor builds a Redactor over a live view of the held credentials.
func NewRedactor(secrets func() []string) *Redactor {
	return &Redactor{secrets: secrets}
}

// Scrub removes credentials from s.
func (r *Redactor) Scrub(s string) string {
	if s == "" {
		return s
	}
	if r != nil && r.secrets != nil {
		for _, sec := range r.secrets() {
			// A short "secret" would match far too much; anything that
			// short is not a token and replacing it would mangle the
			// message without protecting anything.
			if len(sec) < 8 {
				continue
			}
			s = strings.ReplaceAll(s, sec, redacted)
		}
	}
	for _, re := range redactPatterns {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			// Keep the field name so the message still says which
			// credential was involved; replace only the value.
			if idx := credentialValueStart(re, m); idx > 0 {
				return m[:idx] + redacted
			}
			return redacted
		})
	}
	return s
}

// ScrubAndTruncate scrubs s and caps it at maxErrorBody bytes.
//
// Truncation happens after scrubbing, never before: cutting first could
// slice a token in half and leave the first half in the output, which the
// patterns would no longer recognise.
func (r *Redactor) ScrubAndTruncate(s string) string {
	s = r.Scrub(s)
	if len(s) <= maxErrorBody {
		return s
	}
	return s[:maxErrorBody] + "…(truncated)"
}

// credentialValueStart returns the offset in m where the credential value
// begins, for the patterns that capture a name prefix. It returns 0 when
// the whole match is the credential.
func credentialValueStart(re *regexp.Regexp, m string) int {
	sub := re.FindStringSubmatchIndex(m)
	// Group 1 is the "name:" prefix in the keyed pattern; the other
	// patterns have no groups, so the whole match is the value.
	if len(sub) >= 4 && sub[2] == 0 && sub[3] > 0 {
		return sub[3]
	}
	return 0
}

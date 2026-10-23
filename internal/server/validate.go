package server

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Argument validation lives here rather than in the schema alone.
//
// The generated JSON Schema tells a well-behaved client what to send, but
// it is advice: a client may skip validation, and a model may hand over a
// field the schema marks optional with a value the API will reject in a
// way that reads as a server fault. Checking here turns those into a
// message that says what to send instead.

// sortFieldRe bounds a value that becomes part of a server-side ORDER BY.
// The API document names no list of sortable fields for the case filter,
// so the field name is not restricted to a set — but it is restricted to
// the characters a field name can contain.
var sortFieldRe = regexp.MustCompile(`^[A-Za-z0-9_.]{1,64}$`)

// commentSortFields are the values the document lists for the comment
// listing, which does name them.
var commentSortFields = []string{"createdDate", "lastModifiedDate", "createdBy"}

// normalizeSortOrder accepts asc/desc in any case and returns the
// upper-case form the case filter documents.
func normalizeSortOrder(v string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "":
		return "", nil
	case "ASC":
		return "ASC", nil
	case "DESC":
		return "DESC", nil
	default:
		return "", fmt.Errorf("sortOrder %q is not valid: use ASC or DESC", clipArg(v))
	}
}

// normalizeCommentSortOrder returns the lower-case form that endpoint
// documents.
func normalizeCommentSortOrder(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return "", nil
	case "asc":
		return "asc", nil
	case "desc":
		return "desc", nil
	default:
		return "", fmt.Errorf("sortOrder %q is not valid: use asc or desc", clipArg(v))
	}
}

func checkSortField(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if !sortFieldRe.MatchString(v) {
		return "", fmt.Errorf(
			"sortField %q is not valid: it must be a field name of letters, digits, underscores or dots, e.g. CreatedDate or LastModifiedDate",
			clipArg(v))
	}
	return v, nil
}

func checkCommentSortField(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	for _, f := range commentSortFields {
		if strings.EqualFold(f, v) {
			return f, nil
		}
	}
	return "", fmt.Errorf("sortField %q is not valid: use one of %s",
		clipArg(v), strings.Join(commentSortFields, ", "))
}

// checkTimestamp validates an ISO 8601 instant, which is what the case
// filter's date ranges take.
//
// Several layouts are accepted because a model will reasonably produce
// any of them from "cases modified since last Tuesday", and rejecting a
// date for its missing timezone helps nobody. The value is passed through
// as written, not reformatted: the API decides how to read it, and
// rewriting it here would hide a mismatch rather than fix one.
func checkTimestamp(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z0700",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if _, err := time.Parse(l, v); err == nil {
			return v, nil
		}
	}
	return "", fmt.Errorf(
		"%s %q is not a valid date: use ISO 8601, e.g. 2026-01-31T00:00:00Z or 2026-01-31",
		field, clipArg(v))
}

// checkDate validates the YYYY-MM-DD form the comment listing documents.
func checkDate(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return "", fmt.Errorf("%s %q is not a valid date: use YYYY-MM-DD, e.g. 2026-01-31", field, clipArg(v))
	}
	return v, nil
}

// checkEnum matches case-insensitively and returns the canonical
// spelling, so "PLAINTEXT" and "plaintext" both reach the API as the
// documented value.
func checkEnum(field, v string, allowed []string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	for _, a := range allowed {
		if strings.EqualFold(a, v) {
			return a, nil
		}
	}
	return "", fmt.Errorf("%s %q is not valid: use one of %s", field, clipArg(v), strings.Join(allowed, ", "))
}

func requireNonEmpty(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("%s is required and must not be empty", field)
	}
	return v, nil
}

// cleanList trims entries and drops empty ones, returning nil for a list
// that had nothing in it. A filter field sent as [""] is not the same as
// an absent one, and the empty form is never what the caller meant.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// clipArg bounds a value quoted back in an error message, so a huge
// argument cannot turn a validation failure into a wall of text.
func clipArg(s string) string {
	const n = 80
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ptr is the one-liner that turns a value into the pointer the partial
// update request needs.
func ptr[T any](v T) *T { return &v }

package redhat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultLifecycleURL is the Product Life Cycle API's product listing.
//
// It is public and needs no credential. It is not described by a published
// API document (its root and openapi.json both answered 401 on
// 2026-11-11), so the shapes below are what the service returned when it
// was checked, not a contract.
const DefaultLifecycleURL = "https://access.redhat.com/product-life-cycles/api/v1/products"

// LifecycleClient reads the Product Life Cycle API.
//
// It is deliberately a different type from Client, with its own
// http.Client and no TokenSource: the endpoint is public, so neither the
// offline token nor an access token has any business travelling to it.
// Keeping the two apart means no code path can attach one by mistake.
type LifecycleClient struct {
	endpoint string
	http     *http.Client
	redactor *Redactor
}

// NewLifecycleClient builds a client for the given product-listing URL.
func NewLifecycleClient(endpoint string, timeout time.Duration) *LifecycleClient {
	return &LifecycleClient{
		endpoint: endpoint,
		http:     &http.Client{Timeout: timeout},
		// No secrets to scrub, but the patterns still run over any error
		// body quoted back, in case an intermediary reflects something.
		redactor: NewRedactor(nil),
	}
}

// LifecyclePhase is one support phase of a product version.
//
// A boundary is either a calendar date (YYYY-MM-DD, in Start or End) or the
// text the API used instead (StartText or EndText, observed as "N/A").
// The API marks which with *_format; text is never read as a date.
type LifecyclePhase struct {
	Name           string `json:"name"`
	Start          string `json:"start,omitempty"`
	StartText      string `json:"startText,omitempty"`
	End            string `json:"end,omitempty"`
	EndText        string `json:"endText,omitempty"`
	AdditionalText string `json:"additionalText,omitempty"`

	start, end time.Time // zero unless the boundary is a date
}

// LifecycleVersion is one product version and its phases.
type LifecycleVersion struct {
	Name string `json:"name"`
	// ReportedType is the API's own label for the version's state. It can
	// lag the dates (2.6 of AAP was "Full Support" on 2026-11-10 while its
	// dates put it in Maintenance 1), so the tool reports it alongside a
	// phase computed from the dates rather than in place of one.
	ReportedType           string           `json:"reportedType,omitempty"`
	AdditionalText         string           `json:"additionalText,omitempty"`
	OpenShiftCompatibility string           `json:"openshiftCompatibility,omitempty"`
	Phases                 []LifecyclePhase `json:"phases"`
}

// LifecycleProduct is one product entry.
type LifecycleProduct struct {
	Name        string             `json:"name"`
	FormerNames []string           `json:"formerNames,omitempty"`
	Link        string             `json:"link,omitempty"`
	Policies    string             `json:"policies,omitempty"`
	IsRetired   bool               `json:"isRetired"`
	Footnote    string             `json:"footnote,omitempty"`
	Versions    []LifecycleVersion `json:"versions"`
}

// LifecycleResult is a parsed product listing.
//
// Exactly one of Products or Unrecognized is meaningful, as with KCSResult.
type LifecycleResult struct {
	Products      []LifecycleProduct   `json:"products"`
	RegeneratedAt string               `json:"regeneratedAt,omitempty"`
	Unrecognized  *UnrecognizedPayload `json:"unrecognizedShape,omitempty"`
}

// lifecycleTextRunes bounds the free-text fields. They are short notes
// (EUS purchase terms and the like) when present; the cap only stops an
// unexpected essay from reaching the model.
const lifecycleTextRunes = 600

// GetProducts fetches the entries whose name matches product exactly.
//
// The API matches name exactly ("Ansible" returns nothing) and ignores a
// version parameter, so none is sent; filtering by version is the
// caller's job.
func (c *LifecycleClient) GetProducts(ctx context.Context, product string) (*LifecycleResult, error) {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("life cycle endpoint %q: %w", c.endpoint, err)
	}
	q := u.Query()
	q.Set("name", product)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build life cycle request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("life cycle API request failed: %s", c.redactor.Scrub(err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read life cycle API response: %s", c.redactor.Scrub(err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body := strings.TrimSpace(string(raw))
		if body == "" {
			body = "(empty response body)"
		}
		return nil, fmt.Errorf("life cycle API returned HTTP %d: %s",
			// Scrub before clipping, as everywhere else in this package.
			resp.StatusCode, firstLine(c.redactor.Scrub(body), 200))
	}
	return parseLifecycleResponse(raw), nil
}

type lcEnvelope struct {
	Data     []map[string]json.RawMessage `json:"data"`
	Metadata struct {
		RegeneratedAt string `json:"regeneratedAt"`
	} `json:"metadata"`
}

// lcVersion and lcPhase hold the fields the tool depends on as pointers,
// so that a missing field can be told apart from an empty one: a phase
// without its *_format is not a phase with no date, it is a shape this
// server does not know.
type lcVersion struct {
	Name                   *string         `json:"name"`
	Type                   string          `json:"type"`
	AdditionalText         string          `json:"additional_text"`
	OpenShiftCompatibility json.RawMessage `json:"openshift_compatibility"`
	Phases                 *[]lcPhase      `json:"phases"`
}

type lcPhase struct {
	Name            *string `json:"name"`
	StartDate       *string `json:"start_date"`
	StartDateFormat *string `json:"start_date_format"`
	EndDate         *string `json:"end_date"`
	EndDateFormat   *string `json:"end_date_format"`
	AdditionalText  string  `json:"additional_text"`
}

// parseLifecycleResponse projects a listing, or reports that it could
// not recognise one. Like parseKCSResponse it never returns an error,
// and it never quotes the body.
//
// Recognition is all or nothing. If any product, version or phase lacks a
// field the dates depend on, or carries a *_format other than "date" or
// "string", the whole response is reported as unrecognised: a partial
// projection would present a phase with no dates as though the API had
// said it has none.
func parseLifecycleResponse(raw []byte) *LifecycleResult {
	unrecognized := func(note string) *LifecycleResult {
		return &LifecycleResult{
			Products: []LifecycleProduct{},
			Unrecognized: &UnrecognizedPayload{
				TopLevelKeys: topLevelKeys(raw),
				SizeBytes:    len(raw),
				Note:         note + "; no part of the body is included here",
			},
		}
	}

	var env lcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Data == nil {
		return unrecognized("the response was not the object with a data array this server projects")
	}

	out := &LifecycleResult{
		Products:      make([]LifecycleProduct, 0, len(env.Data)),
		RegeneratedAt: env.Metadata.RegeneratedAt,
	}
	for _, d := range env.Data {
		p, problem := projectLifecycleProduct(d)
		if problem != "" {
			return unrecognized(problem)
		}
		out.Products = append(out.Products, p)
	}
	return out
}

// projectLifecycleProduct returns the projected product, or a non-empty
// description of the first field that did not have the expected shape.
func projectLifecycleProduct(d map[string]json.RawMessage) (LifecycleProduct, string) {
	var p LifecycleProduct
	if json.Unmarshal(d["name"], &p.Name) != nil || p.Name == "" {
		return p, "a product entry had no name"
	}
	var versions *[]lcVersion
	if json.Unmarshal(d["versions"], &versions) != nil || versions == nil {
		return p, "a product entry had no versions array"
	}
	// Optional fields are read best-effort: a type change in one of them
	// should not cost the dates, which are what the caller came for.
	_ = json.Unmarshal(d["former_names"], &p.FormerNames)
	_ = json.Unmarshal(d["link"], &p.Link)
	_ = json.Unmarshal(d["policies"], &p.Policies)
	_ = json.Unmarshal(d["is_retired"], &p.IsRetired)
	_ = json.Unmarshal(d["footnote"], &p.Footnote)
	p.Footnote = clipText(p.Footnote)

	p.Versions = make([]LifecycleVersion, 0, len(*versions))
	for _, v := range *versions {
		if v.Name == nil || strings.TrimSpace(*v.Name) == "" {
			return p, "a version had no name"
		}
		if v.Phases == nil {
			return p, "a version had no phases array"
		}
		lv := LifecycleVersion{
			Name:           *v.Name,
			ReportedType:   v.Type,
			AdditionalText: clipText(v.AdditionalText),
			Phases:         make([]LifecyclePhase, 0, len(*v.Phases)),
		}
		var oc string
		if json.Unmarshal(v.OpenShiftCompatibility, &oc) == nil {
			lv.OpenShiftCompatibility = clipText(oc)
		}
		for _, ph := range *v.Phases {
			if ph.Name == nil || strings.TrimSpace(*ph.Name) == "" {
				return p, "a phase had no name"
			}
			lp := LifecyclePhase{Name: *ph.Name, AdditionalText: clipText(ph.AdditionalText)}
			var problem string
			if lp.Start, lp.StartText, lp.start, problem = boundary(ph.StartDate, ph.StartDateFormat); problem != "" {
				return p, "a phase start " + problem
			}
			if lp.End, lp.EndText, lp.end, problem = boundary(ph.EndDate, ph.EndDateFormat); problem != "" {
				return p, "a phase end " + problem
			}
			lv.Phases = append(lv.Phases, lp)
		}
		p.Versions = append(p.Versions, lv)
	}
	return p, ""
}

// boundary turns one API date field into either a calendar date or text.
//
// The API states which it is with *_format, and only its two observed
// values are accepted: "date", which must parse as a timestamp, and
// "string", whose value is passed through as text and never read as a
// date. A missing value, a missing or unknown format, or a "date" that
// does not parse is reported as a problem. The API's timestamps are
// midnight UTC, so the UTC calendar date is the date it means.
func boundary(value, format *string) (date, text string, t time.Time, problem string) {
	if value == nil {
		return "", "", time.Time{}, "had no date field"
	}
	if format == nil {
		return "", "", time.Time{}, "had no *_format field"
	}
	v := strings.TrimSpace(*value)
	switch *format {
	case "date":
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return "", "", time.Time{}, "was marked as a date but did not parse as one"
		}
		parsed = parsed.UTC()
		return parsed.Format(time.DateOnly), "", parsed, ""
	case "string":
		return "", clipText(v), time.Time{}, ""
	default:
		return "", "", time.Time{}, "had a *_format other than date or string"
	}
}

func clipText(s string) string {
	s, _ = truncateRunes(strings.TrimSpace(s), lifecycleTextRunes)
	return s
}

// PhaseStatus is the outcome of CurrentPhase.
type PhaseStatus struct {
	// Phase is the name of the dated phase whose window contains the
	// given day, or "" when none does.
	Phase string
	// Note explains an empty Phase.
	Note string
}

// CurrentPhase finds the phase whose dates contain day.
//
// Only phases with both a start and an end date take part. An end date is
// inclusive: the API writes "end 2027-06-02, next starts 2027-06-03", so
// the last day belongs to the phase that ends on it. When no window
// contains the day, the note says why in terms of the dates alone; it
// does not name a state the API did not.
func (v LifecycleVersion) CurrentPhase(now time.Time) PhaseStatus {
	day := now.UTC().Truncate(24 * time.Hour)
	var dated int
	var first, last time.Time
	for _, ph := range v.Phases {
		if ph.start.IsZero() || ph.end.IsZero() {
			continue
		}
		dated++
		if first.IsZero() || ph.start.Before(first) {
			first = ph.start
		}
		if ph.end.After(last) {
			last = ph.end
		}
		if !day.Before(ph.start) && !day.After(ph.end) {
			return PhaseStatus{Phase: ph.Name}
		}
	}
	switch {
	case dated == 0:
		return PhaseStatus{Note: "no phase has both a start and an end date, so none can be placed on the calendar"}
	case day.After(last):
		return PhaseStatus{Note: fmt.Sprintf("every dated phase has ended; the last ended on %s", last.Format(time.DateOnly))}
	case day.Before(first):
		return PhaseStatus{Note: fmt.Sprintf("the first dated phase starts on %s", first.Format(time.DateOnly))}
	default:
		return PhaseStatus{Note: "today falls between dated phases"}
	}
}

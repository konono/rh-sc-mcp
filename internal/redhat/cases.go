package redhat

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// caseNumberRe is the accepted shape of a case number.
//
// Red Hat case numbers are decimal strings, often with leading zeros
// ("04337263"), which is why they are carried as strings rather than
// integers. The upper bound is a sanity limit on input length, not a
// documented maximum; the document gives no format, so this constrains
// the character set — which is what keeps the value from reaching the URL
// path as anything but a path segment — and leaves the length generous.
var caseNumberRe = regexp.MustCompile(`^[0-9]{1,32}$`)

// commentIDRe matches a Salesforce record id (15 or 18 alphanumerics);
// the bound is loosened a little rather than pinned to those two lengths,
// since the document only says "the commentId to lookup for".
var commentIDRe = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

// ValidateCaseNumber checks a case number before it is used to build a
// path. A rejected value never reaches the network.
func ValidateCaseNumber(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" {
		return "", fmt.Errorf("caseNumber is required")
	}
	if !caseNumberRe.MatchString(n) {
		return "", fmt.Errorf(
			"caseNumber %q is not valid: it must be 1-32 digits, e.g. \"04337263\" (leading zeros are significant, so pass it as a string)",
			clip(n, 64))
	}
	return n, nil
}

// ValidateCommentID checks a comment id before it is used to build a path.
func ValidateCommentID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("commentId is required")
	}
	if !commentIDRe.MatchString(id) {
		return "", fmt.Errorf("commentId %q is not valid: it must be 1-64 letters or digits", clip(id, 64))
	}
	return id, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// GetCase fetches one case.
func (c *Client) GetCase(ctx context.Context, caseNumber string) (*Case, error) {
	num, err := ValidateCaseNumber(caseNumber)
	if err != nil {
		return nil, err
	}
	resp, err := c.Get(ctx, c.VersionedPath("cases", num), nil)
	if err != nil {
		return nil, err
	}
	var out Case
	ok, err := resp.DecodeJSON(&out)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("case %s: the API returned HTTP %d with no body", num, resp.Status)
	}
	// Some deployments omit caseNumber from the body of a lookup by case
	// number. Fill it in so the caller never has to decide whether an
	// empty field means "not this case".
	if out.CaseNumber == "" {
		out.CaseNumber = num
	}
	return &out, nil
}

// FilterCases runs a case search.
func (c *Client) FilterCases(ctx context.Context, req *FilterCasesRequest) (*FilterCasesResponse, error) {
	resp, err := c.PostJSON(ctx, c.VersionedPath("cases", "filter"), req)
	if err != nil {
		return nil, err
	}
	var out FilterCasesResponse
	ok, err := resp.DecodeJSON(&out)
	if err != nil {
		return nil, err
	}
	if !ok {
		// An empty 200 is a legitimate "nothing matched" from some
		// gateways, and is reported as an empty result rather than an
		// error — but totalCount stays 0 so the caller is not told a
		// count it was never given.
		return &FilterCasesResponse{}, nil
	}
	return &out, nil
}

// CreateCaseResult is what a successful creation yields.
//
// Location is always present when the API supplied one. CaseNumber is
// best-effort: the documented response is a list of location URIs, and
// while those normally end in the case number, nothing in the document
// promises it. When it cannot be read, the field is empty and the caller
// still has the URI.
type CreateCaseResult struct {
	CaseNumber string   `json:"caseNumber,omitempty"`
	CaseURL    string   `json:"caseUrl,omitempty"`
	Location   []string `json:"location,omitempty"`
	Status     int      `json:"httpStatus"`
}

// CreateCase opens a new support case.
func (c *Client) CreateCase(ctx context.Context, req *CreateCaseRequest) (*CreateCaseResult, error) {
	resp, err := c.PostJSON(ctx, c.VersionedPath("cases"), req)
	if err != nil {
		return nil, err
	}
	// Validate the body even though the location can come from a header.
	//
	// Without this, a gateway that answers the POST with a 200 and an
	// HTML sign-in page would be read as "created, but no location" —
	// the one failure that must not be reported as success, because the
	// caller's next move is either to go looking for a case that does
	// not exist or to open a second one. An empty body stays acceptable
	// (a 201 carrying only a Location header, or a 204).
	var body struct {
		Location []string `json:"location"`
	}
	if _, err := resp.DecodeJSON(&body); err != nil {
		return nil, fmt.Errorf("create case: %w", err)
	}
	loc := resp.Location()
	return &CreateCaseResult{
		CaseNumber: CaseNumberFromLocations(loc),
		Location:   loc,
		Status:     resp.Status,
	}, nil
}

// CaseNumberFromLocations extracts a case number from creation location
// URIs, returning "" when none of them ends in something that looks like
// one.
//
// Only a value that passes the same validation as user input is accepted.
// A URI ending in "cases" or in an opaque id would otherwise be handed
// back as a case number and then used to build further requests.
func CaseNumberFromLocations(locations []string) string {
	for _, l := range locations {
		if n := caseNumberFromURI(l); n != "" {
			return n
		}
	}
	return ""
}

func caseNumberFromURI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Parse when possible so a query string or fragment cannot end up in
	// the extracted segment; fall back to the raw string for a value that
	// is not a URL at all.
	path := raw
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		path = u.Path
	}
	path = strings.TrimRight(path, "/")
	seg := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		seg = path[i+1:]
	}
	if unescaped, err := url.PathUnescape(seg); err == nil {
		seg = unescaped
	}
	if n, err := ValidateCaseNumber(seg); err == nil {
		return n
	}
	return ""
}

// UpdateCase applies a partial update to a case.
func (c *Client) UpdateCase(ctx context.Context, caseNumber string, req *UpdateCaseRequest) (*UpdateCaseResponse, error) {
	num, err := ValidateCaseNumber(caseNumber)
	if err != nil {
		return nil, err
	}
	resp, err := c.PutJSON(ctx, c.VersionedPath("cases", num), req)
	if err != nil {
		return nil, err
	}
	var out UpdateCaseResponse
	if _, err := resp.DecodeJSON(&out); err != nil {
		return nil, err
	}
	// A 200 with no body still means the update was applied; say so
	// rather than returning a blank result that reads like a failure.
	if out.Message == "" && len(out.FieldsNotSentToSalesforce) == 0 {
		out.Message = fmt.Sprintf("case %s updated (HTTP %d, no message returned)", num, resp.Status)
	}
	return &out, nil
}

// CommentQuery are the optional filters on a comment listing.
type CommentQuery struct {
	StartDate string
	EndDate   string
	SortField string
	SortOrder string
}

// ListComments fetches the comments on a case.
func (c *Client) ListComments(ctx context.Context, caseNumber string, q CommentQuery) ([]Comment, error) {
	num, err := ValidateCaseNumber(caseNumber)
	if err != nil {
		return nil, err
	}
	v := url.Values{}
	setIf(v, "startDate", q.StartDate)
	setIf(v, "endDate", q.EndDate)
	setIf(v, "sortField", q.SortField)
	setIf(v, "sortOrder", q.SortOrder)

	resp, err := c.Get(ctx, c.VersionedPath("cases", num, "comments"), v)
	if err != nil {
		return nil, err
	}
	var out []Comment
	if _, err := resp.DecodeJSON(&out); err != nil {
		return nil, err
	}
	// A case with no comments can answer with an empty body rather than
	// "[]"; both mean the same thing.
	return out, nil
}

// GetComment fetches a single comment.
func (c *Client) GetComment(ctx context.Context, caseNumber, commentID string) (*Comment, error) {
	num, err := ValidateCaseNumber(caseNumber)
	if err != nil {
		return nil, err
	}
	id, err := ValidateCommentID(commentID)
	if err != nil {
		return nil, err
	}
	resp, err := c.Get(ctx, c.VersionedPath("cases", num, "comments", id), nil)
	if err != nil {
		return nil, err
	}
	var out Comment
	ok, err := resp.DecodeJSON(&out)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("comment %s on case %s: the API returned HTTP %d with no body", id, num, resp.Status)
	}
	return &out, nil
}

// AddCommentResult carries the created comment plus the bits of the HTTP
// reply worth reporting when the body was thin.
type AddCommentResult struct {
	Comment  *Comment `json:"comment,omitempty"`
	Location []string `json:"location,omitempty"`
	Status   int      `json:"httpStatus"`
}

// AddComment posts a comment to a case.
func (c *Client) AddComment(ctx context.Context, caseNumber string, req *CreateCommentRequest) (*AddCommentResult, error) {
	num, err := ValidateCaseNumber(caseNumber)
	if err != nil {
		return nil, err
	}
	// The document allows caseNumber in the body but requires it to match
	// the path. Setting it here from the already-validated path value
	// keeps the two from disagreeing.
	req.CaseNumber = num

	resp, err := c.PostJSON(ctx, c.VersionedPath("cases", num, "comments"), req)
	if err != nil {
		return nil, err
	}
	res := &AddCommentResult{Status: resp.Status, Location: resp.Location()}
	var cm Comment
	ok, err := resp.DecodeJSON(&cm)
	if err != nil {
		return nil, err
	}
	if ok {
		if cm.CaseNumber == "" {
			cm.CaseNumber = num
		}
		res.Comment = &cm
	}
	return res, nil
}

// SearchKCS queries the knowledge-base search endpoint.
//
// The response is Solr's, which the document does not model ("200: Solr
// Response"), so it is returned as decoded JSON instead of a struct that
// would be guesswork.
func (c *Client) SearchKCS(ctx context.Context, req *SolrSearchRequest) (any, error) {
	resp, err := c.PostJSON(ctx, c.UnversionedPath("search", "v2", "kcs"), req)
	if err != nil {
		return nil, err
	}
	var out any
	ok, err := resp.DecodeJSON(&out)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]any{}, nil
	}
	return out, nil
}

func setIf(v url.Values, key, val string) {
	if s := strings.TrimSpace(val); s != "" {
		v.Set(key, s)
	}
}

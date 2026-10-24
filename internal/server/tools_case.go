package server

import (
	"context"
	"fmt"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
)

func (s *Server) registerAll() {
	s.registerScope()
	s.registerCase()
	s.registerComment()
	s.registerKCS()
}

// ---- rhcase_scope ---------------------------------------------------------

type scopeArgs struct{}

type scopeResult struct {
	Server      string            `json:"server"`
	Version     string            `json:"version"`
	Endpoint    string            `json:"endpoint"`
	APIVersion  string            `json:"apiVersion"`
	ReadOnly    bool              `json:"readOnly"`
	Permissions map[string]string `json:"permissions"`
	Tools       []string          `json:"tools"`
	MaxResults  int               `json:"maxResultsPerSearch"`
	MaxComments int               `json:"maxCommentsPerListing"`
	Warnings    []string          `json:"warnings,omitempty"`
}

// registerScope exposes what this server may do.
//
// It is always registered, at no permission level, because a model that
// cannot see the boundary tends to probe it: without this, "create a
// case" on a read-only server becomes a call that fails, a guess, and
// another call. One cheap tool answers the question up front.
func (s *Server) registerScope() {
	add(s, toolDef[scopeArgs]{
		name:         "rhcase_scope",
		title:        "Show permitted scope",
		desc:         "Report what this MCP server is allowed to do: the API endpoint, the permission level for each resource, the registered tools and the result limits. Call this before assuming a write is possible.",
		resource:     config.ResCase,
		need:         config.LevelNone,
		readOnlyHint: true,
		handler: func(_ context.Context, _ scopeArgs) (any, error) {
			perms := make(map[string]string, len(config.AllResources))
			for _, r := range config.AllResources {
				perms[string(r)] = s.cfg.LevelFor(r).String()
			}
			return scopeResult{
				Server:      "rh-sc-mcp",
				Version:     Version,
				Endpoint:    s.cfg.BaseURL,
				APIVersion:  s.cfg.APIVersion,
				ReadOnly:    s.cfg.IsReadOnly(),
				Permissions: perms,
				Tools:       s.Registered(),
				MaxResults:  s.cfg.MaxResults,
				MaxComments: s.cfg.MaxComments,
				Warnings:    s.cfg.Warnings(),
			}, nil
		},
	})
}

// ---- case tools -----------------------------------------------------------

type searchArgs struct {
	Keyword  string `json:"keyword,omitempty" jsonschema:"Free-text search across cases"`
	Status   string `json:"status,omitempty" jsonschema:"Single status to match, e.g. Waiting on Red Hat"`
	Severity string `json:"severity,omitempty" jsonschema:"Single severity to match, e.g. 4 (Low)"`
	Product  string `json:"product,omitempty" jsonschema:"Single product name to match, e.g. Red Hat Enterprise Linux"`
	Version  string `json:"version,omitempty" jsonschema:"Product version; only meaningful together with a single product"`
	CaseType string `json:"caseType,omitempty" jsonschema:"Case type, e.g. Defect / Bug"`

	Statuses   []string `json:"statuses,omitempty" jsonschema:"Match any of these statuses"`
	Severities []string `json:"severities,omitempty" jsonschema:"Match any of these severities"`
	Products   []string `json:"products,omitempty" jsonschema:"Match any of these products"`

	CaseNumbers    []string `json:"caseNumbers,omitempty" jsonschema:"Fetch these case numbers specifically; digit strings with leading zeros preserved"`
	AccountNumbers []string `json:"accountNumbers,omitempty" jsonschema:"Limit to these Red Hat account numbers"`
	GroupNumbers   []string `json:"groupNumbers,omitempty" jsonschema:"Limit to these case group numbers"`
	SBRGroups      string   `json:"sbrGroups,omitempty" jsonschema:"Comma-separated SBR group names"`
	Tags           string   `json:"tags,omitempty" jsonschema:"Tags to match"`
	CaseLanguage   string   `json:"caseLanguage,omitempty" jsonschema:"Case language, e.g. en"`

	CreatedBySSOName string `json:"createdBySSOName,omitempty" jsonschema:"Limit to cases opened by this SSO username"`
	OwnerSSOName     string `json:"ownerSSOName,omitempty" jsonschema:"Limit to cases owned by this SSO username"`

	StartDate        string `json:"startDate,omitempty" jsonschema:"Created on or after this ISO 8601 date"`
	EndDate          string `json:"endDate,omitempty" jsonschema:"Created on or before this ISO 8601 date"`
	LastModifiedFrom string `json:"lastModifiedFrom,omitempty" jsonschema:"Last modified on or after this ISO 8601 date"`
	LastModifiedTo   string `json:"lastModifiedTo,omitempty" jsonschema:"Last modified on or before this ISO 8601 date"`

	IncludeClosed      *bool `json:"includeClosed,omitempty" jsonschema:"Include closed cases; the API omits them by default"`
	OnlyEscalated      *bool `json:"onlyEscalated,omitempty" jsonschema:"Only escalated cases"`
	CustomerEscalation *bool `json:"customerEscalation,omitempty" jsonschema:"Only cases with a customer escalation"`

	MaxResults int    `json:"maxResults,omitempty" jsonschema:"Maximum cases to return; capped by the server configuration"`
	Offset     int    `json:"offset,omitempty" jsonschema:"Number of cases to skip, for paging"`
	SortField  string `json:"sortField,omitempty" jsonschema:"Field to sort by; defaults to CreatedDate"`
	SortOrder  string `json:"sortOrder,omitempty" jsonschema:"ASC or DESC; defaults to DESC"`
}

type searchResult struct {
	TotalCount int                  `json:"totalCount"`
	Returned   int                  `json:"returned"`
	Offset     int                  `json:"offset"`
	Cases      []redhat.CaseSummary `json:"cases"`
	Notes      []string             `json:"notes,omitempty"`
}

type getArgs struct {
	CaseNumber      string `json:"caseNumber" jsonschema:"Case number as a string; leading zeros are significant, e.g. 04337263"`
	IncludeComments *bool  `json:"includeComments,omitempty" jsonschema:"Include the comment history in the result; defaults to true"`
}

type getResult struct {
	Case    *redhat.Case `json:"case"`
	CaseURL string       `json:"caseUrl,omitempty"`
	Notes   []string     `json:"notes,omitempty"`
}

type createArgs struct {
	Summary     string `json:"summary" jsonschema:"Case subject line; required"`
	Description string `json:"description" jsonschema:"Full description of the problem; required"`
	Product     string `json:"product" jsonschema:"Product name, e.g. Red Hat Enterprise Linux; required"`
	Version     string `json:"version" jsonschema:"Product version, e.g. 9.4; required"`

	Severity         string `json:"severity,omitempty" jsonschema:"Severity, e.g. 4 (Low); the system picks a default when omitted"`
	CaseType         string `json:"caseType,omitempty" jsonschema:"Case type, e.g. Defect / Bug"`
	AccountNumberRef string `json:"accountNumberRef,omitempty" jsonschema:"Red Hat account number to open the case against"`
	ContactSSOName   string `json:"contactSSOName,omitempty" jsonschema:"SSO username of the case contact"`
	GroupNumber      string `json:"groupNumber,omitempty" jsonschema:"Case group number"`
	CaseLanguage     string `json:"caseLanguage,omitempty" jsonschema:"Case language, e.g. en"`

	OpenshiftClusterID      string `json:"openshiftClusterID,omitempty" jsonschema:"OpenShift cluster ID, for OpenShift cases"`
	OpenshiftClusterVersion string `json:"openshiftClusterVersion,omitempty" jsonschema:"OpenShift cluster version"`
	Hostname                string `json:"hostname,omitempty" jsonschema:"Hostname affected by the issue"`
	Environment             string `json:"environment,omitempty" jsonschema:"Environment in which the issue occurs"`
	Issue                   string `json:"issue,omitempty" jsonschema:"Specific issue description"`
	Notes                   string `json:"notes,omitempty" jsonschema:"Customer notes"`
	ActionPlan              string `json:"actionPlan,omitempty" jsonschema:"Action plan for the case"`
	PeriodicityOfIssue      string `json:"periodicityOfIssue,omitempty" jsonschema:"How often the issue occurs"`
	TimeFramesAndUrgency    string `json:"timeFramesAndUrgency,omitempty" jsonschema:"Time frames and urgency"`

	CustomerEscalation *bool `json:"customerEscalation,omitempty" jsonschema:"Mark the case as a customer escalation"`
}

type createResult struct {
	CaseNumber string   `json:"caseNumber,omitempty"`
	CaseURL    string   `json:"caseUrl,omitempty"`
	Location   []string `json:"location,omitempty"`
	HTTPStatus int      `json:"httpStatus"`
	Notes      []string `json:"notes,omitempty"`
}

type updateArgs struct {
	CaseNumber string `json:"caseNumber" jsonschema:"Case number as a string, e.g. 04337263"`

	// Every updatable field is a pointer so that an omitted key leaves the
	// field alone while an empty string clears it.
	Summary     *string `json:"summary,omitempty" jsonschema:"New subject line; omit to leave unchanged"`
	Description *string `json:"description,omitempty" jsonschema:"New description; omit to leave unchanged"`
	Status      *string `json:"status,omitempty" jsonschema:"New status, e.g. Closed; omit to leave unchanged"`
	Severity    *string `json:"severity,omitempty" jsonschema:"New severity, e.g. 2 (High); omit to leave unchanged"`
	Product     *string `json:"product,omitempty" jsonschema:"New product; omit to leave unchanged"`
	Version     *string `json:"version,omitempty" jsonschema:"New product version; omit to leave unchanged"`
	CaseType    *string `json:"caseType,omitempty" jsonschema:"New case type; omit to leave unchanged"`

	AccountNumberRef *string `json:"accountNumberRef,omitempty" jsonschema:"New account number; omit to leave unchanged"`
	ContactSSOName   *string `json:"contactSSOName,omitempty" jsonschema:"New contact SSO username; omit to leave unchanged"`
	GroupNumber      *string `json:"groupNumber,omitempty" jsonschema:"New case group number; omit to leave unchanged"`
	CaseLanguage     *string `json:"caseLanguage,omitempty" jsonschema:"New case language; omit to leave unchanged"`

	OpenshiftClusterID      *string `json:"openshiftClusterID,omitempty" jsonschema:"New OpenShift cluster ID; omit to leave unchanged"`
	OpenshiftClusterVersion *string `json:"openshiftClusterVersion,omitempty" jsonschema:"New OpenShift cluster version; omit to leave unchanged"`
	Hostname                *string `json:"hostname,omitempty" jsonschema:"New hostname; omit to leave unchanged"`
	Environment             *string `json:"environment,omitempty" jsonschema:"New environment description; omit to leave unchanged"`
	Issue                   *string `json:"issue,omitempty" jsonschema:"New issue description; omit to leave unchanged"`
	Notes                   *string `json:"notes,omitempty" jsonschema:"New customer notes; omit to leave unchanged"`
	PeriodicityOfIssue      *string `json:"periodicityOfIssue,omitempty" jsonschema:"New periodicity; omit to leave unchanged"`
	TimeFramesAndUrgency    *string `json:"timeFramesAndUrgency,omitempty" jsonschema:"New time frames and urgency; omit to leave unchanged"`
	ReopenedReason          *string `json:"reopenedReason,omitempty" jsonschema:"Reason for reopening, when moving a closed case back to open"`

	CustomerEscalation *bool `json:"customerEscalation,omitempty" jsonschema:"Set or clear the customer escalation flag"`
}

type updateResult struct {
	CaseNumber                string         `json:"caseNumber"`
	CaseURL                   string         `json:"caseUrl,omitempty"`
	Message                   string         `json:"message,omitempty"`
	FieldsNotSentToSalesforce map[string]any `json:"fieldsNotSentToSalesforce,omitempty"`
	Notes                     []string       `json:"notes,omitempty"`
}

func (s *Server) registerCase() {
	add(s, toolDef[searchArgs]{
		name:  "rhcase_search",
		title: "Search support cases",
		desc: "Search Red Hat support cases by keyword, status, severity, product, owner, account or date range. " +
			"Returns a compact row per case; call rhcase_get for the full record including the description. " +
			"Closed cases are excluded unless includeClosed is true.",
		resource:     config.ResCase,
		need:         config.LevelRead,
		readOnlyHint: true,
		handler:      s.handleSearch,
	})

	add(s, toolDef[getArgs]{
		name:         "rhcase_get",
		title:        "Get a support case",
		desc:         "Fetch one Red Hat support case by case number, including its description and (unless disabled) its comment history.",
		resource:     config.ResCase,
		need:         config.LevelRead,
		readOnlyHint: true,
		handler:      s.handleGet,
	})

	add(s, toolDef[createArgs]{
		name:  "rhcase_create",
		title: "Open a support case",
		desc: "Open a new Red Hat support case. This is visible to Red Hat support and to everyone on the account, and it cannot be withdrawn — " +
			"confirm the summary, description, product and version with the user before calling. Returns the location URI, and the case number when it can be read from it.",
		resource: config.ResCase,
		need:     config.LevelWrite,
		handler:  s.handleCreate,
	})

	add(s, toolDef[updateArgs]{
		name:  "rhcase_update",
		title: "Update a support case",
		desc: "Change fields on an existing Red Hat support case. Only the fields passed are modified; omitted fields are left alone, and an empty string clears a field. " +
			"Changes are visible to Red Hat support, so confirm them with the user first.",
		resource: config.ResCase,
		need:     config.LevelWrite,
		handler:  s.handleUpdate,
	})
}

func (s *Server) handleSearch(ctx context.Context, in searchArgs) (any, error) {
	req := &redhat.FilterCasesRequest{
		Keyword:          in.Keyword,
		Status:           in.Status,
		Severity:         in.Severity,
		Product:          in.Product,
		Version:          in.Version,
		Type:             in.CaseType,
		Statuses:         cleanList(in.Statuses),
		Severities:       cleanList(in.Severities),
		Products:         cleanList(in.Products),
		AccountNumbers:   cleanList(in.AccountNumbers),
		GroupNumbers:     cleanList(in.GroupNumbers),
		SBRGroups:        in.SBRGroups,
		Tags:             in.Tags,
		CaseLanguage:     in.CaseLanguage,
		CreatedBySSOName: in.CreatedBySSOName,
		OwnerSSOName:     in.OwnerSSOName,

		IncludeClosed:     in.IncludeClosed,
		OnlyEscalated:     in.OnlyEscalated,
		CustomerEscalated: in.CustomerEscalation,
	}

	// Case numbers are validated even though they travel in the body
	// rather than the path: a malformed one is a mistake worth reporting
	// as such, instead of an empty result the model would read as "no
	// such case".
	for _, n := range cleanList(in.CaseNumbers) {
		v, err := redhat.ValidateCaseNumber(n)
		if err != nil {
			return nil, fmt.Errorf("caseNumbers: %w", err)
		}
		req.CaseNumbers = append(req.CaseNumbers, v)
	}

	var err error
	if req.StartDate, err = checkTimestamp("startDate", in.StartDate); err != nil {
		return nil, err
	}
	if req.EndDate, err = checkTimestamp("endDate", in.EndDate); err != nil {
		return nil, err
	}
	if req.LastModifiedFrom, err = checkTimestamp("lastModifiedFrom", in.LastModifiedFrom); err != nil {
		return nil, err
	}
	if req.LastModifiedTo, err = checkTimestamp("lastModifiedTo", in.LastModifiedTo); err != nil {
		return nil, err
	}
	if req.SortField, err = checkSortField(in.SortField); err != nil {
		return nil, err
	}
	if req.SortOrder, err = normalizeSortOrder(in.SortOrder); err != nil {
		return nil, err
	}
	if in.Offset < 0 {
		return nil, fmt.Errorf("offset %d is not valid: it must not be negative", in.Offset)
	}
	// The API refuses a filter that narrows nothing, with a message that
	// does not say which argument was missing. Catching it here names
	// the problem instead of passing an opaque 400 to the model.
	if !req.HasCriteria() {
		return nil, fmt.Errorf(
			"no search criteria: pass at least one of keyword, status, severity, product, caseNumbers, " +
				"accountNumbers, a date range or another filter. maxResults, offset and sorting only shape " +
				"the results, so the API rejects a request that has nothing else")
	}
	req.Offset = in.Offset
	req.MaxResults = s.cfg.ClampMaxResults(in.MaxResults)

	resp, err := s.api.FilterCases(ctx, req)
	if err != nil {
		return nil, err
	}

	out := searchResult{
		TotalCount: resp.TotalCount,
		Returned:   len(resp.Cases),
		Offset:     req.Offset,
		Cases:      make([]redhat.CaseSummary, 0, len(resp.Cases)),
	}
	for i := range resp.Cases {
		sum := resp.Cases[i].Summarize()
		sum.CaseURL = s.cfg.CaseURL(sum.CaseNumber)
		out.Cases = append(out.Cases, sum)
	}
	if in.MaxResults > 0 && req.MaxResults < in.MaxResults {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"maxResults was reduced from %d to the server's configured limit of %d",
			in.MaxResults, req.MaxResults))
	}
	if resp.TotalCount > req.Offset+len(resp.Cases) {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"%d cases matched; this page has %d. Raise offset to %d for the next page",
			resp.TotalCount, len(resp.Cases), req.Offset+len(resp.Cases)))
	}
	out.Notes = append(out.Notes,
		"Rows are abbreviated; call rhcase_get with a caseNumber for the description and comments.")
	return out, nil
}

func (s *Server) handleGet(ctx context.Context, in getArgs) (any, error) {
	num, err := redhat.ValidateCaseNumber(in.CaseNumber)
	if err != nil {
		return nil, err
	}
	c, err := s.api.GetCase(ctx, num)
	if err != nil {
		return nil, err
	}

	out := getResult{Case: c, CaseURL: s.cfg.CaseURL(c.CaseNumber)}

	// The case endpoint embeds the whole comment history, which on a long
	// case is most of the payload. Dropping or trimming it here is the
	// only place it can be done — the API has no parameter for it — and
	// the note tells the model where the rest is.
	if in.IncludeComments != nil && !*in.IncludeComments {
		if n := len(c.Comments); n > 0 {
			out.Notes = append(out.Notes, fmt.Sprintf(
				"%d comments omitted at your request; call rhcase_list_comments to read them", n))
		}
		c.Comments = nil
	} else if len(c.Comments) > s.cfg.MaxComments {
		total := len(c.Comments)
		c.Comments = c.Comments[:s.cfg.MaxComments]
		out.Notes = append(out.Notes, fmt.Sprintf(
			"showing the first %d of %d comments; call rhcase_list_comments with a date range for the rest",
			s.cfg.MaxComments, total))
	}
	return out, nil
}

func (s *Server) handleCreate(ctx context.Context, in createArgs) (any, error) {
	req := &redhat.CreateCaseRequest{
		Severity:                in.Severity,
		CaseType:                in.CaseType,
		AccountNumberRef:        in.AccountNumberRef,
		ContactSSOName:          in.ContactSSOName,
		GroupNumber:             in.GroupNumber,
		CaseLanguage:            in.CaseLanguage,
		OpenshiftClusterID:      in.OpenshiftClusterID,
		OpenshiftClusterVersion: in.OpenshiftClusterVersion,
		Hostname:                in.Hostname,
		Environment:             in.Environment,
		Issue:                   in.Issue,
		Notes:                   in.Notes,
		ActionPlan:              in.ActionPlan,
		PeriodicityOfIssue:      in.PeriodicityOfIssue,
		TimeFramesAndUrgency:    in.TimeFramesAndUrgency,
		CustomerEscalation:      in.CustomerEscalation,
		// Marks the channel, so a case opened through this server is
		// distinguishable from one filed by hand in the console.
		Origin: "API",
	}

	var err error
	// The four the document marks required are checked here so the
	// failure names the field, rather than arriving as a 500 from a
	// Salesforce validation rule.
	if req.Summary, err = requireNonEmpty("summary", in.Summary); err != nil {
		return nil, err
	}
	if req.Description, err = requireNonEmpty("description", in.Description); err != nil {
		return nil, err
	}
	if req.Product, err = requireNonEmpty("product", in.Product); err != nil {
		return nil, err
	}
	if req.Version, err = requireNonEmpty("version", in.Version); err != nil {
		return nil, err
	}

	res, err := s.api.CreateCase(ctx, req)
	if err != nil {
		return nil, err
	}

	out := createResult{
		CaseNumber: res.CaseNumber,
		CaseURL:    s.cfg.CaseURL(res.CaseNumber),
		Location:   res.Location,
		HTTPStatus: res.Status,
	}
	switch {
	case out.CaseNumber != "":
		out.Notes = append(out.Notes, "The case was created. Use this caseNumber for follow-up calls.")
	case len(out.Location) > 0:
		out.Notes = append(out.Notes,
			"The case was created, but its number could not be read from the location URI. "+
				"Open the URI, or find the case with rhcase_search, before making follow-up calls.")
	default:
		out.Notes = append(out.Notes, fmt.Sprintf(
			"The API accepted the request (HTTP %d) but returned no location. "+
				"Confirm with rhcase_search before retrying, so a second case is not opened.", res.Status))
	}
	return out, nil
}

func (s *Server) handleUpdate(ctx context.Context, in updateArgs) (any, error) {
	num, err := redhat.ValidateCaseNumber(in.CaseNumber)
	if err != nil {
		return nil, err
	}
	req := &redhat.UpdateCaseRequest{
		Summary:                 in.Summary,
		Description:             in.Description,
		Status:                  in.Status,
		Severity:                in.Severity,
		Product:                 in.Product,
		Version:                 in.Version,
		CaseType:                in.CaseType,
		AccountNumberRef:        in.AccountNumberRef,
		ContactSSOName:          in.ContactSSOName,
		GroupNumber:             in.GroupNumber,
		CaseLanguage:            in.CaseLanguage,
		OpenshiftClusterID:      in.OpenshiftClusterID,
		OpenshiftClusterVersion: in.OpenshiftClusterVersion,
		Hostname:                in.Hostname,
		Environment:             in.Environment,
		Issue:                   in.Issue,
		Notes:                   in.Notes,
		PeriodicityOfIssue:      in.PeriodicityOfIssue,
		TimeFramesAndUrgency:    in.TimeFramesAndUrgency,
		ReopenedReason:          in.ReopenedReason,
		CustomerEscalation:      in.CustomerEscalation,
	}
	// An update with no fields would be a PUT that reports success while
	// changing nothing — the worst answer to give a model deciding
	// whether its edit landed.
	if req.IsEmpty() {
		return nil, fmt.Errorf(
			"no fields to update: pass at least one field besides caseNumber (an empty string clears a field, omitting it leaves it alone)")
	}
	// A summary cleared to "" would leave the case without a subject,
	// which the backend rejects in a way that does not say so.
	if req.Summary != nil && *req.Summary == "" {
		return nil, fmt.Errorf("summary cannot be cleared: pass a non-empty summary, or omit the field to leave it unchanged")
	}

	res, err := s.api.UpdateCase(ctx, num, req)
	if err != nil {
		return nil, err
	}
	out := updateResult{
		CaseNumber:                num,
		CaseURL:                   s.cfg.CaseURL(num),
		Message:                   res.Message,
		FieldsNotSentToSalesforce: res.FieldsNotSentToSalesforce,
	}
	// The API reports fields it accepted but did not forward. Silence
	// there would read as a complete update.
	if len(res.FieldsNotSentToSalesforce) > 0 {
		out.Notes = append(out.Notes,
			"Some fields were accepted by the API but not applied to the case; they are listed in fieldsNotSentToSalesforce. "+
				"Re-read the case with rhcase_get to confirm what actually changed.")
	}
	return out, nil
}

package redhat

// The structs here mirror the Case Management API document (Swagger 2.0,
// "Support Services" v3). Field names and JSON keys follow the document,
// so a reader can line a struct up against the definition it came from.
//
// Two deliberate departures:
//
//   - Responses are projections, not the whole definition. GetCaseResponse
//     has ~75 properties and CaseResourceLinkDto ~55, most of them internal
//     scoring and moderation metadata. Everything modelled below is a field
//     a support engineer would act on; the rest is dropped on the way out,
//     because a tool result is read into a model's context and padding it
//     with analysisAlgorithmVersion costs tokens and buys nothing.
//   - Optional request fields are pointers. The update endpoint treats an
//     absent key and an empty one differently — absent means "leave it
//     alone" — so a plain string could not express the difference.

// Case is a support case, projected from GetCaseResponse.
type Case struct {
	CaseNumber string `json:"caseNumber,omitempty"`
	ID         string `json:"id,omitempty"`
	URI        string `json:"uri,omitempty"`

	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Product     string `json:"product,omitempty"`
	Version     string `json:"version,omitempty"`
	CaseType    string `json:"caseType,omitempty"`
	IsClosed    *bool  `json:"isClosed,omitempty"`

	CreatedByID      string `json:"createdById,omitempty"`
	CreatedDate      string `json:"createdDate,omitempty"`
	LastModifiedByID string `json:"lastModifiedById,omitempty"`
	LastModifiedDate string `json:"lastModifiedDate,omitempty"`
	LastClosedAt     string `json:"lastClosedAt,omitempty"`

	AccountNumberRef string `json:"accountNumberRef,omitempty"`
	ContactName      string `json:"contactName,omitempty"`
	ContactSSOName   string `json:"contactSSOName,omitempty"`
	OwnerID          string `json:"ownerId,omitempty"`
	GroupName        string `json:"groupName,omitempty"`
	GroupNumber      string `json:"groupNumber,omitempty"`

	OpenshiftClusterID      string `json:"openshiftClusterID,omitempty"`
	OpenshiftClusterVersion string `json:"openshiftClusterVersion,omitempty"`
	Hostname                string `json:"hostname,omitempty"`
	Environment             string `json:"environment,omitempty"`
	Issue                   string `json:"issue,omitempty"`
	PeriodicityOfIssue      string `json:"periodicityOfIssue,omitempty"`
	TimeFramesAndUrgency    string `json:"timeFramesAndUrgency,omitempty"`

	Notes                 string `json:"notes,omitempty"`
	ActionPlan            string `json:"actionPlan,omitempty"`
	ResolutionDescription string `json:"resolutionDescription,omitempty"`

	CaseLanguage       string `json:"caseLanguage,omitempty"`
	EntitlementSLA     string `json:"entitlementSla,omitempty"`
	CustomerEscalation *bool  `json:"customerEscalation,omitempty"`
	FTS                *bool  `json:"fts,omitempty"`
	IsStrategicAccount *bool  `json:"isStrategicAccount,omitempty"`
	CEP                *bool  `json:"cep,omitempty"`

	SBRGroups                 []string `json:"sbrGroups,omitempty"`
	APITags                   []string `json:"apiTags,omitempty"`
	NotificationAddressEmails []string `json:"notificationAddressEmails,omitempty"`

	NotifiedUsers     []NotifiedUser     `json:"notifiedUsers,omitempty"`
	CaseResourceLinks []CaseResourceLink `json:"caseResourceLinks,omitempty"`
	Bugzillas         []Bugzilla         `json:"bugzillas,omitempty"`
	Comments          []Comment          `json:"comments,omitempty"`
}

// NotifiedUser mirrors NotifiedUserDto in full.
type NotifiedUser struct {
	SSOUsername string `json:"ssoUsername,omitempty"`
	Title       string `json:"title,omitempty"`
	Type        string `json:"type,omitempty"`
}

// CaseResourceLink is the useful projection of CaseResourceLinkDto: what
// was linked to the case and where to read it.
type CaseResourceLink struct {
	ResourceType    string `json:"resourceType,omitempty"`
	ResourceID      string `json:"resourceId,omitempty"`
	ResourceViewURI string `json:"resourceViewURI,omitempty"`
	Title           string `json:"title,omitempty"`
	Linked          *bool  `json:"linked,omitempty"`
	CreatedDate     string `json:"createdDate,omitempty"`
}

// Bugzilla is the useful projection of BugzillaDto.
type Bugzilla struct {
	BugzillaNumber string `json:"bugzillaNumber,omitempty"`
	BugzillaURL    string `json:"bugzillaURL,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Status         string `json:"status,omitempty"`
	Resolution     string `json:"resolution,omitempty"`
	Product        string `json:"product,omitempty"`
	Component      string `json:"component,omitempty"`
	IsOpen         *bool  `json:"isOpen,omitempty"`
}

// Comment mirrors CommentDto in full.
type Comment struct {
	ID                string `json:"id,omitempty"`
	CaseNumber        string `json:"caseNumber,omitempty"`
	CommentBody       string `json:"commentBody,omitempty"`
	ContentType       string `json:"contentType,omitempty"`
	CreatedBy         string `json:"createdBy,omitempty"`
	CreatedByType     string `json:"createdByType,omitempty"`
	CreatedDate       string `json:"createdDate,omitempty"`
	LastModifiedByID  string `json:"lastModifiedById,omitempty"`
	LastModifiedDate  string `json:"lastModifiedDate,omitempty"`
	PublishedDate     string `json:"publishedDate,omitempty"`
	IsDraft           *bool  `json:"isDraft,omitempty"`
	DoNotChangeStatus *bool  `json:"doNotChangeStatus,omitempty"`
}

// CaseSummary is the per-row shape returned by a search.
//
// The filter endpoint answers with whole GetCaseResponse objects, each of
// which can carry a description and a comment history. Fifty of those is
// not a search result, it is a transcript; so the rows are narrowed to
// what someone scans before deciding which case to open, and rhcase_get
// fetches the rest.
type CaseSummary struct {
	CaseNumber       string `json:"caseNumber,omitempty"`
	Summary          string `json:"summary,omitempty"`
	Status           string `json:"status,omitempty"`
	Severity         string `json:"severity,omitempty"`
	Product          string `json:"product,omitempty"`
	Version          string `json:"version,omitempty"`
	CaseType         string `json:"caseType,omitempty"`
	OwnerID          string `json:"ownerId,omitempty"`
	ContactName      string `json:"contactName,omitempty"`
	AccountNumberRef string `json:"accountNumberRef,omitempty"`
	CreatedDate      string `json:"createdDate,omitempty"`
	LastModifiedDate string `json:"lastModifiedDate,omitempty"`
	IsClosed         *bool  `json:"isClosed,omitempty"`
	URI              string `json:"uri,omitempty"`
	CaseURL          string `json:"caseUrl,omitempty"`
}

// Summarize narrows a Case to the search-result projection.
func (c *Case) Summarize() CaseSummary {
	return CaseSummary{
		CaseNumber:       c.CaseNumber,
		Summary:          c.Summary,
		Status:           c.Status,
		Severity:         c.Severity,
		Product:          c.Product,
		Version:          c.Version,
		CaseType:         c.CaseType,
		OwnerID:          c.OwnerID,
		ContactName:      c.ContactName,
		AccountNumberRef: c.AccountNumberRef,
		CreatedDate:      c.CreatedDate,
		LastModifiedDate: c.LastModifiedDate,
		IsClosed:         c.IsClosed,
		URI:              c.URI,
	}
}

// FilterCasesRequest mirrors FilterCasesV3Request, limited to the criteria
// the search tool exposes.
type FilterCasesRequest struct {
	Keyword  string `json:"keyword,omitempty"`
	Status   string `json:"status,omitempty"`
	Severity string `json:"severity,omitempty"`
	Product  string `json:"product,omitempty"`
	Version  string `json:"version,omitempty"`
	Type     string `json:"type,omitempty"`

	Statuses   []string `json:"statuses,omitempty"`
	Severities []string `json:"severities,omitempty"`
	Products   []string `json:"products,omitempty"`

	CaseNumbers    []string `json:"caseNumbers,omitempty"`
	AccountNumbers []string `json:"accountNumbers,omitempty"`
	GroupNumbers   []string `json:"groupNumbers,omitempty"`
	SBRGroups      string   `json:"sbrGroups,omitempty"`
	Tags           string   `json:"tags,omitempty"`
	CaseLanguage   string   `json:"caseLanguage,omitempty"`

	CreatedBySSOName string `json:"createdBySSOName,omitempty"`
	OwnerSSOName     string `json:"ownerSSOName,omitempty"`

	StartDate        string `json:"startDate,omitempty"`
	EndDate          string `json:"endDate,omitempty"`
	LastModifiedFrom string `json:"lastModifiedFrom,omitempty"`
	LastModifiedTo   string `json:"lastModifiedTo,omitempty"`

	IncludeClosed     *bool `json:"includeClosed,omitempty"`
	OnlyEscalated     *bool `json:"onlyEscalated,omitempty"`
	CustomerEscalated *bool `json:"customerEscalation,omitempty"`

	MaxResults int    `json:"maxResults,omitempty"`
	Offset     int    `json:"offset,omitempty"`
	SortField  string `json:"sortField,omitempty"`
	SortOrder  string `json:"sortOrder,omitempty"`
}

// HasCriteria reports whether the request narrows the search at all,
// rather than only saying how to page and sort it.
//
// The API rejects a criteria-less filter with
// `400 No valid external filter criteria provided`, which the document
// does not mention; this was found by running against the real service
// on 2026-10-25. Paging and sorting do not count as criteria there —
// a body of {"maxResults":1} is refused.
//
// Which fields the service counts is not documented and only `keyword`
// has been confirmed. So this is deliberately generous: everything that
// is not paging or sorting counts, and a request that passes here can
// still be refused upstream. The point is to catch the empty search,
// not to predict the service's exact rule.
func (f *FilterCasesRequest) HasCriteria() bool {
	switch {
	case f.Keyword != "", f.Status != "", f.Severity != "", f.Product != "",
		f.Version != "", f.Type != "":
		return true
	case len(f.Statuses) > 0, len(f.Severities) > 0, len(f.Products) > 0:
		return true
	case len(f.CaseNumbers) > 0, len(f.AccountNumbers) > 0, len(f.GroupNumbers) > 0:
		return true
	case f.SBRGroups != "", f.Tags != "", f.CaseLanguage != "":
		return true
	case f.CreatedBySSOName != "", f.OwnerSSOName != "":
		return true
	case f.StartDate != "", f.EndDate != "", f.LastModifiedFrom != "", f.LastModifiedTo != "":
		return true
	case f.IncludeClosed != nil, f.OnlyEscalated != nil, f.CustomerEscalated != nil:
		return true
	}
	return false
}

// FilterCasesResponse mirrors FilterCasesV3Response.
type FilterCasesResponse struct {
	TotalCount int    `json:"totalCount"`
	Cases      []Case `json:"cases"`
}

// CreateCaseRequest mirrors the part of CreateCaseV3Request this server
// sends. The document marks summary, description, product and version
// required; everything else is optional and omitted when unset.
type CreateCaseRequest struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Product     string `json:"product"`
	Version     string `json:"version"`

	Severity         string `json:"severity,omitempty"`
	Status           string `json:"status,omitempty"`
	CaseType         string `json:"caseType,omitempty"`
	AccountNumberRef string `json:"accountNumberRef,omitempty"`
	ContactSSOName   string `json:"contactSSOName,omitempty"`
	GroupNumber      string `json:"groupNumber,omitempty"`
	CaseLanguage     string `json:"caseLanguage,omitempty"`

	OpenshiftClusterID      string `json:"openshiftClusterID,omitempty"`
	OpenshiftClusterVersion string `json:"openshiftClusterVersion,omitempty"`
	Hostname                string `json:"hostname,omitempty"`
	Environment             string `json:"environment,omitempty"`
	Issue                   string `json:"issue,omitempty"`
	Notes                   string `json:"notes,omitempty"`
	ActionPlan              string `json:"actionPlan,omitempty"`
	PeriodicityOfIssue      string `json:"periodicityOfIssue,omitempty"`
	TimeFramesAndUrgency    string `json:"timeFramesAndUrgency,omitempty"`

	CustomerEscalation *bool `json:"customerEscalation,omitempty"`

	// Origin identifies the channel the case came from. It is always set
	// by this server, so a case opened by an agent is distinguishable from
	// one a person filed in the console.
	Origin string `json:"origin,omitempty"`
}

// UpdateCaseRequest mirrors UpdateCaseV3Request.
//
// Every field is a pointer: the endpoint updates what it is sent and
// leaves out what it is not, so "" has to mean "clear this" and nil has
// to mean "do not touch it".
type UpdateCaseRequest struct {
	Summary     *string `json:"summary,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	Severity    *string `json:"severity,omitempty"`
	Product     *string `json:"product,omitempty"`
	Version     *string `json:"version,omitempty"`
	CaseType    *string `json:"caseType,omitempty"`

	AccountNumberRef *string `json:"accountNumberRef,omitempty"`
	ContactSSOName   *string `json:"contactSSOName,omitempty"`
	GroupNumber      *string `json:"groupNumber,omitempty"`
	CaseLanguage     *string `json:"caseLanguage,omitempty"`

	OpenshiftClusterID      *string `json:"openshiftClusterID,omitempty"`
	OpenshiftClusterVersion *string `json:"openshiftClusterVersion,omitempty"`
	Hostname                *string `json:"hostname,omitempty"`
	Environment             *string `json:"environment,omitempty"`
	Issue                   *string `json:"issue,omitempty"`
	Notes                   *string `json:"notes,omitempty"`
	PeriodicityOfIssue      *string `json:"periodicityOfIssue,omitempty"`
	TimeFramesAndUrgency    *string `json:"timeFramesAndUrgency,omitempty"`
	ReopenedReason          *string `json:"reopenedReason,omitempty"`

	CustomerEscalation *bool `json:"customerEscalation,omitempty"`
}

// IsEmpty reports whether the request would change nothing.
func (u *UpdateCaseRequest) IsEmpty() bool {
	return u.Summary == nil && u.Description == nil && u.Status == nil &&
		u.Severity == nil && u.Product == nil && u.Version == nil &&
		u.CaseType == nil && u.AccountNumberRef == nil && u.ContactSSOName == nil &&
		u.GroupNumber == nil && u.CaseLanguage == nil &&
		u.OpenshiftClusterID == nil && u.OpenshiftClusterVersion == nil &&
		u.Hostname == nil && u.Environment == nil && u.Issue == nil &&
		u.Notes == nil && u.PeriodicityOfIssue == nil &&
		u.TimeFramesAndUrgency == nil && u.ReopenedReason == nil &&
		u.CustomerEscalation == nil
}

// UpdateCaseResponse mirrors UpdateCaseV3Response.
//
// FieldsNotSentToSalesforce is kept as a free map rather than a struct:
// the document describes it as "fields accepted in the payload but not
// sent to the backend, returned for visibility", which is a list that
// grows server-side. Decoding it loosely means a newly added entry still
// reaches the caller instead of being silently dropped.
type UpdateCaseResponse struct {
	Message                   string         `json:"message,omitempty"`
	FieldsNotSentToSalesforce map[string]any `json:"fieldsNotSentToSalesforce,omitempty"`
}

// CreateCommentRequest mirrors CaseCommentRequest. commentBody and
// contentType are the document's required fields.
type CreateCommentRequest struct {
	CommentBody       string `json:"commentBody"`
	ContentType       string `json:"contentType"`
	CaseNumber        string `json:"caseNumber,omitempty"`
	CreatedByType     string `json:"createdByType,omitempty"`
	IsDraft           *bool  `json:"isDraft,omitempty"`
	DoNotChangeStatus *bool  `json:"doNotChangeStatus,omitempty"`
}

// SolrSearchRequest mirrors the KCS search body.
//
// The document declares `required: ["q", "exp"]` but defines no `exp`
// property — only `expression`. Running against the live API on
// 2026-10-25 settled it: `exp` is not required (a body of {q, rows} is
// answered 200), and sending `exp` is actively harmful (it returns 500).
// So this struct has no `exp` field and must not grow one.
type SolrSearchRequest struct {
	Q          string `json:"q"`
	Expression string `json:"expression,omitempty"`
	Start      int64  `json:"start,omitempty"`
	Rows       int64  `json:"rows,omitempty"`
	ClientName string `json:"clientName,omitempty"`
}

// kcsSnippetRunes bounds the excerpt kept from a document.
//
// The measurement that set this: in a live response, one document's
// `abstract` was 9401 of its 10076 bytes — 93% of the record, and at the
// API's default of ten rows, a 39 KB tool result. A few hundred
// characters is enough to judge whether an article is worth opening,
// which is all a search result has to do.
//
// It counts runes, not bytes, so a Japanese abstract is cut at the same
// number of characters as an English one rather than at a third as many.
const kcsSnippetRunes = 300

// KCSSnippetRunes exposes the excerpt limit so a tool can state it in
// its output rather than repeating the number.
func KCSSnippetRunes() int { return kcsSnippetRunes }

// KCSDocument is the projection of one search hit.
//
// The live response carries 26 fields per document, most of them
// internal: four near-identical titles, three URI forms, and a set of
// vector-index flags. What survives here is what identifies an article
// and helps decide whether to read it.
type KCSDocument struct {
	ID               string `json:"id,omitempty"`
	Title            string `json:"title,omitempty"`
	DocumentKind     string `json:"documentKind,omitempty"`
	Language         string `json:"language,omitempty"`
	LastModifiedDate string `json:"lastModifiedDate,omitempty"`

	// ViewURI is the API's `view_uri` passed through unchanged. Whether
	// it is absolute, or openable without a session, is not something
	// this server has established.
	ViewURI string `json:"viewUri,omitempty"`

	Snippet string `json:"snippet,omitempty"`
	// SnippetTruncated says the excerpt was cut, so a reader does not
	// mistake a clipped abstract for a short one.
	SnippetTruncated bool `json:"snippetTruncated,omitempty"`
}

// UnrecognizedPayload describes a response that was not the Solr
// envelope this code knows how to project.
//
// It carries no part of the body. The published document does not model
// this endpoint's response at all ("200: Solr Response"), so a change in
// shape is foreseeable rather than exceptional; when it happens the
// useful thing is to say so precisely and let a human look, not to pour
// an unknown payload into a model's context.
type UnrecognizedPayload struct {
	TopLevelKeys []string `json:"topLevelKeys"`
	SizeBytes    int      `json:"sizeBytes"`
	Note         string   `json:"note"`
}

// KCSResult is a parsed KCS search response.
//
// Exactly one of Documents or Unrecognized is meaningful: when the
// payload did not match the known envelope, Unrecognized is set and no
// projection was attempted.
type KCSResult struct {
	NumFound int `json:"numFound"`
	// NumFoundExact is the API's own flag for whether NumFound is exact
	// or an estimate. It is a pointer because an absent flag and a false
	// one mean different things: absent is "the API did not say".
	NumFoundExact *bool                `json:"numFoundExact,omitempty"`
	Start         int                  `json:"start"`
	Documents     []KCSDocument        `json:"documents"`
	Unrecognized  *UnrecognizedPayload `json:"unrecognizedShape,omitempty"`
}

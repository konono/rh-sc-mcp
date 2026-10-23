package server

import (
	"context"
	"fmt"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
)

// commentContentTypes are the values the document gives as examples for
// CaseCommentRequest.contentType.
//
// Note the asymmetry with responses: a request carries "plaintext" or
// "html", while CommentDto describes its contentType as "text/plain or
// text/html". The two are not the same vocabulary, so a value read back
// from a comment is not a value to send.
var commentContentTypes = []string{"plaintext", "html"}

// commentCreatedByTypes are the values the document lists for
// createdByType.
var commentCreatedByTypes = []string{"Customer", "Associate", "Internal"}

type listCommentsArgs struct {
	CaseNumber string `json:"caseNumber" jsonschema:"Case number as a string, e.g. 04337263"`
	StartDate  string `json:"startDate,omitempty" jsonschema:"Only comments created on or after this date, YYYY-MM-DD"`
	EndDate    string `json:"endDate,omitempty" jsonschema:"Only comments created on or before this date, YYYY-MM-DD"`
	SortField  string `json:"sortField,omitempty" jsonschema:"Sort by createdDate, lastModifiedDate or createdBy"`
	SortOrder  string `json:"sortOrder,omitempty" jsonschema:"asc or desc; the API defaults to desc"`
}

type listCommentsResult struct {
	CaseNumber string           `json:"caseNumber"`
	CaseURL    string           `json:"caseUrl,omitempty"`
	Count      int              `json:"count"`
	Comments   []redhat.Comment `json:"comments"`
	Notes      []string         `json:"notes,omitempty"`
}

type getCommentArgs struct {
	CaseNumber string `json:"caseNumber" jsonschema:"Case number as a string, e.g. 04337263"`
	CommentID  string `json:"commentId" jsonschema:"Comment record id, as returned by rhcase_list_comments"`
}

type addCommentArgs struct {
	CaseNumber        string `json:"caseNumber" jsonschema:"Case number as a string, e.g. 04337263"`
	CommentBody       string `json:"commentBody" jsonschema:"The comment text; required"`
	ContentType       string `json:"contentType,omitempty" jsonschema:"plaintext or html; defaults to plaintext"`
	CreatedByType     string `json:"createdByType,omitempty" jsonschema:"Customer, Associate or Internal"`
	IsDraft           *bool  `json:"isDraft,omitempty" jsonschema:"Save as a draft instead of publishing it"`
	DoNotChangeStatus *bool  `json:"doNotChangeStatus,omitempty" jsonschema:"Keep the case status unchanged; by default a comment can move the case to Waiting on Red Hat"`
}

type addCommentResult struct {
	CaseNumber string          `json:"caseNumber"`
	CaseURL    string          `json:"caseUrl,omitempty"`
	Comment    *redhat.Comment `json:"comment,omitempty"`
	Location   []string        `json:"location,omitempty"`
	HTTPStatus int             `json:"httpStatus"`
	Notes      []string        `json:"notes,omitempty"`
}

func (s *Server) registerComment() {
	add(s, toolDef[listCommentsArgs]{
		name:         "rhcase_list_comments",
		title:        "List case comments",
		desc:         "List the comments on a Red Hat support case, newest first unless sorted otherwise. Use this instead of rhcase_get when only the conversation is needed.",
		resource:     config.ResComment,
		need:         config.LevelRead,
		readOnlyHint: true,
		handler:      s.handleListComments,
	})

	add(s, toolDef[getCommentArgs]{
		name:         "rhcase_get_comment",
		title:        "Get one case comment",
		desc:         "Fetch a single comment on a Red Hat support case by its record id.",
		resource:     config.ResComment,
		need:         config.LevelRead,
		readOnlyHint: true,
		handler:      s.handleGetComment,
	})

	add(s, toolDef[addCommentArgs]{
		name:  "rhcase_add_comment",
		title: "Comment on a support case",
		desc: "Post a comment to a Red Hat support case. The comment is visible to Red Hat support and to everyone on the account, and it cannot be edited or deleted afterwards — " +
			"show the user the exact text and get their agreement before calling. Posting a comment can also change the case status unless doNotChangeStatus is true.",
		resource: config.ResComment,
		need:     config.LevelWrite,
		handler:  s.handleAddComment,
	})
}

func (s *Server) handleListComments(ctx context.Context, in listCommentsArgs) (any, error) {
	num, err := redhat.ValidateCaseNumber(in.CaseNumber)
	if err != nil {
		return nil, err
	}
	q := redhat.CommentQuery{}
	if q.StartDate, err = checkDate("startDate", in.StartDate); err != nil {
		return nil, err
	}
	if q.EndDate, err = checkDate("endDate", in.EndDate); err != nil {
		return nil, err
	}
	if q.SortField, err = checkCommentSortField(in.SortField); err != nil {
		return nil, err
	}
	if q.SortOrder, err = normalizeCommentSortOrder(in.SortOrder); err != nil {
		return nil, err
	}

	comments, err := s.api.ListComments(ctx, num, q)
	if err != nil {
		return nil, err
	}

	out := listCommentsResult{CaseNumber: num, CaseURL: s.cfg.CaseURL(num), Comments: comments}
	// The endpoint has no count parameter, so a long case returns its
	// whole history. Trim on the way out and say which end was kept,
	// since the default order is newest first and "the first 50" means
	// something different depending on the sort.
	if len(out.Comments) > s.cfg.MaxComments {
		total := len(out.Comments)
		out.Comments = out.Comments[:s.cfg.MaxComments]
		order := "newest first"
		if q.SortOrder == "asc" {
			order = "oldest first"
		}
		out.Notes = append(out.Notes, fmt.Sprintf(
			"the case has %d comments; the first %d in the returned order (%s) are shown. Narrow with startDate/endDate to see the others",
			total, s.cfg.MaxComments, order))
	}
	out.Count = len(out.Comments)
	return out, nil
}

func (s *Server) handleGetComment(ctx context.Context, in getCommentArgs) (any, error) {
	num, err := redhat.ValidateCaseNumber(in.CaseNumber)
	if err != nil {
		return nil, err
	}
	id, err := redhat.ValidateCommentID(in.CommentID)
	if err != nil {
		return nil, err
	}
	return s.api.GetComment(ctx, num, id)
}

func (s *Server) handleAddComment(ctx context.Context, in addCommentArgs) (any, error) {
	num, err := redhat.ValidateCaseNumber(in.CaseNumber)
	if err != nil {
		return nil, err
	}
	body, err := requireNonEmpty("commentBody", in.CommentBody)
	if err != nil {
		return nil, err
	}
	ct, err := checkEnum("contentType", in.ContentType, commentContentTypes)
	if err != nil {
		return nil, err
	}
	if ct == "" {
		// contentType is required by the document but has an obvious
		// default, and making the model supply it buys nothing.
		ct = "plaintext"
	}
	cbt, err := checkEnum("createdByType", in.CreatedByType, commentCreatedByTypes)
	if err != nil {
		return nil, err
	}

	res, err := s.api.AddComment(ctx, num, &redhat.CreateCommentRequest{
		CommentBody:       body,
		ContentType:       ct,
		CreatedByType:     cbt,
		IsDraft:           in.IsDraft,
		DoNotChangeStatus: in.DoNotChangeStatus,
	})
	if err != nil {
		return nil, err
	}

	out := addCommentResult{
		CaseNumber: num,
		CaseURL:    s.cfg.CaseURL(num),
		Comment:    res.Comment,
		Location:   res.Location,
		HTTPStatus: res.Status,
	}
	if res.Comment == nil {
		// Reporting "posted" off a bare status code would be a guess in
		// the one direction that matters: a comment cannot be unsent, so
		// a wrong "it failed" leads to a duplicate.
		out.Notes = append(out.Notes, fmt.Sprintf(
			"the API accepted the comment (HTTP %d) but returned no comment body; "+
				"confirm with rhcase_list_comments before posting again", res.Status))
	}
	if in.IsDraft != nil && *in.IsDraft {
		out.Notes = append(out.Notes, "saved as a draft, so Red Hat support cannot see it yet")
	}
	return out, nil
}

package server

import (
	"context"
	"fmt"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
)

// kcsMaxRows bounds a single knowledge-base page. The endpoint is a Solr
// front end and will return as many documents as it is asked for.
const kcsMaxRows = 50

type kcsSearchArgs struct {
	Query      string `json:"query" jsonschema:"Search query, passed to Solr as q; required"`
	Expression string `json:"expression,omitempty" jsonschema:"Raw Solr expression for sorting, filtering and field selection, as a URL-encoded query string"`
	Rows       int64  `json:"rows,omitempty" jsonschema:"Documents to return, up to 50"`
	Start      int64  `json:"start,omitempty" jsonschema:"Documents to skip, for paging"`
}

type kcsSearchResult struct {
	Result any      `json:"result"`
	Notes  []string `json:"notes,omitempty"`
}

// registerKCS adds the knowledge-base search tool.
//
// It is off unless the operator grants the kcs resource (see
// config.defaultLevels). Two reasons beyond scope hygiene: the endpoint
// is a different API surface from case management, and — unlike the case
// endpoints — the published document does not model its response, so
// what comes back cannot be validated against anything.
func (s *Server) registerKCS() {
	add(s, toolDef[kcsSearchArgs]{
		name:  "rhkcs_search",
		title: "Search the Red Hat knowledge base",
		desc: "Search Red Hat Knowledge Centered Service solutions and articles. " +
			"The response is passed through as the search backend returned it; its shape is not described by the published API document, so read it defensively.",
		resource:     config.ResKCS,
		need:         config.LevelRead,
		readOnlyHint: true,
		handler:      s.handleKCSSearch,
	})
}

func (s *Server) handleKCSSearch(ctx context.Context, in kcsSearchArgs) (any, error) {
	q, err := requireNonEmpty("query", in.Query)
	if err != nil {
		return nil, err
	}
	if in.Rows < 0 || in.Start < 0 {
		return nil, fmt.Errorf("rows and start must not be negative")
	}
	rows := in.Rows
	if rows == 0 {
		rows = 10
	}
	out := kcsSearchResult{}
	if rows > kcsMaxRows {
		out.Notes = append(out.Notes, fmt.Sprintf("rows reduced from %d to %d", rows, kcsMaxRows))
		rows = kcsMaxRows
	}

	res, err := s.api.SearchKCS(ctx, &redhat.SolrSearchRequest{
		Q:          q,
		Expression: in.Expression,
		Rows:       rows,
		Start:      in.Start,
		ClientName: "rh-sc-mcp",
	})
	if err != nil {
		return nil, err
	}
	out.Result = res
	return out, nil
}

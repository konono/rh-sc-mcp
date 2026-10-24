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
	NumFound      int                  `json:"numFound"`
	NumFoundExact *bool                `json:"numFoundExact,omitempty"`
	Start         int                  `json:"start"`
	Returned      int                  `json:"returned"`
	Documents     []redhat.KCSDocument `json:"documents"`
	// Unrecognized replaces Documents when the payload was not the
	// envelope this server knows how to project.
	Unrecognized *redhat.UnrecognizedPayload `json:"unrecognizedShape,omitempty"`
	Notes        []string                    `json:"notes,omitempty"`
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
			"Each hit is abbreviated to its id, title, kind, language, last-modified date, the viewUri the API supplied, and a short excerpt of the abstract. " +
			"The backend's response shape is not described by the published API document; if it arrives in a form this server does not recognise, the result says so instead of guessing.",
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

	if res.Unrecognized != nil {
		out.Unrecognized = res.Unrecognized
		out.Documents = []redhat.KCSDocument{}
		out.Notes = append(out.Notes,
			"the search backend answered in a shape this server does not recognise, so nothing was extracted. "+
				"The response body is deliberately not included. This endpoint's response is not described by the "+
				"published API document, so its shape can change without notice.")
		return out, nil
	}

	out.NumFound = res.NumFound
	out.NumFoundExact = res.NumFoundExact
	out.Start = res.Start
	out.Documents = res.Documents
	out.Returned = len(res.Documents)

	var truncated int
	for _, d := range res.Documents {
		if d.SnippetTruncated {
			truncated++
		}
	}
	if truncated > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"%d of %d snippets were cut to %d characters; viewUri is the API-provided location for each document",
			truncated, len(res.Documents), redhat.KCSSnippetRunes()))
	}
	if res.NumFoundExact != nil && !*res.NumFoundExact {
		out.Notes = append(out.Notes, "the backend reported numFound as an estimate, not an exact count")
	}
	// Phrased as what numFound says rather than as a fact about the
	// corpus: the backend may report numFound as an estimate, and it
	// does not always say whether it did.
	if res.NumFound > res.Start+len(res.Documents) {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"numFound (%d) exceeds this page; try start=%d for more",
			res.NumFound, res.Start+len(res.Documents)))
	}
	out.Notes = append(out.Notes,
		"Results are abbreviated to identity, kind and a short excerpt; viewUri is the value the API returned.")
	return out, nil
}

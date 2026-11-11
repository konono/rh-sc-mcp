package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/redhat"
)

// lifecycleMaxProductLen bounds the product argument. The longest product
// names in the API are well under this; anything longer is not a name.
const lifecycleMaxProductLen = 200

type lifecycleArgs struct {
	Product string `json:"product" jsonschema:"Exact product name as the Product Life Cycle API spells it, e.g. Red Hat Ansible Automation Platform; partial names match nothing"`
	Version string `json:"version,omitempty" jsonschema:"Only return this version, matched exactly, e.g. 2.7"`
}

type lifecycleVersionOut struct {
	redhat.LifecycleVersion
	// CurrentPhase is computed from the phase dates and AsOf. It is
	// absent when no dated phase contains that day; CurrentPhaseNote
	// then says why.
	CurrentPhase     string `json:"currentPhase,omitempty"`
	CurrentPhaseNote string `json:"currentPhaseNote,omitempty"`
}

type lifecycleProductOut struct {
	Name        string                `json:"name"`
	FormerNames []string              `json:"formerNames,omitempty"`
	Link        string                `json:"link,omitempty"`
	Policies    string                `json:"policies,omitempty"`
	IsRetired   bool                  `json:"isRetired"`
	Footnote    string                `json:"footnote,omitempty"`
	Versions    []lifecycleVersionOut `json:"versions"`
}

type lifecycleResult struct {
	// AsOf is the UTC date currentPhase was computed for.
	AsOf          string                      `json:"asOf"`
	RegeneratedAt string                      `json:"regeneratedAt,omitempty"`
	Products      []lifecycleProductOut       `json:"products"`
	Unrecognized  *redhat.UnrecognizedPayload `json:"unrecognizedShape,omitempty"`
	Notes         []string                    `json:"notes,omitempty"`
}

// registerLifecycle adds the product life cycle tool.
//
// It is off unless the operator grants the lifecycle resource, for the
// same reason as KCS: it is not case management. It needs no Red Hat
// credential and sends none (see redhat.LifecycleClient).
func (s *Server) registerLifecycle() {
	add(s, toolDef[lifecycleArgs]{
		name:  "rhlifecycle_get",
		title: "Get Red Hat product life cycle dates",
		desc: "Return the support phases (General availability, Full support, Maintenance, EUS, ELS and so on) and their dates for each version of a Red Hat product, from the public Product Life Cycle API. " +
			"product must be the exact product name (e.g. \"Red Hat Ansible Automation Platform\", \"Red Hat OpenShift Container Platform\"); partial names return nothing. " +
			"currentPhase is computed from the dates; reportedType is the API's own label, which can lag behind them.",
		resource:     config.ResLifecycle,
		need:         config.LevelRead,
		readOnlyHint: true,
		handler:      s.handleLifecycle,
	})
}

func (s *Server) handleLifecycle(ctx context.Context, in lifecycleArgs) (any, error) {
	product, err := requireNonEmpty("product", in.Product)
	if err != nil {
		return nil, err
	}
	if len(product) > lifecycleMaxProductLen {
		return nil, fmt.Errorf("product is longer than %d characters; pass the exact product name", lifecycleMaxProductLen)
	}
	version := strings.TrimSpace(in.Version)

	res, err := s.lifecycle.GetProducts(ctx, product)
	if err != nil {
		return nil, err
	}

	now := s.now()
	out := lifecycleResult{
		AsOf:          now.UTC().Format(time.DateOnly),
		RegeneratedAt: res.RegeneratedAt,
		Products:      []lifecycleProductOut{},
	}
	if res.Unrecognized != nil {
		out.Unrecognized = res.Unrecognized
		out.Notes = append(out.Notes,
			"the Product Life Cycle API answered in a shape this server does not recognise, so nothing was extracted. "+
				"The response body is deliberately not included. The API has no published specification, so its shape can change without notice.")
		return out, nil
	}
	if len(res.Products) == 0 {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"no product is named exactly %q. The API matches the full product name only (\"Ansible\" finds nothing; "+
				"\"Red Hat Ansible Automation Platform\" does). Try the full name as Red Hat writes it.", clipArg(product)))
		return out, nil
	}

	var available []string
	matched := false
	for _, p := range res.Products {
		po := lifecycleProductOut{
			Name: p.Name, FormerNames: p.FormerNames, Link: p.Link, Policies: p.Policies,
			IsRetired: p.IsRetired, Footnote: p.Footnote,
			Versions: []lifecycleVersionOut{},
		}
		for _, v := range p.Versions {
			available = append(available, v.Name)
			if version != "" && v.Name != version {
				continue
			}
			matched = true
			st := v.CurrentPhase(now)
			po.Versions = append(po.Versions, lifecycleVersionOut{
				LifecycleVersion: v,
				CurrentPhase:     st.Phase,
				CurrentPhaseNote: st.Note,
			})
		}
		out.Products = append(out.Products, po)
	}
	if version != "" && !matched {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"no version is named exactly %q; versions listed by the API: %s",
			clipArg(version), strings.Join(available, ", ")))
	}
	out.Notes = append(out.Notes,
		"Dates are calendar dates in UTC; an end date is the last day of its phase. "+
			"A boundary the API gives as text (usually \"N/A\") is in startText/endText and is not a date. "+
			"currentPhase is computed from the dates as of asOf; reportedType is the API's label and may be out of date.")
	return out, nil
}

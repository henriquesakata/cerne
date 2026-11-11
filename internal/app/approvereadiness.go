package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/henriquesakata/cerne/internal/clarify"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// Approve impediment codes (F-0308).
const (
	ImpedimentSpecMissing       = "spec-missing"
	ImpedimentBarMissing        = "bar-missing"
	ImpedimentBarMalformed      = "bar-malformed"
	ImpedimentOpenClarification = "open-clarification"
	// The card impediments (F-0336): soft, like an open clarification, but
	// the agent's to fix — one command each — not a question for the human.
	ImpedimentUntagged          = "untagged"
	ImpedimentCapabilityMissing = "capability-missing"
)

// ApproveImpediment is one reason a candidate cannot be approved yet. Hard
// ones are refused by the shared transition core on every surface; the others
// only by the CLI, which --force crosses.
type ApproveImpediment struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hard    bool   `json:"hard"`
}

// ApproveReadiness returns every impediment to approving candidate id — or
// re-approving an approved one whose approval went stale (F-0318): the
// document gate, then the bar gate (both hard: the shared core refuses them on
// every surface), then one per open clarification, then the card's own (all
// soft: only the CLI refuses them, before the core). It composes the same
// predicates the gates and the lint run — domain.CheckGate over FeatureDocSet,
// doccheck.SpecBar, clarify.Open, lint.Untagged, lint.HasCapabilityTaxonomy —
// so an empty hard list is exactly an approve the core accepts. A missing feature, one
// past approval or an approved one with nothing to renew is an error, the
// approve's own refusal.
func ApproveReadiness(repoDir, id string) ([]ApproveImpediment, error) {
	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("feature %s not found in backend", id)
	}
	if err != nil {
		return nil, err
	}
	if _, err := domain.Transition(f.State, domain.ActionApprove); err != nil {
		return nil, err
	}
	if err := checkReapprove(repoDir, f); err != nil {
		return nil, err
	}
	docs, err := FeatureDocSet(repoDir, id)
	if err != nil {
		return nil, err
	}
	spec, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var out []ApproveImpediment
	if domain.CheckGate(id, domain.ActionApprove, docs) != nil {
		out = append(out, ApproveImpediment{ImpedimentSpecMissing, "the spec is missing or still the template stub", true})
	} else if _, err := doccheck.SpecBar(spec); err != nil {
		code := ImpedimentBarMissing
		if errors.Is(err, doccheck.ErrBarMalformed) {
			code = ImpedimentBarMalformed
		}
		out = append(out, ApproveImpediment{code, err.Error(), true})
	}
	for _, c := range clarify.Open(string(spec)) {
		q := c.Question
		if q == "" {
			q = c.Raw
		}
		out = append(out, ApproveImpediment{ImpedimentOpenClarification, q, false})
	}
	card, err := cardImpediments(repoDir, f)
	if err != nil {
		return nil, err
	}
	return append(out, card...), nil
}

// cardImpediments are the soft impediments of the card itself (F-0336): no
// tag on either axis, and no capability linked while the project curates a
// taxonomy. They ask the same predicates the untagged-live and
// capability-orphan lint rules ask, so the check and the lint cannot drift.
func cardImpediments(repoDir string, f store.Feature) ([]ApproveImpediment, error) {
	var out []ApproveImpediment
	if lint.Untagged(f.Facets, f.Components) {
		out = append(out, ApproveImpediment{ImpedimentUntagged,
			fmt.Sprintf("no tag (facet/component) — classify it with 'cerne feature retag %s --facet <f> --component <c>'", f.ID), false})
	}
	if len(f.Capabilities) == 0 {
		caps, err := store.LoadAllCapabilities(repoDir)
		if err != nil {
			return nil, err
		}
		if lint.HasCapabilityTaxonomy(caps) {
			out = append(out, ApproveImpediment{ImpedimentCapabilityMissing,
				fmt.Sprintf("no capability linked — link it with 'cerne feature capability %s C-…' (the taxonomy: 'cerne capability list --tree')", f.ID), false})
		}
	}
	return out, nil
}

// AcceptedBar returns the valid bar line of feature id's spec — the one an
// approve accepts (F-0308) — or "" when the spec is missing or names none.
func AcceptedBar(repoDir, id string) (string, error) {
	spec, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	bar, _ := doccheck.SpecBar(spec)
	return bar, nil
}

// ErrIncompleteCard is the sentinel for an approve attempted over a card with
// no tag, or with no capability while the project curates a taxonomy
// (F-0336). Like ErrOpenClarifications it is enforced at the CLI and --force
// crosses it: the shared core and the UI approve do not refuse it, so a
// project without a taxonomy is never held to it.
var ErrIncompleteCard = errors.New("card missing tags or capability")

// CheckIncompleteCard refuses when feature id's card lacks what the board
// filters on, listing each impediment with the command that fixes it. An id
// the approve itself would refuse (missing feature, invalid state) passes,
// so the core names the refusal; any other load error fails closed. The id
// is canonicalized first, as the core accepts a lowercase one.
func CheckIncompleteCard(repoDir, id string) error {
	id = domain.CanonicalID(id)
	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking the card of %s: %w", id, err)
	}
	if _, err := domain.Transition(f.State, domain.ActionApprove); err != nil {
		return nil
	}
	imps, err := cardImpediments(repoDir, f)
	if err != nil {
		return fmt.Errorf("checking the card of %s: %w", id, err)
	}
	if len(imps) == 0 {
		return nil
	}
	var b strings.Builder
	for _, im := range imps {
		fmt.Fprintf(&b, "\n  - %s: %s", im.Code, im.Message)
	}
	fmt.Fprintf(&b, "\nthen run 'cerne feature approve %s' again.", id)
	b.WriteString("\n--force skips this check only: it approves the card as it is; the other approve gates still apply.")
	return fmt.Errorf("%w — %s is not registered complete:%s", ErrIncompleteCard, id, b.String())
}

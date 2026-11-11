package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/henriquesakata/cerne/internal/clarify"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// Approve impediment codes (F-0308).
const (
	ImpedimentSpecMissing       = "spec-missing"
	ImpedimentBarMissing        = "bar-missing"
	ImpedimentBarMalformed      = "bar-malformed"
	ImpedimentOpenClarification = "open-clarification"
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
// every surface), then one per open clarification (soft: only the CLI refuses
// them, and it checks them first, before the core). It composes the same
// predicates the gates run —
// domain.CheckGate over FeatureDocSet, doccheck.SpecBar, clarify.Open — so an
// empty hard list is exactly an approve the core accepts. A missing feature, one
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

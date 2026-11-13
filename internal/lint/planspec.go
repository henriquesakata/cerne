package lint

import (
	"os"
	"path/filepath"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// PlanBehindSpec reports whether feature f's plan was last written against a
// spec other than the live one (F-0377): the spec_digest of its last plan
// write against SpecDigest of spec.md — by content, like F-0318, so a change
// of wrapping only is not one. planAt is that write, specAt the last recorded
// spec write (zero when none is). A last plan write with no digest (before
// F-0377, or a stub) is never behind; nor is a feature without a spec.
//
// Exported because the lint rule, the start and NextStep derive this one fact.
func PlanBehindSpec(repoDir string, f store.Feature) (behind bool, planAt, specAt time.Time) {
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return false, time.Time{}, time.Time{}
	}
	return planBehindSpec(repoDir, f, events)
}

// planBehindSpec is PlanBehindSpec over an event log already read.
func planBehindSpec(repoDir string, f store.Feature, events []domain.Event) (behind bool, planAt, specAt time.Time) {
	var digest string
	for _, e := range events {
		if e.EntityID != f.ID || e.Type != "feature.doc_edited" {
			continue
		}
		switch e.Payload["doc"] {
		case "plan":
			if !e.At.Before(planAt) {
				planAt, digest = e.At, e.Payload["spec_digest"]
			}
		case "spec":
			if !e.At.Before(specAt) {
				specAt = e.At
			}
		}
	}
	if digest == "" {
		return false, planAt, specAt
	}
	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", f.ID, "spec.md"))
	if err != nil {
		return false, planAt, specAt
	}
	return SpecDigest(spec) != digest, planAt, specAt
}

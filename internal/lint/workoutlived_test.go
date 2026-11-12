package lint

import (
	"slices"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestWorkOutlivedFeatureFindings (F-0341 crit. 8): an active work whose refs
// are all features out of the live cycle is flagged, with the command that
// closes it; a work with a live, unknown or non-feature ref, with no refs, or
// already closed is not.
func TestWorkOutlivedFeatureFindings(t *testing.T) {
	features := []store.Feature{
		{ID: "F-0001", State: domain.StateImplemented},
		{ID: "F-0002", State: domain.StateDone},
		{ID: "F-0003", State: domain.StateRejected},
		{ID: "F-0004", State: domain.StateSuperseded},
		{ID: "F-0005", State: domain.StateInDevelopment},
	}
	work := func(id string, state domain.WorkState, refs ...string) store.Work {
		return store.Work{ID: id, State: state, Actor: "a@b", Refs: refs}
	}
	works := []store.Work{
		work("W-0001", domain.WorkActive, "F-0001"),
		work("W-0002", domain.WorkActive, "F-0003", "F-0004"),
		work("W-0003", domain.WorkActive, "F-0001", "F-0005"), // one still live
		work("W-0004", domain.WorkActive, "D-0001"),           // a decision
		work("W-0005", domain.WorkActive, "F-0099"),           // unknown feature
		work("W-0006", domain.WorkActive),                     // no refs
		work("W-0007", domain.WorkDone, "F-0002"),             // already closed
	}

	got := workOutlivedFeatureFindings(works, features)
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want W-0001 and W-0002", got)
	}
	want := map[string]string{
		"W-0001": `cerne work done W-0001 --note "feature implemented"`,
		"W-0002": `cerne work done W-0002 --note "feature rejected, superseded"`,
	}
	for _, f := range got {
		if f.Rule != RuleWorkOutlivedFeature || f.Fix != want[f.Entity] {
			t.Errorf("finding = %+v, want rule %s fix %q", f, RuleWorkOutlivedFeature, want[f.Entity])
		}
	}
}

// TestWorkOutlivedFeatureIsAdvisoryAndWired: the rule is a warning, sits in
// the report order and runs from RunConfig.
func TestWorkOutlivedFeatureIsAdvisoryAndWired(t *testing.T) {
	if !Advisory(RuleWorkOutlivedFeature) {
		t.Fatal("work-outlived-feature must be advisory")
	}
	if !slices.Contains(RuleOrder, RuleWorkOutlivedFeature) {
		t.Fatal("RuleWorkOutlivedFeature missing from RuleOrder")
	}
	if _, ok := ruleRank[RuleWorkOutlivedFeature]; !ok {
		t.Fatal("RuleWorkOutlivedFeature missing from ruleRank")
	}

	dir := t.TempDir()
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Feita", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveWork(dir, store.Work{ID: "W-0001", Title: "Planejamento", State: domain.WorkActive, Actor: "a@b", Refs: []string{"F-0001"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	got, err := RunConfig(dir, 14, 8)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	var n int
	for _, f := range got {
		if f.Rule == RuleWorkOutlivedFeature && f.Entity == "W-0001" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("work-outlived-feature findings for W-0001 = %d, want 1: %+v", n, got)
	}
}

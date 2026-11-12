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

	closed := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC) // after every work's (zero) CreatedAt
	events := []domain.Event{
		{EntityID: "F-0001", Type: "feature.completed", At: closed},
		{EntityID: "F-0002", Type: "feature.accepted", At: closed},
		{EntityID: "F-0003", Type: "feature.rejected", At: closed},
		{EntityID: "F-0004", Type: "feature.superseded", At: closed},
	}

	got := workOutlivedFeatureFindings(works, features, events)
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
	if err := store.AppendEvent(dir, domain.NewEvent("F-0001", "feature.completed", nil, "test")); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	opened := time.Now().UTC().Add(-time.Hour)
	if err := store.SaveWork(dir, store.Work{ID: "W-0001", Title: "Planejamento", State: domain.WorkActive, Actor: "a@b", Refs: []string{"F-0001"}, CreatedAt: opened, UpdatedAt: opened}); err != nil {
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

// TestWorkOutlivedFeatureOpenedAfterClose: a work opened after the feature
// reached its current closed state (an errata, the QA of an accept) is live
// work on a closed feature, not a leftover — only a work created before the
// last transition into that state is flagged, since its Fix closes it.
func TestWorkOutlivedFeatureOpenedAfterClose(t *testing.T) {
	dir := t.TempDir()
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Feita", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	completed := time.Now().UTC().Add(-time.Hour)
	ev := domain.NewEvent("F-0001", "feature.completed", nil, "test")
	ev.At = completed
	if err := store.AppendEvent(dir, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	for id, created := range map[string]time.Time{
		"W-0001": completed.Add(-time.Hour),  // planning left over: flagged
		"W-0002": completed.Add(time.Minute), // errata opened after the complete
	} {
		w := store.Work{ID: id, Title: "Trabalho " + id, State: domain.WorkActive, Actor: "a@b", Refs: []string{"F-0001"}, CreatedAt: created, UpdatedAt: created}
		if err := store.SaveWork(dir, w); err != nil {
			t.Fatalf("SaveWork: %v", err)
		}
	}
	got, err := RunConfig(dir, 14, 8)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	var flagged []string
	for _, f := range got {
		if f.Rule == RuleWorkOutlivedFeature {
			flagged = append(flagged, f.Entity)
		}
	}
	if !slices.Equal(flagged, []string{"W-0001"}) {
		t.Fatalf("work-outlived-feature = %v, want [W-0001]", flagged)
	}
}

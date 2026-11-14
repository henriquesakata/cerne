package app

import (
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0422: a manual work with several features closes when the last of them
// leaves the live cycle. Tests pin the spec's criteria by number.

// seedWork persists (and pushes) active work id of outro@host on refs.
func seedWork(t *testing.T, repoDir, id string, auto bool, refs ...string) {
	t.Helper()
	now := time.Now().UTC()
	w := store.Work{ID: id, Title: "Lote " + id, State: domain.WorkActive, Actor: "outro@host", Refs: refs, Auto: auto, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

// exitFrom is the live state a feature leaves by action in these tests, and
// the by/reason the transition needs.
func exitFrom(action domain.Action) (from domain.FeatureState, by, reason string) {
	switch action {
	case domain.ActionReject:
		return domain.StateCandidate, "", "fora de escopo"
	case domain.ActionSupersede:
		return domain.StateInDevelopment, "F-0003", "substituída"
	}
	return domain.StateInDevelopment, "", ""
}

// TestBatchWorkClosedOnLastExit (crit. 1-6): the transition that takes the
// last live feature of a batch work out of the cycle closes it in its own
// commit — done when a ref was delivered, abandoned when all were dropped —
// and nothing else: a live ref, a work opened after a ref closed, an auto
// work, a single-ref work, a non-feature ref or the opt-out leave it active.
func TestBatchWorkClosedOnLastExit(t *testing.T) {
	both := []string{"F-0001", "F-0002"}
	cases := []struct {
		name      string
		refs      []string
		auto      bool
		optOut    bool
		other     domain.Action // F-0002's exit, before F-0001's; "" = stays approved
		openAfter bool          // the work is created after F-0002 closes (crit. 4)
		action    domain.Action // F-0001's exit
		want      domain.WorkState
		note      string
	}{
		{"last exit is a complete", both, false, false, domain.ActionComplete, false, domain.ActionComplete, domain.WorkDone, "all refs closed (feature implemented)"},
		{"every ref rejected", both, false, false, domain.ActionReject, false, domain.ActionReject, domain.WorkAbandoned, "all refs closed (feature rejected)"},
		{"one delivery makes it done", both, false, false, domain.ActionComplete, false, domain.ActionSupersede, domain.WorkDone, "all refs closed (feature superseded, implemented)"},
		{"a ref still live", both, false, false, "", false, domain.ActionComplete, domain.WorkActive, ""},
		{"opened after a ref closed", both, false, false, domain.ActionComplete, true, domain.ActionComplete, domain.WorkActive, ""},
		// Rejects from candidate: leaving in_development would close it by F-0146.
		{"auto work", both, true, false, domain.ActionReject, false, domain.ActionReject, domain.WorkActive, ""},
		{"single ref opened in development", []string{"F-0001"}, false, false, "", false, domain.ActionComplete, domain.WorkActive, ""},
		{"a ref is not a feature", []string{"F-0001", "D-0001"}, false, false, "", false, domain.ActionComplete, domain.WorkActive, ""},
		{"opt-out", both, false, true, domain.ActionComplete, false, domain.ActionComplete, domain.WorkActive, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			if tc.optOut {
				optOutActivity(t, projDir)
			}
			from, by, reason := exitFrom(tc.action)
			seedFeatureInState(t, repoDir, "F-0001", from, "spec.md", "plan.md", "result.md")
			if from == domain.StateInDevelopment {
				// F-0001 really started: a single-ref work opened after it is
				// development work, which F-0341 leaves alone (crit. 5).
				seedStartEvent(t, repoDir, "F-0001", time.Now().UTC().Add(-time.Hour))
			}
			otherFrom, otherBy, otherReason := domain.StateApproved, "", ""
			if tc.other != "" {
				otherFrom, otherBy, otherReason = exitFrom(tc.other)
			}
			seedFeatureInState(t, repoDir, "F-0002", otherFrom, "spec.md", "plan.md", "result.md")
			seedFeatureInState(t, repoDir, "F-0003", domain.StateCandidate, "spec.md")

			if !tc.openAfter {
				seedWork(t, repoDir, "W-0001", tc.auto, tc.refs...)
			}
			if tc.other != "" {
				if _, err := TransitionFeature(repoDir, projDir, "F-0002", tc.other, otherBy, otherReason, nil); err != nil {
					t.Fatalf("F-0002 %s: %v", tc.other, err)
				}
			}
			if tc.openAfter {
				seedWork(t, repoDir, "W-0001", tc.auto, tc.refs...)
			}
			if tc.other != "" && !tc.auto {
				assertActive(t, repoDir, "W-0001") // F-0001 still live (crit. 3)
			}
			before, err := gitback.HeadSHA(repoDir)
			if err != nil {
				t.Fatalf("HeadSHA: %v", err)
			}

			if _, err := TransitionFeature(repoDir, projDir, "F-0001", tc.action, by, reason, nil); err != nil {
				t.Fatalf("F-0001 %s: %v", tc.action, err)
			}
			if tc.want == domain.WorkActive {
				assertActive(t, repoDir, "W-0001")
				return
			}
			assertClosedAuto(t, repoDir, "W-0001", tc.want, tc.note)
			assertOneCommit(t, repoDir, before, "entities/works/W-0001.json", "entities/features/F-0001.json")
		})
	}
}

// TestBatchWorkMatchesLint (crit. 7): the transition and work-outlived-feature
// ask one predicate — with the opt-out the transition leaves the batch work
// and the lint names it; without, the transition closes it and the lint is
// left with nothing.
func TestBatchWorkMatchesLint(t *testing.T) {
	for _, optOut := range []bool{true, false} {
		repoDir, projDir, _ := newFeatureBackend(t)
		if optOut {
			optOutActivity(t, projDir)
		}
		seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
		seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate, "spec.md")
		seedWork(t, repoDir, "W-0001", false, "F-0001", "F-0002")
		for _, id := range []string{"F-0002", "F-0001"} {
			if _, err := TransitionFeature(repoDir, projDir, id, domain.ActionReject, "", "fora de escopo", nil); err != nil {
				t.Fatalf("reject %s: %v", id, err)
			}
		}
		works := loadWorks(t, repoDir)
		features, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			t.Fatalf("LoadAllFeatures: %v", err)
		}
		events, err := store.ReadEvents(repoDir)
		if err != nil {
			t.Fatalf("ReadEvents: %v", err)
		}
		got := lint.OutlivedWorks(works, features, events)
		if optOut && (len(got) != 1 || got[0].Work.ID != "W-0001") {
			t.Fatalf("opt-out: OutlivedWorks = %+v, want W-0001", got)
		}
		if !optOut && len(got) != 0 {
			t.Fatalf("closed by the transition, OutlivedWorks = %+v, want none", got)
		}
	}
}

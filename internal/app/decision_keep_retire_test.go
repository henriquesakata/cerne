package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// lastEvent returns the newest event logged for id.
func lastEvent(t *testing.T, repoDir, id string) domain.Event {
	t.Helper()
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EntityID == id {
			return events[i]
		}
	}
	t.Fatalf("no event for %s", id)
	return domain.Event{}
}

// TestKeepDecisionStampsReview pins F-0385 criterion 4: keep records the
// review date on the decision and a decision.kept event with the actor; the
// decision stays active. A retired decision cannot be kept.
func TestKeepDecisionStampsReview(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d := mkTaggedDecision(t, repoDir, projDir, nil, nil)

	got, err := KeepDecision(repoDir, projDir, d.ID)
	if err != nil {
		t.Fatalf("KeepDecision: %v", err)
	}
	if got.ReviewedAt == nil || got.ReviewedAt.Before(d.CreatedAt) || got.State != domain.DecisionActive {
		t.Fatalf("kept = %+v, want active with ReviewedAt >= CreatedAt", got)
	}
	reload, _ := store.LoadDecision(repoDir, d.ID)
	if reload.ReviewedAt == nil || !reload.ReviewedAt.Equal(*got.ReviewedAt) {
		t.Fatalf("persisted ReviewedAt = %v, want %v", reload.ReviewedAt, got.ReviewedAt)
	}
	if ev := lastEvent(t, repoDir, d.ID); ev.Type != "decision.kept" || ev.Actor != EventActor() {
		t.Fatalf("event = %+v, want decision.kept by %s", ev, EventActor())
	}

	if _, err := RetireDecision(repoDir, projDir, d.ID, "no longer applies"); err != nil {
		t.Fatalf("RetireDecision: %v", err)
	}
	if _, err := KeepDecision(repoDir, projDir, d.ID); err == nil {
		t.Fatal("KeepDecision on a retired decision succeeded, want an error")
	}
}

// TestRetireDecision pins F-0385 criterion 5: retire moves the decision to
// retired with no successor and the reason, and decision.retired carries the
// reason and the state it left (the undo path of a rollback).
func TestRetireDecision(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d := mkTaggedDecision(t, repoDir, projDir, nil, nil)

	out, err := RetireDecision(repoDir, projDir, d.ID, "the subsystem is gone")
	if err != nil {
		t.Fatalf("RetireDecision: %v", err)
	}
	if out.From != domain.DecisionActive || out.To != domain.DecisionRetired {
		t.Fatalf("outcome = %+v, want active → retired", out)
	}
	reload, _ := store.LoadDecision(repoDir, d.ID)
	if reload.State != domain.DecisionRetired || reload.SupersededBy != "" || reload.Reason != "the subsystem is gone" {
		t.Fatalf("decision = %+v, want retired, no successor, the reason", reload)
	}
	ev := lastEvent(t, repoDir, d.ID)
	if ev.Type != "decision.retired" || ev.Payload["reason"] != "the subsystem is gone" || ev.Payload["from"] != "active" || ev.Actor != EventActor() {
		t.Fatalf("event = %+v, want decision.retired with reason, from and actor", ev)
	}
}

// TestRetireDecisionWithoutReasonWritesNothing pins F-0385 criterion 6: no
// reason is refused with the sentinel, and the backend HEAD does not move.
func TestRetireDecisionWithoutReasonWritesNothing(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	d := mkTaggedDecision(t, repoDir, projDir, nil, nil)
	before, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}

	if _, err := RetireDecision(repoDir, projDir, d.ID, "  "); !errors.Is(err, domain.ErrRetireReasonRequired) {
		t.Fatalf("err = %v, want ErrRetireReasonRequired", err)
	}
	after, _ := gitback.HeadSHA(origin)
	if after != before {
		t.Fatalf("origin HEAD moved %s → %s on a refused retire", before, after)
	}
	if reload, _ := store.LoadDecision(repoDir, d.ID); reload.State != domain.DecisionActive {
		t.Fatalf("state = %s, want still active", reload.State)
	}
}

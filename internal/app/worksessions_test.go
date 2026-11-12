package app

import (
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestWorkSessionsAttribution(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	works := []store.Work{
		{ID: "W-0001", State: domain.WorkActive},
		{ID: "W-0002", State: domain.WorkActive, Auto: true, Refs: []string{"F-0010"}},
		{ID: "W-0003", State: domain.WorkActive, Refs: []string{"F-0011"}}, // not Auto
		{ID: "W-0004", State: domain.WorkDone, Auto: true, Refs: []string{"F-0012"}},
	}
	signals := []SessionSignal{
		{Session: "s-old", Entity: "F-0010", LastSignal: t0.Add(-time.Hour)},
		{Session: "s-new", Entity: "W-0002", LastSignal: t0},
		{Session: "s-direct", Entity: "W-0001", LastSignal: t0},
		{Session: "s-noauto", Entity: "F-0011", LastSignal: t0},
		{Session: "s-done", Entity: "F-0012", LastSignal: t0},
		{Session: "s-gone", Entity: "W-0009", LastSignal: t0},
	}
	got := WorkSessions(works, signals)
	if l := got["W-0001"]; len(l) != 1 || l[0].Session != "s-direct" {
		t.Fatalf("W-0001: %+v", l)
	}
	if l := got["W-0002"]; len(l) != 2 || l[0].Session != "s-new" || l[1].Session != "s-old" {
		t.Fatalf("W-0002 want newest first [s-new s-old]: %+v", l)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected extra attributions: %+v", got)
	}
}

func TestActorHost(t *testing.T) {
	for in, want := range map[string]string{"agent:u@h": "h", "u@h": "h", "agent": "", "": ""} {
		if got := ActorHost(in); got != want {
			t.Errorf("ActorHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// A feature binding belongs to every active auto work referencing the feature.
func TestWorkSessionsFeatureBindingToEveryAutoWork(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	works := []store.Work{
		{ID: "W-0001", State: domain.WorkActive, Auto: true, Refs: []string{"F-0010"}},
		{ID: "W-0002", State: domain.WorkActive, Auto: true, Refs: []string{"F-0010"}},
	}
	got := WorkSessions(works, []SessionSignal{{Session: "s", Entity: "F-0010", LastSignal: t0}})
	if len(got["W-0001"]) != 1 || len(got["W-0002"]) != 1 {
		t.Fatalf("got %+v", got)
	}
}

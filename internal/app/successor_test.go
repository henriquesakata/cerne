package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestTerminalSuccessor(t *testing.T) {
	dir := t.TempDir()
	// Simple chain: D-0003 superseded by the active D-0002.
	mustSaveDecision(t, dir, "D-0003", domain.DecisionSuperseded, "D-0002")
	mustSaveDecision(t, dir, "D-0002", domain.DecisionActive, "")
	// Superseded with no successor (retire / historical mention).
	mustSaveFeature(t, dir, "F-0001", domain.StateSuperseded, "")
	// Multi-hop A->B->C: resolve to the terminal C, not the first hop.
	mustSaveFeature(t, dir, "F-0010", domain.StateSuperseded, "F-0011")
	mustSaveFeature(t, dir, "F-0011", domain.StateSuperseded, "F-0012")
	mustSaveFeature(t, dir, "F-0012", domain.StateImplemented, "")
	// Cycle A->B->A in corrupt data: terminate, non-resolvable, no infinite loop.
	mustSaveDecision(t, dir, "D-0020", domain.DecisionSuperseded, "D-0021")
	mustSaveDecision(t, dir, "D-0021", domain.DecisionSuperseded, "D-0020")

	tests := []struct {
		name   string
		id     string
		want   string
		wantOk bool
	}{
		{"simple chain", "D-0003", "D-0002", true},
		{"no successor (retire)", "F-0001", "", false},
		{"live id nobody superseded", "D-0002", "", false},
		{"unknown id", "D-9999", "", false},
		{"multi-hop to terminal", "F-0010", "F-0012", true},
		{"cycle terminates non-resolvable", "D-0020", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := TerminalSuccessor(dir, tt.id)
			if err != nil {
				t.Fatalf("TerminalSuccessor(%q): unexpected err %v", tt.id, err)
			}
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("TerminalSuccessor(%q) = (%q, %v), want (%q, %v)", tt.id, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func mustSaveDecision(t *testing.T, dir, id string, state domain.DecisionState, supersededBy string) {
	t.Helper()
	if err := store.SaveDecision(dir, store.Decision{ID: id, Title: id, State: state, SupersededBy: supersededBy}); err != nil {
		t.Fatalf("SaveDecision %s: %v", id, err)
	}
}

func mustSaveFeature(t *testing.T, dir, id string, state domain.FeatureState, supersededBy string) {
	t.Helper()
	if err := store.SaveFeature(dir, store.Feature{ID: id, Title: id, State: state, SupersededBy: supersededBy}); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
}

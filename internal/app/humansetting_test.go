package app

import (
	"errors"
	"testing"
)

// TestCheckHumanSetting pins F-0392 criterion 4's guard: an agent actor is
// refused on a human-only key, anything else passes.
func TestCheckHumanSetting(t *testing.T) {
	for _, tc := range []struct {
		name, actor, id string
		refused         bool
	}{
		{"agent on human-only", "agent", "gates.gauntlet", true},
		{"prefixed agent on human-only", "agent:someone", "gates.gauntlet", true},
		{"agent on ordinary key", "agent", "graph.enabled", false},
		{"no actor on human-only", "", "gates.gauntlet", false},
		{"human actor on human-only", "someone@host", "gates.gauntlet", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CERNE_ACTOR", tc.actor)
			err := CheckHumanSetting(tc.id)
			if tc.refused != errors.Is(err, ErrHumanGate) || (!tc.refused && err != nil) {
				t.Errorf("CheckHumanSetting(%s) as %q = %v, refused want %v", tc.id, tc.actor, err, tc.refused)
			}
		})
	}
}

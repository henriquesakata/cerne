package domain

import (
	"slices"
	"testing"
)

func TestValidStates(t *testing.T) {
	tests := []struct {
		kind   string
		ok     bool
		length int
		has    []string
	}{
		{"feature", true, 7, []string{"candidate", "in_development", "implemented", "done", "superseded"}},
		{"decision", true, 3, []string{"active", "superseded", "retired"}},
		{"reference", true, 2, []string{"current", "obsolete"}},
		{"runbook", true, 2, []string{"active", "superseded"}},
		{"note", true, 2, []string{"active", "superseded"}},
		{"bogus", false, 0, nil},
	}
	for _, tt := range tests {
		got, ok := ValidStates(tt.kind)
		if ok != tt.ok {
			t.Fatalf("ValidStates(%q) ok = %v, want %v", tt.kind, ok, tt.ok)
		}
		if len(got) != tt.length {
			t.Fatalf("ValidStates(%q) len = %d, want %d (got %v)", tt.kind, len(got), tt.length, got)
		}
		for _, s := range tt.has {
			if !slices.Contains(got, s) {
				t.Fatalf("ValidStates(%q) missing %q (got %v)", tt.kind, s, got)
			}
		}
	}
}

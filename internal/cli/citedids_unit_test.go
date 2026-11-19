package cli

import (
	"slices"
	"testing"
)

// F-0449: a Cerne: trailer names its ids on its own line, separated by commas
// or spaces; an empty one never borrows the next line's word.
func TestCitedIDs(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want []string
	}{
		{"fix: y\n\nCerne: F-0449, F-0450\n", []string{"F-0449", "F-0450"}},
		{"fix: y\n\nCerne: F-0449 F-0450\n", []string{"F-0449", "F-0450"}},
		{"fix: y\n\nCerne:\tF-0001\n", []string{"F-0001"}},
		{"fix: y\n\nCerne:\nF-0001\n", nil},
		{"fix: y\n\ncerne: F-0001\nRefs: x\n", []string{"F-0001"}},
	} {
		if got := citedIDs(tc.msg); !slices.Equal(got, tc.want) {
			t.Errorf("citedIDs(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

package lint

import (
	"reflect"
	"testing"
)

// TestLint_LightModeSkipsGauntletRules (F-0364 crit. 2, 3): in light mode the
// two Gauntlet rules are dropped and every other finding stays; with the
// Gauntlet on the findings pass through untouched.
func TestLint_LightModeSkipsGauntletRules(t *testing.T) {
	in := []Finding{
		{Rule: RuleSpecMissingBar, Entity: "F-0001"},
		{Rule: RuleResultMissingEvidence, Entity: "F-0002"},
		{Rule: RuleResultMissingGauntlet, Entity: "F-0002"},
		{Rule: RuleBrokenLink, Entity: "F-0003"},
	}
	if got := DropGauntletRules(append([]Finding(nil), in...), true); !reflect.DeepEqual(got, in) {
		t.Fatalf("gauntlet on: got %+v, want untouched", got)
	}
	got := DropGauntletRules(append([]Finding(nil), in...), false)
	want := []Finding{in[1], in[3]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("light mode: got %+v, want %+v", got, want)
	}
}

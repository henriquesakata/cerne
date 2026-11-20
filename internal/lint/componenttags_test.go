package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// E-0005 verification 10, the tag part (criterion 18): single-use components,
// same-root groups and note-only components each come as ONE advisory line
// with the count, the names and the commands that list and retag.
func TestComponentTagFindings(t *testing.T) {
	features := []store.Feature{
		{ID: "F-0001", State: domain.StateImplemented, Components: []string{"gitea", "testing"}},
		{ID: "F-0002", State: domain.StateCandidate, Components: []string{"gitea", "tests"}},
	}
	notes := []store.Note{
		{ID: "N-0001", State: domain.NoteActive, Components: []string{"gitea", "kpack"}},
		{ID: "N-0002", State: domain.NoteActive, Components: []string{"kpack", "skill"}},
		{ID: "N-0003", State: domain.NoteActive, Components: []string{"skills"}},
		{ID: "N-0004", State: domain.NoteSuperseded, Components: []string{"legacy", "gitea"}},
	}
	decisions := []store.Decision{{ID: "D-0001", State: domain.DecisionActive, Components: []string{"gitea"}}}

	got := componentTagFindings(componentUses(features, notes, decisions, nil, nil, nil))
	want := map[string]string{
		RuleComponentSingleUse: "5 components are carried by a single entity: legacy (N-0004), skill (N-0002), skills (N-0003), testing (F-0001), tests (F-0002)",
		RuleComponentSameRoot:  "2 groups of components share a root: skill/skills, testing/tests",
		RuleComponentNoteOnly:  "3 components are on active notes and on no feature: kpack, skill, skills",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want one per rule: %+v", len(got), got)
	}
	for _, f := range got {
		w, ok := want[f.Rule]
		if !ok {
			t.Fatalf("unexpected rule %q: %+v", f.Rule, f)
		}
		if !strings.HasPrefix(f.Detail, w) {
			t.Errorf("%s detail = %q, want prefix %q", f.Rule, f.Detail, w)
		}
		if !strings.Contains(f.Detail, "cerne tags") || !strings.Contains(f.Detail, "retag") || f.Fix != "cerne tags" {
			t.Errorf("%s does not carry the list and retag commands: %+v", f.Rule, f)
		}
		if Blocking(f) {
			t.Errorf("%s blocks the lint; it is informational", f.Rule)
		}
		if !slices.Contains(RuleOrder, f.Rule) {
			t.Errorf("%s missing from RuleOrder", f.Rule)
		}
	}
	if got := componentTagFindings(componentUses(features[:1], nil, nil, nil, nil, nil)); len(got) != 1 || got[0].Rule != RuleComponentSingleUse {
		t.Fatalf("a clean vocabulary reports only what applies: %+v", got)
	}
}

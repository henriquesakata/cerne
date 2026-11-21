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

	got := componentTagFindings(componentUses(features, notes, decisions, nil, nil, nil), notes)
	want := map[string]string{
		RuleComponentSingleUse: "5 components are carried by a single entity: legacy (N-0004: legacy,gitea), skill (N-0002: kpack,skill), skills (N-0003: skills), testing (F-0001: gitea,testing), tests (F-0002: gitea,tests)",
		RuleComponentSameRoot:  "2 groups of components share a root: skill/skills, testing/tests",
		RuleComponentNoteOnly:  "2 active notes share no component with any feature: N-0002 (kpack,skill), N-0003 (skills)",
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
		if f.Rule == RuleComponentSingleUse && !strings.Contains(f.Detail, "'cerne note retag N-0004 --component gitea,<existing>'") {
			t.Errorf("the single-use hint does not pass the carrier's whole list: %q", f.Detail)
		}
		if Blocking(f) {
			t.Errorf("%s blocks the lint; it is informational", f.Rule)
		}
		if !slices.Contains(RuleOrder, f.Rule) {
			t.Errorf("%s missing from RuleOrder", f.Rule)
		}
	}
	if got := componentTagFindings(componentUses(features[:1], nil, nil, nil, nil, nil), nil); len(got) != 1 || got[0].Rule != RuleComponentSingleUse {
		t.Fatalf("a clean vocabulary reports only what applies: %+v", got)
	}
}

// B-0061: component-note-only counts the note, not the component. A marker
// cerne itself asks for (golden-path) is charged by no tag rule, a note that
// shares one component with a feature is reached, and the hint's retag clears it.
func TestComponentNoteOnlyCountsNotes(t *testing.T) {
	features := []store.Feature{{ID: "F-0001", State: domain.StateImplemented, Components: []string{"ui"}}}
	notes := []store.Note{
		{ID: "N-0001", State: domain.NoteActive, Components: []string{"golden-path"}},
		{ID: "N-0002", State: domain.NoteActive, Components: []string{"ui", "react"}},
		{ID: "N-0003", State: domain.NoteActive, Components: []string{"react"}},
	}
	noteOnly := func() []Finding {
		return slices.DeleteFunc(componentTagFindings(componentUses(features, notes, nil, nil, nil, nil), notes), func(f Finding) bool {
			return f.Rule != RuleComponentNoteOnly
		})
	}
	for _, f := range componentTagFindings(componentUses(features, notes, nil, nil, nil, nil), notes) {
		if strings.Contains(f.Detail, "golden-path") {
			t.Errorf("%s charges the golden-path marker: %q", f.Rule, f.Detail)
		}
	}
	got := noteOnly()
	if len(got) != 1 || !strings.HasPrefix(got[0].Detail, "1 active notes share no component with any feature: N-0003 (react)") {
		t.Fatalf("findings = %+v, want only N-0003", got)
	}
	if !strings.Contains(got[0].Detail, "'cerne note retag N-0003 --component react,<feature component>'") {
		t.Errorf("the hint does not retag the charged note with its whole list: %q", got[0].Detail)
	}
	notes[2].Components = []string{"react", "ui"} // cerne note retag N-0003 --component react,ui
	if got := noteOnly(); len(got) != 0 {
		t.Fatalf("the retag the hint teaches did not clear the finding: %+v", got)
	}
}

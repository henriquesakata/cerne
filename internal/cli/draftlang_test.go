package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0359 criterion 9: each draft prompt follows the documentation language.
func TestDraftPrompt_FollowsDocsLanguage(t *testing.T) {
	f := store.Feature{ID: "F-0001", Title: "Sign in with Google"}
	prompts := []struct {
		name   string
		kind   doclang.Kind
		prompt string
	}{
		{"spec", doclang.Spec, BuildSpecDraftPrompt(f, "")},
		{"plan", doclang.Plan, BuildPlanDraftPrompt(f, "spec", "", nil)},
		{"result", doclang.Result, BuildResultDraftPrompt(f, "spec", "plan", "")},
		{"decision", doclang.Decision, BuildDecisionDraftPrompt(store.Decision{ID: "D-0001", Title: "Use Postgres"}, "")},
		{"runbook", doclang.Runbook, BuildRunbookDraftPrompt(store.Runbook{ID: "RB-0001", Title: "Rotate the key"}, "")},
	}
	for _, p := range prompts {
		t.Run(p.name, func(t *testing.T) {
			if got := draftPromptIn(doclang.PT, p.kind, p.prompt); got != p.prompt {
				t.Error("the Portuguese prompt changed")
			}
			en := draftPromptIn(doclang.EN, p.kind, p.prompt)
			if !strings.Contains(en, "Write the WHOLE document in English") {
				t.Error("the English prompt lacks the language directive")
			}
			for i, heading := range doclang.Sections(p.kind, doclang.EN) {
				pt := doclang.Sections(p.kind, doclang.PT)[i]
				// The builder must have listed the Portuguese heading, or the
				// swap below proves nothing.
				if !strings.Contains(p.prompt, "## "+pt+"\n") {
					t.Fatalf("the builder does not list the section %q", pt)
				}
				if !strings.Contains(en, "## "+heading+"\n") {
					t.Errorf("the English prompt does not list the section %q", heading)
				}
				if pt != heading && strings.Contains(en, "## "+pt+"\n") {
					t.Errorf("the English prompt still lists the Portuguese section %q", pt)
				}
			}
		})
	}
}

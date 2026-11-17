package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0447: an example a draft prompt hands the model is either a <placeholder>
// shape or comes from doclang in the project's language — never an English
// sentence the model would copy into a pt-BR document.
func TestDraftPromptsDictateNoEnglishExampleSentence(t *testing.T) {
	f := store.Feature{ID: "F-0001", Title: "Sign in"}
	kids := []store.Feature{{ID: "T-0001", Title: "Task", State: domain.StateCandidate}}
	prompts := map[string]string{
		"spec":   BuildSpecDraftPrompt(doclang.PT, f, ""),
		"plan":   BuildPlanDraftPrompt(doclang.PT, f, "spec", "", kids),
		"result": BuildResultDraftPrompt(doclang.PT, f, "spec", "plan", ""),
	}
	for name, p := range prompts {
		for _, banned := range []string{"Agent report:", "Gauntlet record:", "[parallel]", "(session 2026"} {
			if strings.Contains(p, banned) {
				t.Errorf("%s pt prompt dictates the English example %q", name, banned)
			}
		}
	}
	if !strings.Contains(prompts["plan"], doclang.Examples(doclang.PT).Parallel) {
		t.Errorf("the pt plan prompt does not use the pt parallel mark %q", doclang.Examples(doclang.PT).Parallel)
	}
	if !strings.Contains(BuildPlanDraftPrompt(doclang.EN, f, "spec", "", kids), doclang.Examples(doclang.EN).Parallel) {
		t.Error("the en plan prompt does not use the en parallel mark")
	}
}

// F-0447 round 2: the rules the pt-BR prompts carried before the translation
// keep their force, and every sentence a model could copy into the document is
// the project's language's (the Gauntlet blind judge lost round 1 on these).
func TestDraftPromptsKeepTheRulesOfTheOriginals(t *testing.T) {
	f := store.Feature{ID: "F-0001", Title: "Sign in"}
	kids := []store.Feature{{ID: "T-0001", Title: "Task", State: domain.StateCandidate}}
	for _, l := range doclang.Langs() {
		ex := doclang.Examples(l)
		rules := map[string][]string{
			BuildSpecDraftPrompt(l, f, ""): {"anchored in real code (file.go:line)", "do not omit the \"how\"", ex.LevelLine, ex.OriginExample, "do not name LLM models or vendors", "Do not include a status metadata line"},
			BuildPlanDraftPrompt(l, f, "spec", "", kids): {"each task leaves the repository green", "Never commit a red state", "test first (TDD)", "citing the number of the spec criterion", "the exact Conventional Commit message"},
			BuildResultDraftPrompt(l, f, "spec", "plan", ""): {"Record what was DELIVERED, not what was planned", "binary, 1:1", "never invent a round"},
			BuildDecisionDraftPrompt(l, store.Decision{ID: "D-0001", Title: "Use X"}, ""): {ex.DecisionVoice, ex.DecisionTitle, "ALWAYS list the negatives/trade-offs", "give 2+ options with pros/cons"},
			BuildRunbookDraftPrompt(l, store.Runbook{ID: "RB-0001", Title: "Rotate"}, ""): {ex.ExpectedOutput, "numbered steps, action first, copyable commands"},
		}
		for prompt, wants := range rules {
			for _, want := range wants {
				if want == "" || !strings.Contains(prompt, want) {
					t.Errorf("%s: a draft prompt lacks the rule %q", l, want)
				}
			}
		}
	}
	// No English sentence for a model to copy into a pt-BR document.
	pt := BuildDecisionDraftPrompt(doclang.PT, store.Decision{ID: "D-0001", Title: "Use X"}, "") +
		BuildRunbookDraftPrompt(doclang.PT, store.Runbook{ID: "RB-0001", Title: "Rotate"}, "")
	for _, banned := range []string{"We will X", "Should we use X", "you should see"} {
		if strings.Contains(pt, banned) {
			t.Errorf("a pt prompt dictates the English example %q", banned)
		}
	}
}

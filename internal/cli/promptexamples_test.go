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

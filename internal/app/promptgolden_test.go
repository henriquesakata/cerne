package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// checkPromptGolden compares got with testdata/<name>; PROMPT_GOLDEN_UPDATE=1
// regenerates it (only on a deliberate prompt change, F-0447).
func checkPromptGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if os.Getenv("PROMPT_GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("the prompt differs from %s (PROMPT_GOLDEN_UPDATE=1 regenerates it)", golden)
	}
}

// TestRoastPromptGoldens (F-0447 crit. 1, 2): the spec and plan critics' prompts
// in both languages, closed by the reply-language line.
func TestRoastPromptGoldens(t *testing.T) {
	docs := []RoastDoc{{Label: "SPEC", Body: "## Goal\n\n1. WHEN x THE SYSTEM SHALL y"}, {Label: "PLANO", Body: "## Tasks\n\n1. Do y"}}
	for _, l := range doclang.Langs() {
		checkPromptGolden(t, "roast_spec_"+string(l)+".golden", BuildRoastPrompt(l, RoastPersonas[0], "F-0001", "Sign in", "## Goal\n\n1. WHEN x THE SYSTEM SHALL y"))
		checkPromptGolden(t, "roast_plan_"+string(l)+".golden", BuildRoastPromptFor(l, PlanRoastPersonas[0], "F-0001", "Sign in", PlanRoastIntro, docs))
	}
}

// TestCapabilityBootstrapPromptGoldens: the prompt follows the backend's
// documentation language only in its closing line.
func TestCapabilityBootstrapPromptGoldens(t *testing.T) {
	for _, l := range doclang.Langs() {
		repoDir, _, _ := newFeatureBackend(t)
		p, err := store.LoadProject(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		p.DocsLanguage = string(l)
		if err := store.SaveProject(repoDir, p); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Sign in", State: domain.StateImplemented}); err != nil {
			t.Fatal(err)
		}
		got, err := CapabilityBootstrapPrompt(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		checkPromptGolden(t, "capability_bootstrap_"+string(l)+".golden", got)
	}
}

package cli

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestDraftPromptsStateTheOutputContract is F-0290 criterion 2: every
// document-draft prompt states the same output contract BuildPagePrompt states
// (F-0287) — the answer IS the markdown, not a report about having produced it.
// Reused verbatim from app.DraftOutputContract so the five never drift.
func TestDraftPromptsStateTheOutputContract(t *testing.T) {
	f := store.Feature{ID: "F-0001", Title: "Boletos", Module: "finance"}
	for name, prompt := range map[string]string{
		"spec":     BuildSpecDraftPrompt(f, ""),
		"plan":     BuildPlanDraftPrompt(f, "spec", "", nil),
		"result":   BuildResultDraftPrompt(f, "spec", "plan", ""),
		"decision": BuildDecisionDraftPrompt(store.Decision{ID: "D-0001", Title: "Postgres"}, ""),
		"runbook":  BuildRunbookDraftPrompt(store.Runbook{ID: "RB-0001", Title: "Rotacionar chave"}, ""),
	} {
		if !strings.Contains(prompt, app.DraftOutputContract) {
			t.Errorf("%s draft prompt does not state app.DraftOutputContract:\n%s", name, prompt)
		}
	}
}

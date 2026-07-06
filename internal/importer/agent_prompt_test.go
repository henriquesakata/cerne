package importer

import (
	"strings"
	"testing"
)

func TestBuildPlanPromptContainsModelAndSchema(t *testing.T) {
	prompt := BuildPlanPrompt([]Source{
		{RelPath: "CLAUDE.md", Content: "ESTADOSECRETO atual do projeto"},
		{RelPath: "HISTORY.md", Content: "MUDANCAX no release"},
	})

	// (crit. 2) the cerne model: every kind and a representative state each.
	for _, kind := range []string{"feature", "decision", "reference", "runbook", "note"} {
		if !strings.Contains(prompt, kind) {
			t.Errorf("prompt missing kind %q", kind)
		}
	}
	for _, state := range []string{"in_development", "obsolete", "active"} {
		if !strings.Contains(prompt, state) {
			t.Errorf("prompt missing state %q", state)
		}
	}
	// (crit. 2) the plan schema fields.
	for _, field := range []string{"items", "kind", "title", "state", "body", "url", "informs", "source"} {
		if !strings.Contains(prompt, field) {
			t.Errorf("prompt missing schema field %q", field)
		}
	}
	// (crit. 1) the discovered sources, by path and content.
	for _, want := range []string{"CLAUDE.md", "ESTADOSECRETO", "HISTORY.md", "MUDANCAX"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing source token %q", want)
		}
	}
}

func TestBuildPlanPromptTruncatesLargeSource(t *testing.T) {
	big := strings.Repeat("A", sourceCap) + "TAILMARKER_SHOULD_BE_CUT"
	prompt := BuildPlanPrompt([]Source{{RelPath: "big.md", Content: big}})
	if strings.Contains(prompt, "TAILMARKER_SHOULD_BE_CUT") {
		t.Error("prompt kept content past the cap")
	}
	if !strings.Contains(prompt, "[truncated]") {
		t.Error("prompt missing truncation marker")
	}
}

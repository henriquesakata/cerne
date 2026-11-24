package importer

import (
	"strings"
	"testing"
)

// F-0500 crit. 1: the plan carries `evidence` and the schema advertises it.
func TestParsePlanReadsEvidence(t *testing.T) {
	data := []byte(`{"items":[{"kind":"note","title":"N","state":"active","body":"b","source":"CLAUDE.md","evidence":["a","b"]}]}`)
	p, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if got := p.Items[0].Evidence; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Evidence = %v, want [a b]", got)
	}
	if !strings.Contains(planSchemaJSON, `"evidence"`) {
		t.Fatal("planSchemaJSON does not contain the evidence field")
	}
}

// F-0500 crit. 2: the prompt asks for literal evidence and scopes the
// "do not copy" rule to the body.
func TestBuildPlanPromptAsksLiteralEvidence(t *testing.T) {
	prompt := BuildPlanPrompt([]Source{{RelPath: "CLAUDE.md", Content: "x"}})
	for _, want := range []string{
		"evidence",
		"1 to 3 snippets copied literally from the source",
		"do not copy the source verbatim in the body",
		"evidence must be copied unchanged",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

package cli

import (
	"strings"
	"testing"
)

func TestAskDelegationPrintsPrompt(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	out, err := execCerne(t, "ask", "o que é boletos?", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask: %v\n%s", err, out)
	}
	// Delegation prints the assembled prompt for the host agent, not an answer.
	if !strings.Contains(out, "Pergunta: o que é boletos?") {
		t.Fatalf("ask output missing the assembled question:\n%s", out)
	}
	if !strings.Contains(out, "Respeite o estado de cada fonte") {
		t.Fatalf("ask output missing the state-respect instruction:\n%s", out)
	}
}

func TestAskEmptyQuestion(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "ask", "   ", "--dir", projDir); err == nil {
		t.Fatalf("ask with blank question succeeded, want error")
	}
}

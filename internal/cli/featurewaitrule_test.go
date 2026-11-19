package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/hooks"
)

// waitHint is the conditional the agent reads at a human gate (F-0460
// criterion 8): wait in the background where the harness wakes it, else stop.
const waitHint = "if your harness wakes you when a background command ends"

// TestGateTextsTeachTheWait (F-0460 criterion 8): every text an agent reads
// at a human gate names the wait — conditioned on the harness, never telling
// the agent to approve or accept.
func TestGateTextsTeachTheWait(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	out, err := cardNew(t, "Exportar o kanban em CSV", "--context", "o time pede", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	wantInOrder(t, stepBlock(out), stepHuman+"F-0001'", waitHint, "cerne feature wait F-0001 --for approved")

	if out, err := execCerne(t, "feature", "retag", "F-0001", "--facet", "code", "--component", "cli", "--dir", projDir); err != nil {
		t.Fatalf("retag: %v\n%s", err, out)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent") // the human-gate wording is the agent's
	out, _ = execCerne(t, "feature", "approve", "F-0001", "--check", "--dir", projDir)
	wantInOrder(t, out, "Next: ask a human to run 'cerne feature approve F-0001'.", waitHint, "cerne feature wait F-0001 --for approved")

	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	out, err = execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	wantInOrder(t, out, "human accept: cerne feature accept F-0001", waitHint, "cerne feature wait F-0001 --for done")

	asset, err := os.ReadFile(filepath.Join("..", "hooks", "assets", "cerne-register-reminder.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"RegisterRules": hooks.RegisterRules, "register reminder asset": string(asset)} {
		gate := text[strings.Index(text, "HUMAN GATES"):]
		gate = gate[:strings.Index(gate, "\n")]
		for _, want := range []string{waitHint, "cerne feature wait F-… --for approved", "--for done", "otherwise just stop"} {
			if !strings.Contains(gate, want) {
				t.Errorf("%s HUMAN GATES line lacks %q:\n%s", name, want, gate)
			}
		}
	}
}

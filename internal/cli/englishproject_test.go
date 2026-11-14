package cli

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// ptProtocolForm is every pt-BR protocol form an English project must never
// be shown (F-0418 crit. 9).
var ptProtocolForm = regexp.MustCompile(`Barra|Método|Saída|Pergunta ao juiz|Rodada|vitória|derrota|empate|Achados do crítico|critério|Verificação|ESCLARECER`)

// TestEnglishProject_NoPortugueseProtocolForm (F-0418 crit. 9): in a project
// documented in English, with the Gauntlet on, every refusal along the cycle
// — no bar, a bar slot, no judge question, a result missing a criterion's
// evidence and its Gauntlet record — and next on each refused feature teach
// only the English forms.
func TestEnglishProject_NoPortugueseProtocolForm(t *testing.T) {
	out := protocolGuidanceTranscript(t, "en") // features 1-3: the spec refusals

	projDir, _, repoDir := setupProjectIn(t, "en")
	t.Setenv("CERNE_ACTOR", "")
	var b strings.Builder
	run := func(args ...string) {
		o, err := execCerne(t, append(args, "--dir", projDir)...)
		fmt.Fprintf(&b, "$ cerne %s\n%s", strings.Join(args, " "), o)
		if err != nil {
			fmt.Fprintf(&b, "error: %v\n", err)
		}
	}
	id := newFeature(t, projDir, "Export widgets", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Export widgets\n\n## Problem\n\nA real problem.\n\n## Goal and acceptance criteria\n\nGoal.\n\n1. WHEN a THE SYSTEM SHALL b.\n2. WHEN c THE SYSTEM SHALL d.\n\n## Verification\n\nBar: test fixture — named reference\nQuestion for the judge: which is clearer?\n")
	commitBackend(t, repoDir)
	run("feature", "approve", id)
	writeDoc(t, repoDir, id, "plan.md", "---\ncerne_id: "+id+"\n---\n\n# Export widgets\n\n## Strategy\n\nOne MR, one task (criteria 1-2).\n")
	commitBackend(t, repoDir)
	run("feature", "start", id)
	writeDoc(t, repoDir, id, "result.md", "---\ncerne_id: "+id+"\n---\n\n# Export widgets\n\n## Result\n\nDelivered; criterion 1 proved by TestA.\n")
	commitBackend(t, repoDir)
	run("feature", "complete", id)
	run("next", id)
	if !strings.Contains(b.String(), "in_development") || !strings.Contains(b.String(), "incomplete result") {
		t.Fatalf("the result refusal was not reached:\n%s", b.String())
	}

	if m := ptProtocolForm.FindAllString(out+b.String(), -1); len(m) > 0 {
		t.Fatalf("English project shown pt-BR forms %q:\n%s\n%s", m, out, b.String())
	}
}

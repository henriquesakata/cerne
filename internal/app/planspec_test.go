package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
)

// sha256Spec is the spec_digest F-0377 pins: sha256 hex of lint.SpecContent.
func sha256Spec(spec []byte) string {
	sum := sha256.Sum256(lint.SpecContent(spec))
	return hex.EncodeToString(sum[:])
}

// TestPlanWriteRecordsSpecDigest (F-0377 crit. 1): every plan write F-0375
// records — push, the editor, amend, the plan draft — carries spec_digest, the
// sha256 of the spec's SpecContent at that instant; a push carrying the spec
// too is written against the pushed spec. A stub plan, a spec write and a
// feature without a spec carry no digest.
func TestPlanWriteRecordsSpecDigest(t *testing.T) {
	authorEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_PROVIDER", "openai")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateApproved)
	specBytes, err := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	stored := sha256Spec(specBytes)
	plan := "---\ncerne_id: F-0001\n---\n\n# Plano\n\n## Tarefas\n\n1. Fazer.\n"
	newSpec := []byte("# Spec\n\n## Problema\n\nOutra versão.\n" + testBarTail)
	push := func(changes ...FileChange) func() error {
		return func() error { return RecordPushedDocs(repoDir, changes, PushAuthor(projDir, changes)) }
	}
	draftLLM := LLMAuthor(project.LLMConfig{Mode: "byo-key", Model: "m"})

	cases := []struct {
		name, id, doc string
		write         func() error
		want          string // "" = no spec_digest
	}{
		{"push", "F-0001", "plan", push(FileChange{Path: "content/features/F-0001/plan.md", Content: []byte(plan)}), stored},
		{"push with the spec", "F-0001", "plan", push(
			FileChange{Path: "content/features/F-0001/spec.md", Content: newSpec},
			FileChange{Path: "content/features/F-0001/plan.md", Content: []byte(plan)},
		), sha256Spec(newSpec)},
		{"push of a stub plan", "F-0001", "plan", push(FileChange{Path: "content/features/F-0001/plan.md", Content: []byte("---\ncerne_id: F-0001\n---\n\n# Plano\n\n## Tarefas\n")}), ""},
		{"push without a spec", "F-0002", "plan", push(FileChange{Path: "content/features/F-0002/plan.md", Content: []byte(plan)}), ""},
		{"editor", "F-0001", "plan", func() error {
			return SaveFeatureDocBy(repoDir, projDir, "F-0001", "plan", plan, HumanAuthor())
		}, stored},
		{"amend", "F-0001", "plan", func() error {
			_, _, _, err := AmendFeatureDoc(repoDir, projDir, "F-0001", "plan", "# Plano\n\n## Tarefas\n\n1. Fazer melhor.\n", "refeito")
			return err
		}, stored},
		{"plan draft", "F-0001", "plan", func() error {
			if err := WriteStampedDraft(FeatureDocPath(repoDir, "F-0001", "plan.md"), "F-0001", "plan", "# Plano\n\n## Tarefas\n\n"+strings.Repeat("1. Escrito pelo LLM a partir da spec vigente, com passos reais.\n", 5)); err != nil {
				return err
			}
			return RecordDocWrite(repoDir, "F-0001", "plan", draftLLM)
		}, stored},
		{"spec write", "F-0001", "spec", func() error {
			return SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", string(specBytes)+"\nMais.\n")
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.write(); err != nil {
				t.Fatalf("write: %v", err)
			}
			ev, n := lastDocEdit(t, repoDir, tc.id)
			if n == 0 || ev.Payload["doc"] != tc.doc {
				t.Fatalf("last doc_edited = %v (%d events), want a %s write", ev.Payload, n, tc.doc)
			}
			got, has := ev.Payload["spec_digest"]
			if tc.want == "" && has {
				t.Fatalf("spec_digest = %q, want none (payload %v)", got, ev.Payload)
			}
			if tc.want != "" && got != tc.want {
				t.Fatalf("spec_digest = %q, want %q (payload %v)", got, tc.want, ev.Payload)
			}
		})
	}
}

// TestStartWarnsPlanBehindSpec (F-0377 crit. 3): the F-0371 case — the plan
// written, then the spec rewritten and re-approved. NextStep still sends the
// agent to the start, with the warning; the start warns and proceeds. A plan
// pushed again after the spec (crit. 4), or a feature planned once, gets no
// warning.
func TestStartWarnsPlanBehindSpec(t *testing.T) {
	authorEnv(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	behind := seedApproved(t, repoDir, projDir, "Plano velho", "")
	fresh := seedApproved(t, repoDir, projDir, "Plano em dia", "")
	if err := SaveFeatureDoc(repoDir, projDir, behind.ID, "spec", realDoc(behind.ID)+"\n## Novo critério\n\nQuinze critérios agora.\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionFeature(repoDir, projDir, behind.ID, domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("re-approve: %v", err)
	}
	const head, tail = "warning: F-0001 plan predates the current spec (plan written ", ") — re-read the spec and push the plan again"

	step, err := NextStep(repoDir, projDir, behind.ID)
	if err != nil {
		t.Fatal(err)
	}
	if step.Command != "cerne feature start "+behind.ID || !strings.Contains(step.Step, head) || !strings.Contains(step.Step, tail) {
		t.Fatalf("NextStep = %+v, want the start with the plan warning", step)
	}
	out := mustStart(t, repoDir, projDir, behind.ID)
	var warned string
	for _, w := range out.Warnings {
		if strings.HasPrefix(w, head) {
			warned = w
		}
	}
	if out.To != domain.StateInDevelopment || !strings.HasSuffix(warned, tail) || !strings.Contains(warned, ", spec changed ") {
		t.Fatalf("start = %s, warnings %q — want in_development with the plan warning", out.To, out.Warnings)
	}

	if step, _ := NextStep(repoDir, projDir, fresh.ID); strings.Contains(step.Step, "warning") {
		t.Fatalf("fresh plan: NextStep = %+v, want no warning", step)
	}
	for _, w := range mustStart(t, repoDir, projDir, fresh.ID).Warnings {
		if strings.Contains(w, "plan predates") {
			t.Fatalf("fresh plan warned: %q", w)
		}
	}
}

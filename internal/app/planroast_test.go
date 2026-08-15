package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestPlanRoastPersonasContract pins F-0275 criteria 1/2: three plan critics
// with distinct lenses, the conformity lens demanding a verdict per numbered
// spec criterion, and a prompt that carries BOTH documents explicitly
// delimited — the plan is judged against the spec, so a spec-only prompt
// cannot answer "criterion N: covered by which task?".
func TestPlanRoastPersonasContract(t *testing.T) {
	if len(PlanRoastPersonas) != 3 {
		t.Fatalf("PlanRoastPersonas = %d, want exactly 3", len(PlanRoastPersonas))
	}
	docs := []RoastDoc{
		{Label: "SPEC", Body: "## Objetivo\n\n1. QUANDO x O SISTEMA DEVE y"},
		{Label: "PLANO", Body: "## Sequência de tarefas\n\n1. Fazer y"},
	}
	for _, p := range PlanRoastPersonas {
		prompt := BuildRoastPromptFor(p, "F-0001", "Login social", PlanRoastIntro, docs)
		for _, want := range []string{
			"trecho EXATO",
			"nunca aprove nem reprove",
			"contexto fresco",
			"no máximo 5",
			"=== SPEC (início) ===",
			"=== SPEC (fim) ===",
			"=== PLANO (início) ===",
			"=== PLANO (fim) ===",
			"Login social",
			"QUANDO x O SISTEMA DEVE y",
			"1. Fazer y",
			p.Lens,
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("persona %s: prompt missing %q:\n%s", p.Key, want, prompt)
			}
		}
	}
	if PlanRoastPersonas[0].Lens == PlanRoastPersonas[1].Lens ||
		PlanRoastPersonas[1].Lens == PlanRoastPersonas[2].Lens ||
		PlanRoastPersonas[0].Lens == PlanRoastPersonas[2].Lens {
		t.Fatalf("plan personas share a lens: %+v", PlanRoastPersonas)
	}
	// Criterion 1(a): the conformity persona demands a per-criterion verdict
	// in a literal format — as a MATRIX, explicitly outside the five-finding
	// cap. Capping it would make the contract unsatisfiable on a spec with
	// six criteria: the critic would have to break one rule or the other.
	conf := PlanRoastPersonas[0]
	for _, want := range []string{"critério N", "coberto pela tarefa", "descoberto"} {
		if !strings.Contains(conf.Matrix, want) {
			t.Errorf("conformity matrix missing %q: %s", want, conf.Matrix)
		}
	}
	for _, want := range []string{"não tem teto", "não conta como achado"} {
		if !strings.Contains(conf.Matrix, want) {
			t.Errorf("matrix must be exempt from the finding cap, missing %q: %s", want, conf.Matrix)
		}
	}
	confPrompt := BuildRoastPromptFor(conf, "F-0001", "T", PlanRoastIntro, docs)
	if !strings.Contains(confPrompt, conf.Matrix) {
		t.Errorf("conformity prompt does not carry the matrix instruction:\n%s", confPrompt)
	}
	// The escape hatch must not delete the matrix on a clean plan.
	if strings.Contains(confPrompt, "Nada relevante pela sua lente → responda exatamente") {
		t.Errorf("conformity prompt offers the bare no-findings escape, which erases the matrix:\n%s", confPrompt)
	}
	// Two documents in the prompt: an unsourced quote is ambiguous.
	if !strings.Contains(confPrompt, "E o documento de onde veio") {
		t.Errorf("multi-document prompt must demand the source of each quote:\n%s", confPrompt)
	}
	// Criterion 1(c): coverage of verification belongs to the conformity lens.
	// The risk persona must not do that job (no per-criterion verdict) and
	// must hand it off explicitly, so the two reports don't say the same thing.
	risk := PlanRoastPersonas[2]
	if strings.Contains(risk.Lens, "coberto pela tarefa") {
		t.Errorf("risk lens duplicates the conformity verdict: %s", risk.Lens)
	}
	if !strings.Contains(risk.Lens, "outro crítico") {
		t.Errorf("risk lens must hand criterion coverage off explicitly: %s", risk.Lens)
	}
}

// TestBuildRoastPromptBackCompat pins that the F-0271 spec entry point is
// unchanged by the parametrization: same single delimited spec, no PLANO
// fence leaking into a spec roast.
func TestBuildRoastPromptBackCompat(t *testing.T) {
	prompt := BuildRoastPrompt(RoastPersonas[0], "F-0002", "Título", "corpo da spec")
	if !strings.Contains(prompt, "=== SPEC (início) ===") || !strings.Contains(prompt, "corpo da spec") {
		t.Fatalf("spec prompt lost its document:\n%s", prompt)
	}
	if strings.Contains(prompt, "PLANO") {
		t.Fatalf("spec roast prompt leaked a PLANO fence:\n%s", prompt)
	}
}

// TestRunRoastWithPersonaSet pins F-0275 criterion 1: the engine runs any
// persona set in parallel over any document bundle, marking a failed critic
// partial instead of killing the round.
func TestRunRoastWithPersonaSet(t *testing.T) {
	docs := []RoastDoc{{Label: "SPEC", Body: "s"}, {Label: "PLANO", Body: "p"}}
	prov := &stubRoastProvider{
		delay: 20 * time.Millisecond,
		reply: func(_ context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, PlanRoastPersonas[1].Lens) {
				return "", context.DeadlineExceeded
			}
			return "- [alta] achado — trecho: \"p\"", nil
		},
	}
	rep := RunRoastWith(context.Background(), prov, PlanRoastPersonas, "F-0003", "T", PlanRoastIntro, docs, time.Second)
	if len(rep.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(rep.Results))
	}
	if prov.maxSeen != len(PlanRoastPersonas) {
		t.Errorf("max in-flight = %d, want %d — every critic runs concurrently, not just some", prov.maxSeen, len(PlanRoastPersonas))
	}
	if !rep.Partial {
		t.Errorf("round with a failed critic must be marked partial")
	}
	if rep.AllFailed() {
		t.Errorf("round with two successful critics is not AllFailed")
	}
	// Every document is fingerprinted: the plan roast asserts conformity
	// AGAINST the spec, so a spec edit invalidates the verdicts just like a
	// plan edit does.
	if len(rep.Fingerprints) != 2 {
		t.Fatalf("fingerprints = %+v, want one per document", rep.Fingerprints)
	}
	if rep.Fingerprints[0].Label != "spec" || rep.Fingerprints[1].Label != "plano" {
		t.Errorf("fingerprint labels = %+v, want spec then plano", rep.Fingerprints)
	}
}

// TestRunRoastWithEmptyAnswer pins that a critic answering nothing is a failed
// critic, not a silent success: the contract has a literal phrase for "nothing
// to report", so an empty string means the critic did not run.
func TestRunRoastWithEmptyAnswer(t *testing.T) {
	docs := []RoastDoc{{Label: "SPEC", Body: "s"}, {Label: "PLANO", Body: "p"}}
	prov := &stubRoastProvider{reply: func(_ context.Context, _ string) (string, error) {
		return "   \n", nil
	}}
	rep := RunRoastWith(context.Background(), prov, PlanRoastPersonas, "F-0003", "T", PlanRoastIntro, docs, time.Second)
	if !rep.Partial || !rep.AllFailed() {
		t.Fatalf("all-empty round: partial=%v allFailed=%v, want true/true", rep.Partial, rep.AllFailed())
	}
}

// TestFormatPlanRoastReport pins F-0275 criterion 4: the plan report is its
// own artifact, stamped with time + plan sha, and the stamp is readable back
// (the staleness badge is the stamp's consumer).
func TestFormatPlanRoastReport(t *testing.T) {
	rep := RoastReport{
		FeatureID: "F-0004",
		Title:     "Roast do plano",
		Results:   []RoastResult{{Persona: PlanRoastPersonas[0], Output: "- [alta] x — trecho: \"y\""}},
		At:        time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
		Fingerprints: []RoastFingerprint{
			{Label: "spec", SHA: "cafebabe"},
			{Label: "plano", SHA: "deadbeef"},
		},
	}
	md := FormatPlanRoastReport(rep)
	for _, want := range []string{
		"Roast do plano",
		"F-0004",
		"spec sha256:cafebabe",
		"plano sha256:deadbeef",
		"2026-08-15T12:00:00Z",
		PlanRoastPersonas[0].Title,
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("plan roast report missing %q:\n%s", want, md)
		}
	}
	if got := ParseRoastSHA(md, "plano"); got != "deadbeef" {
		t.Fatalf("ParseRoastSHA(plano) = %q, want deadbeef", got)
	}
	if got := ParseRoastSHA(md, "spec"); got != "cafebabe" {
		t.Fatalf("ParseRoastSHA(spec) = %q, want cafebabe", got)
	}
	if got := ParseRoastSHA("# Roast do plano\n\nsem carimbo\n", "plano"); got != "" {
		t.Fatalf("ParseRoastSHA on an unstamped report = %q, want \"\"", got)
	}
}

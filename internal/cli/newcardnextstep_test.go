package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// The moment-of-action guidance after a card is born or started rides the
// command's own stdout (F-0426): the one channel every harness delivers, with
// or without hooks.

// stepBlock returns stdout from its last "Next step:" on, or "" when the
// command printed none.
func stepBlock(stdout string) string {
	i := strings.LastIndex(stdout, "Next step:")
	if i < 0 {
		return ""
	}
	return stdout[i:]
}

// wantInOrder fails unless every want appears in s, each after the previous.
func wantInOrder(t *testing.T, s string, wants ...string) {
	t.Helper()
	at := 0
	for _, w := range wants {
		i := strings.Index(s[at:], w)
		if i < 0 {
			t.Fatalf("want %q (in order) in:\n%s", w, s)
		}
		at += i + len(w)
	}
}

const (
	stepCardNow    = "complete the card now, in this same turn, before any code"
	stepSpec       = "write the real spec: cerne pull, fill .docs/current/features/"
	stepTags       = "tag it: cerne feature retag "
	stepCapability = "link its capability: cerne feature capability "
	stepCheck      = "--check' and fix every impediment it lists except open clarifications"
	stepHuman      = "human approve: ask a human to run 'cerne feature approve "
	stepNoImpl     = "does not implement an unapproved feature"
)

func cardNew(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	stdout, _, err = execCerneSplit(t, append([]string{"feature", "new"}, args...)...)
	return stdout, err
}

// Criterion 1 (a, b, c, e, f): a seeded, untagged candidate gets every step,
// in order, and the block is the last thing on stdout.
func TestNewCardNextStep_Seeded(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := cardNew(t, "Exportar o kanban em CSV", "--context", "o time pede", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	block := stepBlock(out)
	if block == "" {
		t.Fatalf("no Next step block:\n%s", out)
	}
	wantInOrder(t, block, stepCardNow,
		stepSpec+"F-0001-*/spec.md, cerne push",
		stepTags+"F-0001 --facet <f> --component <c>",
		"cerne feature approve F-0001 "+stepCheck,
		stepHuman+"F-0001'", stepNoImpl)
	if strings.Contains(block, stepCapability) {
		t.Fatalf("no capability taxonomy, yet the block asks for one:\n%s", block)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), strings.TrimRight(block, "\n")) || strings.Count(out, "Next step:") != 1 {
		t.Fatalf("the block must be the one Next step and the end of stdout:\n%s", out)
	}
}

// Criterion 1 (b, c): a card born with its spec and both tag axes gets neither
// the spec step nor the tag step.
func TestNewCardNextStep_FileAndTags(t *testing.T) {
	projDir, _, _ := setupProject(t)
	spec := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(spec, []byte("# Exportar o kanban em CSV\n\n## Problema\n\nO time pede.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := cardNew(t, "Exportar o kanban em CSV", "--file", spec, "--facet", "code", "--component", "cli", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	block := stepBlock(out)
	wantInOrder(t, block, stepCardNow, "cerne feature approve F-0001 --check", stepHuman+"F-0001'")
	for _, no := range []string{stepSpec, stepTags} {
		if strings.Contains(block, no) {
			t.Fatalf("block carries %q for a card that already has it:\n%s", no, block)
		}
	}
}

// Criterion 1 (c): one axis missing is still an incomplete card.
func TestNewCardNextStep_HalfTagged(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := cardNew(t, "Exportar o kanban em CSV", "--context", "o time pede", "--facet", "code", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if !strings.Contains(stepBlock(out), stepTags+"F-0001") {
		t.Fatalf("facet without component, yet no tag step:\n%s", out)
	}
}

// Criterion 1 (d): the capability step shows only when the project curates a
// taxonomy and the card has no capability.
func TestNewCardNextStep_Capability(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "capability", "new", "Studio", "--dir", projDir); err != nil {
		t.Fatalf("capability new: %v\n%s", err, out)
	}
	out, err := cardNew(t, "Exportar o kanban em CSV", "--context", "o time pede", "--facet", "code", "--component", "cli", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if !strings.Contains(stepBlock(out), stepCapability+"F-0001 C-") {
		t.Fatalf("taxonomy present, card without capability, yet no capability step:\n%s", out)
	}
}

// Criterion 4: an epic is a candidate and gets the block; a task is born
// approved and does not.
func TestNewCardNextStep_EpicAndTask(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, _, err := execCerneSplit(t, "epic", "new", "Meu épico", "--context", "por quê", "--dir", projDir)
	if err != nil {
		t.Fatalf("epic new: %v\n%s", err, out)
	}
	wantInOrder(t, stepBlock(out), stepCardNow, stepHuman+"E-0001'")

	e, err := store.LoadFeature(repoDir, "E-0001")
	if err != nil {
		t.Fatal(err)
	}
	seedApprovedState(t, repoDir, e)
	out, _, err = execCerneSplit(t, "task", "new", "Primeiro passo", "--parent", "E-0001", "--context", "ctx", "--dir", projDir)
	if err != nil {
		t.Fatalf("task new: %v\n%s", err, out)
	}
	if strings.Contains(out, stepCardNow) || strings.Contains(out, "cerne feature approve T-0001") {
		t.Fatalf("a task is born approved, yet it got the candidate block:\n%s", out)
	}
}

// Criterion 4: a refused creation prints no block.
func TestNewCardNextStep_Refused(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := cardNew(t, "Exportar o kanban em CSV", "--context", "o time pede", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	out, errOut, err := execCerneSplit(t, "feature", "new", "Exportar o kanban em CSV", "--context", "outra vez", "--dir", projDir)
	if err == nil {
		t.Fatalf("duplicate accepted:\n%s", out)
	}
	if strings.Contains(out+errOut, "Next step:") {
		t.Fatalf("refused creation printed a Next step:\n%s%s", out, errOut)
	}
}

// Criterion 4: a refused start (the plan is still a stub) prints no block,
// on either stream.
func TestFeatureStartNextStep_Refused(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Exportar o kanban em CSV", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	out, errOut, err := execCerneSplit(t, "feature", "start", id, "--dir", projDir)
	if err == nil {
		t.Fatalf("start with a stub plan accepted:\n%s", out)
	}
	if strings.Contains(out+errOut, "Next step:") {
		t.Fatalf("refused start printed a Next step:\n%s%s", out, errOut)
	}
}

// The approve step names the bar form the agent proposed, in the project's
// language (F-0418), and none in light mode.
func TestNewCardNextStep_BarForm(t *testing.T) {
	for _, c := range []struct{ lang, gauntlet, want, not string }{
		{"pt-BR", "", "approving accepts the `Barra:` line you proposed", "`Bar:`"},
		{"en", "", "approving accepts the `Bar:` line you proposed", "Barra"},
		{"pt-BR", "off", "", "approving accepts"},
	} {
		projDir, _, _ := setupProjectIn(t, c.lang)
		setGauntlet(t, projDir, c.gauntlet)
		out, err := cardNew(t, "Exportar o kanban em CSV", "--context", "o time pede", "--dir", projDir)
		if err != nil {
			t.Fatalf("%s/%s feature new: %v\n%s", c.lang, c.gauntlet, err, out)
		}
		block := stepBlock(out)
		if !strings.Contains(block, c.want) || strings.Contains(block, c.not) || !strings.Contains(block, stepHuman) {
			t.Fatalf("%s/gauntlet=%q: want %q, not %q, in:\n%s", c.lang, c.gauntlet, c.want, c.not, block)
		}
	}
}

// Criterion 5: the block depends on the card and the project only — the same
// text whoever runs it, with or without agent hooks.
func TestNewCardNextStep_HarnessIndependent(t *testing.T) {
	projDir, _, _ := setupProject(t)
	ids := regexp.MustCompile(`F-\d{4}`)
	var blocks []string
	for i, env := range []map[string]string{
		{"CERNE_ACTOR": "agent"},
		{"CERNE_ACTOR": ""},
		{"CERNE_ACTOR": "", "CERNE_NO_AGENT_HOOKS": "1"},
	} {
		for k, v := range env {
			t.Setenv(k, v)
		}
		title := []string{"Exportar o kanban em CSV", "Importar planilha de custos", "Arquivar salas antigas"}[i]
		// B-0086: an agent shell must choose; the same explicit origin in all
		// three keeps the block comparable.
		out, err := cardNew(t, title, "--context", "o time pede", "--origin", "agent", "--dir", projDir)
		if err != nil {
			t.Fatalf("feature new %q: %v\n%s", title, err, out)
		}
		blocks = append(blocks, ids.ReplaceAllString(stepBlock(out), "F-X"))
	}
	if blocks[0] == "" || blocks[0] != blocks[1] || blocks[1] != blocks[2] {
		t.Fatalf("block varies with the environment:\n%q\n%q\n%q", blocks[0], blocks[1], blocks[2])
	}
}

// Criterion 3: start points at the notes before the code and at real-time
// progress, then at the result.
func TestFeatureStartNextStep(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Exportar o kanban em CSV", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
	commitBackend(t, repoDir)
	out, _, err := execCerneSplit(t, "feature", "start", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	wantInOrder(t, stepBlock(out),
		"before the code: consult the active notes (cerne note list, filter with --facet/--component) and cite the applicable conventions in plan.md",
		"mark each proving test with a comment `F-0001 crit. N`", // F-0473 crit. 12
		"keep progress real-time: tick finished tasks in plan.md, gather evidence in result.md, cerne sync at each milestone",
		"when done: fill in the result ('cerne result draft F-0001' scaffolds it): run 'cerne pull', edit .docs/current/features/F-0001-exportar-o-kanban-em-csv/result.md and run 'cerne push'") // B-0074
}

// Criterion 8, end to end with agent hooks off (a hookless harness): the
// three moments each print their block through the CLI alone.
func TestLifecycleGuidanceWithoutHooks(t *testing.T) {
	t.Setenv("CERNE_NO_AGENT_HOOKS", "1")
	projDir, _, repoDir := setupProject(t)

	out, err := cardNew(t, "Exportar o kanban em CSV", "--context", "o time pede", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new --context: %v\n%s", err, out)
	}
	wantInOrder(t, stepBlock(out), stepCardNow, stepSpec+"F-0001-", stepTags+"F-0001", stepHuman+"F-0001'")

	spec := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(spec, []byte(completeSpec(strings.SplitN(realDoc("F-0002", "Importar a planilha de custos."), "---\n", 3)[2])), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = cardNew(t, "Importar planilha de custos", "--file", spec, "--facet", "code", "--component", "cli", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new --file: %v\n%s", err, out)
	}
	block := stepBlock(out)
	wantInOrder(t, block, stepCardNow, stepHuman+"F-0002'")
	if strings.Contains(block, stepSpec) || strings.Contains(block, stepTags) {
		t.Fatalf("complete card born with spec and tags, yet the block asks for them:\n%s", block)
	}

	if out, err := execCerne(t, "feature", "approve", "F-0002", "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	writeDoc(t, repoDir, "F-0002", "plan.md", realDoc("F-0002", "Passo 1: implementar."))
	commitBackend(t, repoDir)
	out, _, err = execCerneSplit(t, "feature", "start", "F-0002", "--dir", projDir)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	wantInOrder(t, stepBlock(out), "consult the active notes", "cerne sync at each milestone", ".docs/current/features/F-0002-", "/result.md")
}

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// newFeatureWithSpec births a feature via the one-shot path so the spec is
// real content and the doc gate is already open.
func newFeatureWithSpec(t *testing.T, projDir, title, specBody string) {
	t.Helper()
	specFile := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(specFile, []byte(specBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "feature", "new", title, "--file", specFile, "--dir", projDir); err != nil {
		t.Fatalf("feature new --file: %v\noutput: %s", err, out)
	}
}

// TestApproveRefusesOpenClarifications pins F-0175 criterion 7: approve over a
// spec with open [ESCLARECER] markers refuses listing the questions; --force
// proceeds; a spec with only resolved markers passes untouched.
func TestApproveRefusesOpenClarifications(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	newFeatureWithSpec(t, projDir, "Com pergunta aberta",
		"# Com pergunta aberta\n\n## Problema\n\nContexto real.\n\n"+
			"[ESCLARECER: qual formato usar?]\n\n[ESCLARECER: onde persistir? | doc | sidecar]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")

	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("approve with open clarifications succeeded, want refusal:\n%s", out)
	}
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	for _, q := range []string{"qual formato usar?", "onde persistir?"} {
		if !strings.Contains(err.Error(), q) {
			t.Errorf("error message missing question %q: %v", q, err)
		}
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate (refusal must not transition)", f.State)
	}

	// --force crosses the gate deliberately.
	out, err = execCerne(t, "feature", "approve", "F-0001", "--force", "--dir", projDir)
	if err != nil {
		t.Fatalf("approve --force: %v\noutput: %s", err, out)
	}
	f, err = store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved after --force", f.State)
	}
}

// TestApproveRefusesOpenClarificationsEnglish pins F-0182 criterion 7 — the
// end of the fail-open-by-language gate: an English spec with an open
// [CLARIFY] marker blocks approve exactly like a pt-BR one. Before the en
// vocabulary entered the catalog, this spec sailed through silently.
func TestApproveRefusesOpenClarificationsEnglish(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	newFeatureWithSpec(t, projDir, "English spec",
		"# English spec\n\n## Problema\n\nReal context in English.\n\n"+
			"[CLARIFY: which storage backend?]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")

	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("approve with open [CLARIFY] succeeded, want refusal:\n%s", out)
	}
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	if !strings.Contains(err.Error(), "which storage backend?") {
		t.Errorf("error message missing the English question: %v", err)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate (refusal must not transition)", f.State)
	}
}

// TestApprovePassesResolvedClarifications pins criterion 5's data side: the
// resolved [ESCLARECIDO] trail does not block approve.
func TestApprovePassesResolvedClarifications(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	newFeatureWithSpec(t, projDir, "Só resolvidas",
		"# Só resolvidas\n\n## Problema\n\nContexto real.\n\n"+
			"[ESCLARECIDO: qual formato usar? → pipe inline]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")

	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("approve with resolved markers: %v\noutput: %s", err, out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved", f.State)
	}
}

// TestApproveClarifyRefusalNumbersMatchAnswer pins F-0330 crit. 5 (and 3 end to
// end): the refusal's commands, run literally in the order printed, each
// resolve exactly their own question — an answer renumbers the open markers
// after it, so printed in document order the second command would answer the
// third question. The plan's marker is never touched, and the approve the
// refusal asks for passes once they are answered.
func TestApproveClarifyRefusalNumbersMatchAnswer(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Tres perguntas",
		"# Tres perguntas\n\n## Problema\n\nContexto real.\n\n"+
			"[ESCLARECER: qual formato usar?]\n\n[ESCLARECER: a barra cobre a CLI e a UI? | só CLI | as duas]\n\n[ESCLARECER: qual o prazo?]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")
	writeDoc(t, repoDir, "F-0001", "plan.md", "---\ncerne_id: F-0001\n---\n\n[ESCLARECER: do plano?]\n")
	commitBackend(t, repoDir)

	_, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	msg := err.Error()
	if missing, _ := missingCommands(msg); len(missing) > 0 {
		t.Errorf("refusal names commands that do not exist: %v", missing)
	}
	lines := refusalCommands(msg)
	if len(lines) != 3 {
		t.Fatalf("want 3 answer lines, got %d:\n%s", len(lines), msg)
	}
	answered := map[string]string{}
	for i, m := range lines {
		cmdline, ans := m[3], fmt.Sprintf("resp-%d", i+1)
		if strings.Contains(cmdline, "<k>") {
			cmdline, ans = strings.Replace(cmdline, "<k>", "1", 1), "só CLI"
		} else {
			cmdline = strings.Replace(cmdline, `"..."`, `"`+ans+`"`, 1)
		}
		args := regexpSplit(cmdline)
		if out, err := execCerne(t, append(args[1:], "--dir", projDir)...); err != nil {
			t.Fatalf("%s: %v\n%s", cmdline, err, out)
		}
		answered[m[2]] = ans
		spec := readBackendSpec(t, repoDir)
		for _, o := range lines {
			want := "[ESCLARECER: " + o[2]
			if a, ok := answered[o[2]]; ok {
				want = "[ESCLARECIDO: " + o[2] + " → " + a + "]"
			}
			if !strings.Contains(spec, want) {
				t.Fatalf("after %q the spec lacks %q:\n%s", cmdline, want, spec)
			}
		}
	}
	plan, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "plan.md"))
	if !strings.Contains(string(plan), "[ESCLARECER: do plano?]") {
		t.Errorf("an answer reached the plan's marker:\n%s", plan)
	}
	if out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("approve after following the refusal: %v\n%s", err, out)
	}
}

// F-0330 (critic C2): 'approve f-0001' on a spec with an open question is
// refused like 'approve F-0001' — before, the gate read the lowercase id as
// unknown and let it pass, and a case-insensitive filesystem approved it.
func TestApproveClarifyGateLowercaseID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Minuscula",
		"# Minuscula\n\n## Problema\n\nContexto real. [ESCLARECER: qual o prazo?]\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n")
	_, err := execCerne(t, "feature", "approve", "f-0001", "--dir", projDir)
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("approve f-0001: %v, want ErrOpenClarifications", err)
	}
	if f, err := store.LoadFeature(repoDir, "F-0001"); err != nil || f.State != domain.StateCandidate {
		t.Fatalf("after the refusal: %+v, %v; want candidate", f, err)
	}
}

// refusalCommandRe takes one answer row of the approve refusal: number,
// question and the command as printed.
var refusalCommandRe = regexp.MustCompile(`(?m)^\s+(\d+)\. (.+?)\s+→ (cerne clarify answer \S+ \d+ --expect \S+ (?:--pick <k>|--answer "\.\.\."))`)

// refusalCommands returns the refusal's answer rows in printed order.
func refusalCommands(msg string) [][]string { return refusalCommandRe.FindAllStringSubmatch(msg, -1) }

// F-0330 (critic C1): the printed commands run in natural number order
// (1, 2, 3) — not the printed one — never write a wrong marker. #1 is right;
// #2 and #3 now name other markers (the third one in the plan, which the
// refusal never showed), so each is refused with nothing written.
func TestClarifyAnswerExpectNaturalOrder(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Ordem natural",
		"# Ordem natural\n\n## Problema\n\nContexto real.\n\n"+
			"[ESCLARECER: pergunta A?]\n\n[ESCLARECER: pergunta B? | sim | não]\n\n[ESCLARECER: pergunta C?]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")
	writeDoc(t, repoDir, "F-0001", "plan.md", "---\ncerne_id: F-0001\n---\n\n[ESCLARECER: pergunta do plano?]\n")
	commitBackend(t, repoDir)

	_, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	byNumber := map[string]string{}
	for _, m := range refusalCommands(err.Error()) {
		byNumber[m[1]] = strings.NewReplacer("<k>", "1", `"..."`, `"resp-`+m[1]+`"`).Replace(m[3])
	}
	if len(byNumber) != 3 {
		t.Fatalf("want 3 printed commands, got %v:\n%v", byNumber, err)
	}
	run := func(n string) error {
		args := regexpSplit(byNumber[n])
		_, err := execCerne(t, append(args[1:], "--dir", projDir)...)
		return err
	}
	if err := run("1"); err != nil {
		t.Fatalf("#1: %v", err)
	}
	spec := readBackendSpec(t, repoDir)
	plan, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "plan.md"))
	for _, n := range []string{"2", "3"} {
		if err := run(n); !errors.Is(err, app.ErrClarificationMoved) {
			t.Fatalf("#%s after #1: err = %v, want ErrClarificationMoved", n, err)
		}
	}
	if got := readBackendSpec(t, repoDir); got != spec || !strings.Contains(got, "[ESCLARECIDO: pergunta A? → resp-1]") {
		t.Fatalf("a refused command wrote the spec:\n%s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "plan.md")); string(got) != string(plan) {
		t.Fatalf("a refused command wrote the plan:\n%s", got)
	}
}

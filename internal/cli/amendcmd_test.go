package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedResultFeatures persists F-0001.. straight into the backend, one per
// state, each with a real spec and result, and returns the id per state.
func seedResultFeatures(t *testing.T, repoDir string, states ...domain.FeatureState) map[domain.FeatureState]string {
	t.Helper()
	now := time.Now().UTC()
	ids := map[domain.FeatureState]string{}
	for i, s := range states {
		id := fmt.Sprintf("F-%04d", i+1)
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feature " + id, State: s, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Spec."))
		writeDoc(t, repoDir, id, "result.md", realDoc(id, "Result original."))
		ids[s] = id
	}
	commitBackend(t, repoDir)
	return ids
}

// amendFile writes a result body that passes the result checks and returns its path.
func amendFile(t *testing.T, id string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "result.md")
	if err := os.WriteFile(p, []byte(realDoc(id, "Result emendado.")), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// assertAmendRefused runs `feature amend` on id and asserts the refusal names
// want and leaves the backend result byte-identical.
func assertAmendRefused(t *testing.T, projDir, repoDir, id, want string) error {
	t.Helper()
	resultPath := filepath.Join(repoDir, "content", "features", id, "result.md")
	before, _ := os.ReadFile(resultPath)
	_, err := execCerne(t, "feature", "amend", id, "--file", amendFile(t, id), "--reason", "corrigir", "--dir", projDir)
	if !errors.Is(err, app.ErrAmendNotImplemented) || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: err = %v, want ErrAmendNotImplemented naming %q", id, err, want)
	}
	if after, _ := os.ReadFile(resultPath); string(after) != string(before) {
		t.Fatalf("%s: result.md changed", id)
	}
	return err
}

// TestAmendRefusesOtherStates (F-0305 crit. 9): outside implemented the amend
// refuses without writing, naming the state's path — pull+push while live,
// errata once closed.
func TestAmendRefusesOtherStates(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ids := seedResultFeatures(t, repoDir, domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment,
		domain.StateDone, domain.StateRejected, domain.StateSuperseded)
	for state, id := range ids {
		want := wantFix(state, id, "result")
		err := assertAmendRefused(t, projDir, repoDir, id, want)
		if state == domain.StateDone {
			t.Logf("amend refusal (done): %v", err)
		}
	}
}

// TestAmendRequiresReasonAndBody (F-0305 crit. 11): no reason, a blank reason
// or an empty body is refused before the backend is touched.
func TestAmendRequiresReasonAndBody(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	resultPath := filepath.Join(repoDir, "content", "features", id, "result.md")
	before, _ := os.ReadFile(resultPath)
	head0, _ := gitback.HeadSHA(origin)
	empty := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(empty, []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"no reason", "needs a reason", []string{"--file", amendFile(t, id)}},
		{"blank reason", "needs a reason", []string{"--file", amendFile(t, id), "--reason", "  "}},
		{"empty body", "empty result body", []string{"--file", empty, "--reason", "corrigir"}},
	} {
		_, err := execCerne(t, append([]string{"feature", "amend", id, "--dir", projDir}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want a refusal containing %q", tc.name, err, tc.want)
		}
	}
	if after, _ := os.ReadFile(resultPath); string(after) != string(before) {
		t.Fatal("result.md changed")
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

// TestFeatureAmendCommand (F-0305 crit. 8, CLI side): the verb replaces the
// result of an implemented feature and says how to read it back.
func TestFeatureAmendCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	out, err := execCerne(t, "feature", "amend", id, "--file", amendFile(t, id), "--reason", "faltava o critério", "--dir", projDir)
	if err != nil {
		t.Fatalf("amend: %v\n%s", err, out)
	}
	t.Logf("amend output: %s", out)
	want := "✓ " + id + " result amended (implemented) — already synced. Read it with 'cerne show " + id + " --doc result'."
	if !strings.Contains(out, want) {
		t.Fatalf("output = %q, want %q", out, want)
	}
	got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md"))
	if !strings.Contains(string(got), "Result emendado.") {
		t.Fatalf("result not replaced:\n%s", got)
	}

	// The same body again is a no-op, and says so.
	out, err = execCerne(t, "feature", "amend", id, "--file", amendFile(t, id), "--reason", "de novo", "--dir", projDir)
	if want := id + " result unchanged — nothing to amend."; err != nil || !strings.Contains(out, want) || strings.Contains(out, "amended") {
		t.Fatalf("identical amend: err %v, output = %q, want %q", err, out, want)
	}
}

// TestFeatureAmendFromStdinAndUnknownID: the documented `cat result.md |` form
// amends, and an unknown id is refused without moving origin (judge finding,
// F-0305).
func TestFeatureAmendFromStdinAndUnknownID(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	body, _ := os.ReadFile(amendFile(t, id))
	pipeStdinBody(t, string(body))
	if out, err := execCerne(t, "feature", "amend", id, "--reason", "via stdin", "--dir", projDir); err != nil || !strings.Contains(out, "result amended") {
		t.Fatalf("stdin amend: err %v, output %q", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md")); !strings.Contains(string(got), "Result emendado.") {
		t.Fatalf("result not replaced from stdin:\n%s", got)
	}

	head0, _ := gitback.HeadSHA(origin)
	if _, err := execCerne(t, "feature", "amend", "F-0999", "--file", amendFile(t, "F-0999"), "--reason", "x", "--dir", projDir); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: err = %v, want store.ErrNotFound", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved on an unknown id: %s -> %s", head0, h)
	}
}

// F-0307: an amend whose --file is the .docs/ copy says to remove it — the
// copy keeps blocking push until it is gone — on both the success and the
// unchanged message; a file elsewhere gets no such line.
func TestAmendFromDocsCopySaysToRemoveIt(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	local := filepath.Join(projDir, ".docs", "current", "features", id+"-x", "result.md")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, local, realDoc(id, "Result emendado."))
	rel := ".docs/current/features/" + id + "-x/result.md"
	want := "remove " + rel + " now"

	for _, step := range []string{"amended", "unchanged"} {
		out, err := execCerne(t, "feature", "amend", id, "--file", local, "--reason", "r", "--dir", projDir)
		if err != nil || !strings.Contains(out, step) || !strings.Contains(out, want) {
			t.Fatalf("%s: err %v, output %q, want %q", step, err, out, want)
		}
	}
	other := amendFile(t, id)
	for i := 0; i < 2; i++ {
		out, err := execCerne(t, "feature", "amend", id, "--file", other, "--reason", "r", "--dir", projDir)
		if err != nil || strings.Contains(out, "remove ") {
			t.Fatalf("file outside .docs/: err %v, output %q, want no remove line", err, out)
		}
	}
}

// seedDocFeature persists feature id in state s straight into the backend, with
// a real spec and plan, committed.
func seedDocFeature(t *testing.T, repoDir, id string, s domain.FeatureState, spec, plan string) {
	t.Helper()
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feature " + id, State: s, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, repoDir, id, "spec.md", spec)
	writeDoc(t, repoDir, id, "plan.md", plan)
	commitBackend(t, repoDir)
}

// bodyFile writes body to a temp file and returns its path.
func bodyFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.md")
	mustWrite(t, p, body)
	return p
}

// TestFeatureAmendDocCommand (F-0338 crit. 1, CLI side): `--doc spec|plan`
// amends the document of an approved or in-development feature and says which
// revision kept the old text and how to read it; the same body again is a
// no-op that says so, and an unknown --doc is refused.
func TestFeatureAmendDocCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDocFeature(t, repoDir, "F-0001", domain.StateApproved, realDoc("F-0001", "Spec original."), realDoc("F-0001", "Plano original."))
	seedDocFeature(t, repoDir, "F-0002", domain.StateInDevelopment, realDoc("F-0002", "Spec original."), realDoc("F-0002", "Plano original."))

	for _, tc := range []struct{ id, doc, state string }{{"F-0001", "spec", "approved"}, {"F-0002", "plan", "in_development"}} {
		file := bodyFile(t, realDoc(tc.id, "Texto emendado."))
		out, err := execCerne(t, "feature", "amend", tc.id, "--doc", tc.doc, "--file", file, "--reason", "prefixo mudou", "--dir", projDir)
		if err != nil {
			t.Fatalf("amend --doc %s: %v\n%s", tc.doc, err, out)
		}
		for _, want := range []string{
			"✓ " + tc.id + " " + tc.doc + " amended (" + tc.state + ") — revision 1 keeps the previous text: 'cerne show " + tc.id + " --doc " + tc.doc + " --rev 1'",
			"'cerne pull'",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("output = %q, want %q", out, want)
			}
		}
		got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", tc.id, tc.doc+".md"))
		if !strings.Contains(string(got), "Texto emendado.") {
			t.Fatalf("%s not replaced:\n%s", tc.doc, got)
		}
		out, err = execCerne(t, "feature", "amend", tc.id, "--doc", tc.doc, "--file", file, "--reason", "de novo", "--dir", projDir)
		if want := tc.id + " " + tc.doc + " unchanged — nothing to amend."; err != nil || !strings.Contains(out, want) {
			t.Fatalf("identical amend: err %v, output %q, want %q", err, out, want)
		}
	}
	if _, err := execCerne(t, "feature", "amend", "F-0001", "--doc", "xyz", "--file", bodyFile(t, "x"), "--reason", "r", "--dir", projDir); !errors.Is(err, app.ErrUnknownDoc) {
		t.Fatalf("--doc xyz: err = %v, want app.ErrUnknownDoc", err)
	}
}

// realCaseSpec is a synthetic spec with the shape of the case that motivated
// F-0338: criteria pinning a prefix, and three appended amendment sections that
// swap it — two inside the criteria section, one before the out-of-scope one.
// crit lists the criteria already rewritten into the new prefix; sections says
// whether the appended sections are still there.
func realCaseSpec(crit map[int]bool, sections bool) string {
	prefix := func(n int) string {
		if crit[n] {
			return "registry.example.com/acme-apps/"
		}
		return "registry.example.com/acme/"
	}
	var b strings.Builder
	b.WriteString("# Publicar imagens\n\n## Problema\n\nAs imagens saem com o prefixo errado.\n\n## Objetivo e critérios de aceitação\n\n")
	for n := 1; n <= 4; n++ {
		fmt.Fprintf(&b, "%d. A imagem %d é publicada em `%simg%d`.\n", n, n, prefix(n), n)
	}
	if sections {
		b.WriteString("\n## Emenda (2026-11-01) — prefixo\n\nNos critérios 1 e 2, o prefixo passa a ser `acme-apps/`.\n")
		b.WriteString("\n### Emenda de 2026-11-03\n\nNo critério 3, o mesmo.\n")
	}
	b.WriteString("\n## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n")
	if sections {
		b.WriteString("\n## Emendas da revisão (2026-11-05)\n\nNo critério 4, o mesmo.\n")
	}
	b.WriteString("\n## Fora de escopo\n\n- Outros registries.\n")
	return b.String()
}

// TestAmendRealCase (F-0338 crit. 11): the spec with three appended amendments
// goes through three amends, each rewriting the criteria it changes — the
// first one also drops the appended sections, since an amend refuses a body
// that still has one (crit. 5). Then show lists three amendments, the current
// body has no amendment heading, --rev 1 is the original and the lint no longer
// flags it; a plan with "## Emendas da auditoria (…)" is flagged as the plan.
func TestAmendRealCase(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	original := "---\ncerne_id: F-0001\n---\n\n" + realCaseSpec(nil, true)
	seedDocFeature(t, repoDir, "F-0001", domain.StateApproved, original, realDoc("F-0001", "Plano."))
	seedDocFeature(t, repoDir, "F-0002", domain.StateInDevelopment, realDoc("F-0002", "Spec."),
		realDoc("F-0002", "## Tarefas\n\n1. Fazer.\n\n## Emendas da auditoria (W-1)\n\nA tarefa 1 mudou."))

	const specFinding = "content/features/F-0001/spec.md — spec line "
	lintOut, _ := execCerne(t, "lint", "--dir", projDir)
	if got := strings.Count(lintOut, specFinding); got != 3 || !strings.Contains(lintOut, "APPENDED-AMENDMENT") {
		t.Fatalf("before: want three appended-amendment findings on the spec, got %d:\n%s", got, lintOut)
	}
	// The plan-shaped fixture fires with doc=plan.
	if !strings.Contains(lintOut, "content/features/F-0002/plan.md") || !strings.Contains(lintOut, "plan line ") ||
		!strings.Contains(lintOut, "cerne feature amend F-0002 --doc plan") {
		t.Fatalf("plan with an appended amendment not flagged as the plan:\n%s", lintOut)
	}

	for i, step := range []struct {
		crit   map[int]bool
		reason string
	}{
		{map[int]bool{1: true, 2: true}, "emenda 1: prefixo nos critérios 1 e 2"},
		{map[int]bool{1: true, 2: true, 3: true}, "emenda 2: prefixo no critério 3"},
		{map[int]bool{1: true, 2: true, 3: true, 4: true}, "emenda 3: prefixo no critério 4"},
	} {
		out, err := execCerne(t, "feature", "amend", "F-0001", "--doc", "spec", "--file", bodyFile(t, realCaseSpec(step.crit, false)), "--reason", step.reason, "--dir", projDir)
		if err != nil || !strings.Contains(out, fmt.Sprintf("revision %d keeps the previous text", i+1)) {
			t.Fatalf("amend %d: %v\n%s", i+1, err, out)
		}
	}

	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(out, "\n## amendments\n")
	if !ok || strings.Count(block, "spec r") != 3 || !strings.Contains(block, "spec r3 · ") || !strings.Contains(block, "emenda 3: prefixo no critério 4") {
		t.Fatalf("show: want three spec lines under ## amendments, newest first:\n%s", out)
	}
	current, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if lines := lint.AmendmentLines(string(current)); len(lines) != 0 || strings.Contains(string(current), "registry.example.com/acme/") {
		t.Fatalf("current spec still has amendment headings %v or the old prefix:\n%s", lines, current)
	}
	if rev1, err := execCerne(t, "show", "F-0001", "--doc", "spec", "--rev", "1", "--dir", projDir); err != nil || rev1 != original {
		t.Fatalf("--rev 1 = %q, %v — want the original spec", rev1, err)
	}
	if lintOut, _ = execCerne(t, "lint", "--dir", projDir); strings.Contains(lintOut, specFinding) {
		t.Fatalf("after the amends the spec is still flagged:\n%s", lintOut)
	}
}

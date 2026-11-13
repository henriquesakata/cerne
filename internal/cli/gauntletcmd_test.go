package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/gauntlet"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// gauntletFixture: a project (pt-BR) with F-0001 carrying a spec and a plan in
// the backend clone, and its project dir as a git repo on a feature branch
// that adds code.go and ui/dist/a.js over main.
func gauntletFixture(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Alpha", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	doc := filepath.Join(repoDir, "content", "features", "F-0001")
	if err := os.MkdirAll(doc, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(doc, "spec.md"), []byte("## Problema\n\nprosa\n\n## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y.\n"), 0o644)
	os.WriteFile(filepath.Join(doc, "plan.md"), []byte("## Estratégia\n\nlivre\n\n## Contratos fixados pelo TL\n\n- contrato A\n"), 0o644)

	gitIn(t, projDir, "init", "-q", "-b", "main")
	gitIn(t, projDir, "commit", "-q", "--allow-empty", "-m", "base")
	gitIn(t, projDir, "checkout", "-q", "-b", "feature/x")
	os.WriteFile(filepath.Join(projDir, "code.go"), []byte("package x\n"), 0o644)
	os.MkdirAll(filepath.Join(projDir, "ui", "dist"), 0o755)
	os.WriteFile(filepath.Join(projDir, "ui", "dist", "a.js"), []byte("noise\n"), 0o644)
	gitIn(t, projDir, "add", "code.go", "ui")
	gitIn(t, projDir, "commit", "-q", "-m", "feat")
	return projDir, repoDir
}

func TestGauntletArtifactStdout(t *testing.T) {
	projDir, repoDir := gauntletFixture(t)
	head := gitIn(t, repoDir, "rev-parse", "HEAD")
	docsBefore := treeListing(t, filepath.Join(projDir, ".docs"))
	notes := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(notes, []byte("desvio declarado"), 0o644)

	out, err := execCerne(t, "gauntlet", "artifact", "F-0001", "--notes", notes, "--dir", projDir)
	if err != nil {
		t.Fatalf("artifact: %v\n%s", err, out)
	}
	for _, want := range []string{"=== CRITÉRIOS (início) ===", "QUANDO x O SISTEMA DEVE y", "## Contratos fixados pelo TL",
		"## Desvios declarados pelo builder", "desvio declarado", "=== DIFF (início) ===", "code.go", "=== DIFF (fim) ==="} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ui/dist") || strings.Contains(out, "prosa") {
		t.Errorf("noise in output:\n%s", out)
	}
	// critério 5: nothing written to the backend or .docs/.
	if got := gitIn(t, repoDir, "rev-parse", "HEAD"); got != head {
		t.Errorf("backend HEAD moved: %s -> %s", head, got)
	}
	if got := treeListing(t, filepath.Join(projDir, ".docs")); got != docsBefore {
		t.Errorf(".docs/ changed:\n%s\n--\n%s", docsBefore, got)
	}
}

func TestGauntletArtifactOut(t *testing.T) {
	projDir, _ := gauntletFixture(t)
	dest := filepath.Join(t.TempDir(), "artifact.txt")
	stdout, stderr, err := execCerneSplit(t, "gauntlet", "artifact", "F-0001", "--out", dest, "--exclude", "code.go", "--dir", projDir)
	if err != nil {
		t.Fatalf("artifact: %v\n%s", err, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty with --out: %q", stdout)
	}
	data, _ := os.ReadFile(dest)
	if strings.Contains(string(data), "code.go") {
		t.Errorf("--exclude ignored:\n%s", data)
	}
	if strings.Contains(string(data), "ui/dist") {
		t.Errorf("--exclude must add to the defaults, ui/dist is back:\n%s", data)
	}
	for _, want := range []string{"cerne: gauntlet artifact for F-0001 → " + dest, " bytes; diff main..HEAD, excluded: ui/dist, docs.lock, code.go)"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q: %s", want, stderr)
		}
	}
}

func TestGauntletArtifactErrors(t *testing.T) {
	projDir, repoDir := gauntletFixture(t)
	if out, err := execCerne(t, "gauntlet", "artifact", "F-0999", "--dir", projDir); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown feature: err=%v out=%s", err, out)
	}
	if out, err := execCerne(t, "gauntlet", "artifact", "F-0001", "--base", "nope", "--dir", projDir); !errors.Is(err, gauntlet.ErrBadBase) {
		t.Errorf("bad base: err=%v out=%s", err, out)
	}
	// no criteria section: error, nothing written.
	spec := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	os.WriteFile(spec, []byte("## Problema\n\nx\n"), 0o644)
	dest := filepath.Join(t.TempDir(), "a.txt")
	if out, err := execCerne(t, "gauntlet", "artifact", "F-0001", "--out", dest, "--dir", projDir); !errors.Is(err, gauntlet.ErrNoCriteria) || !strings.Contains(err.Error(), "F-0001") {
		t.Errorf("no criteria: err=%v out=%s", err, out)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("--out written despite the error")
	}
	// no base: a repo with neither main nor origin.
	gitIn(t, projDir, "branch", "-m", "main", "trunk")
	if out, err := execCerne(t, "gauntlet", "artifact", "F-0001", "--dir", projDir); !errors.Is(err, gauntlet.ErrNoBase) {
		t.Errorf("no base: err=%v out=%s", err, out)
	}
}

func TestGauntletArtifactNotGit(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Alpha", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := execCerne(t, "gauntlet", "artifact", "F-0001", "--dir", projDir); !errors.Is(err, gauntlet.ErrNotGitRepo) {
		t.Errorf("err=%v out=%s", err, out)
	}
}

func treeListing(t *testing.T, root string) string {
	t.Helper()
	var names []string
	_ = filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err == nil {
			names = append(names, p)
		}
		return nil
	})
	return strings.Join(names, "\n")
}

const recordResultPT = "# R\n\n## Resultado\n\nok\n\n## Aceitação\n\ncritério 1 → teste X.\n\n## O que mudou\n\nx\n\n## Débitos e follow-ups\n\nnenhum\n\n## Aprendizados e desvios\n\nnenhum\n\n## Gauntlet\n\nBarra: o carimbo `## Custo` da F-0309 (`internal/cost/stamp.go`)\n"

// materialize puts a result.md in F-0001's .docs slot, as `cerne pull` does for an active feature.
func materializeResult(t *testing.T, projDir, body string) string {
	t.Helper()
	slot := filepath.Join(projDir, ".docs", "current", "features", "F-0001-alpha")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(slot, "result.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeVerdict(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "judge.txt")
	os.WriteFile(p, []byte(body), 0o644)
	return p
}

func TestGauntletRecordRound(t *testing.T) {
	projDir, _ := gauntletFixture(t)
	res := materializeResult(t, projDir, recordResultPT)
	v := writeVerdict(t, "Análise.\n\n\"B grava sem prosa.\"\n\nVEREDITO: B\n")
	out, err := execCerne(t, "gauntlet", "record", "F-0001", "--verdict", v, "--ours", "A", "--dir", projDir)
	if err != nil {
		t.Fatalf("record: %v\n%s", err, out)
	}
	if want := "cerne: recorded round 1 — derrota in F-0001 result; run 'cerne push' to send it"; !strings.Contains(out, want) {
		t.Errorf("stdout %q lacks %q", out, want)
	}
	b, _ := os.ReadFile(res)
	if !strings.Contains(string(b), "internal/cost/stamp.go`)\nRodada 1 — derrota: \"B grava sem prosa.\"\n") {
		t.Errorf("result:\n%s", b)
	}
	// second round numbers on; critic line appended
	if _, err := execCerne(t, "gauntlet", "record", "F-0001", "--verdict", v, "--ours", "B", "--quote", "citação", "--dir", projDir); err != nil {
		t.Fatal(err)
	}
	out, err = execCerne(t, "gauntlet", "record", "F-0001", "--critic", "1 achado — corrigido", "--dir", projDir)
	if err != nil || !strings.Contains(out, "cerne: recorded critic findings in F-0001 result; run 'cerne push' to send it") {
		t.Fatalf("critic: %v\n%s", err, out)
	}
	b, _ = os.ReadFile(res)
	if !strings.Contains(string(b), "Rodada 2 — vitória: \"citação\"\nAchados do crítico: 1 achado — corrigido\n") {
		t.Errorf("result:\n%s", b)
	}
}

func TestGauntletRecordRefusals(t *testing.T) {
	projDir, _ := gauntletFixture(t)
	res := materializeResult(t, projDir, recordResultPT)
	good := writeVerdict(t, "Frase.\nVEREDITO: A\n")
	bad := writeVerdict(t, "Frase sem veredito.\n")
	for name, c := range map[string]struct {
		args []string
		want error
	}{
		"no mode":          {[]string{"F-0001"}, errGauntletRecordMode},
		"both modes":       {[]string{"F-0001", "--verdict", good, "--ours", "A", "--critic", "x"}, errGauntletRecordMode},
		"verdict, no ours": {[]string{"F-0001", "--verdict", good}, errGauntletRecordMode},
		"no verdict line":  {[]string{"F-0001", "--verdict", bad, "--ours", "A"}, gauntlet.ErrNoVerdict},
		"bad ours":         {[]string{"F-0001", "--verdict", good, "--ours", "Z"}, gauntlet.ErrBadOurs},
		"not materialized": {[]string{"F-0002", "--verdict", good, "--ours", "A"}, errGauntletNotMaterialized},
	} {
		out, err := execCerne(t, append([]string{"gauntlet", "record"}, append(c.args, "--dir", projDir)...)...)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err=%v out=%s", name, err, out)
		}
	}
	if b, _ := os.ReadFile(res); string(b) != recordResultPT {
		t.Errorf("a refusal wrote:\n%s", b)
	}
}

// The line the command wrote passes the complete gate (criterion 5), and the
// backend is untouched until `cerne push`.
func TestGauntletRecordPassesTheCompleteGate(t *testing.T) {
	projDir, repoDir := gauntletFixture(t)
	res := materializeResult(t, projDir, recordResultPT)
	head := gitIn(t, repoDir, "rev-parse", "HEAD")
	v := writeVerdict(t, "Frase.\n\"B vence.\"\nVEREDITO: A\n")
	if out, err := execCerne(t, "gauntlet", "record", "F-0001", "--verdict", v, "--ours", "A", "--dir", projDir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if gitIn(t, repoDir, "rev-parse", "HEAD") != head {
		t.Error("backend moved")
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(res)
	os.WriteFile(filepath.Join(repoDir, "content", "features", "F-0001", "result.md"), body, 0o644)
	if err := app.CheckCompleteResult(repoDir, f, project.GatesConfig{}); err != nil {
		t.Errorf("gate refused the recorded result: %v", err)
	}
}

func TestGauntletRecordRejectsMalformedID(t *testing.T) {
	projDir, _ := gauntletFixture(t)
	materializeResult(t, projDir, recordResultPT)
	good := writeVerdict(t, "Frase.\nVEREDITO: A\n")
	for _, id := range []string{"F-*", "F-0001-*", "../F-0001", "F-1/x", "f-0001"} {
		_, err := execCerne(t, "gauntlet", "record", id, "--verdict", good, "--ours", "A", "--dir", projDir)
		if !errors.Is(err, errGauntletBadID) {
			t.Errorf("%q: err=%v", id, err)
		}
	}
}

// English project: the lines are English and the gate reads them (criterion 5).
func TestGauntletRecordEnglishPassesTheCompleteGate(t *testing.T) {
	projDir, repoDir := gauntletFixture(t)
	p, err := store.LoadProject(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	p.DocsLanguage = "en"
	if err := store.SaveProject(repoDir, p); err != nil {
		t.Fatal(err)
	}
	spec := "## Problem\n\nx\n\n## Goal and acceptance criteria\n\n1. WHEN x THE SYSTEM SHALL y.\n"
	os.WriteFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"), []byte(spec), 0o644)
	res := materializeResult(t, projDir, "# R\n\n## Result\n\nok\n\n## Acceptance\n\ncriterion 1 → test X.\n\n## What changed\n\nx\n\n## Debts and follow-ups\n\nnone\n\n## Learnings and deviations\n\nnone\n\n## Gauntlet\n\nBar: the cost stamp (`internal/cost/stamp.go`)\n")
	lose := writeVerdict(t, "Fine.\n\"B wins.\"\nVERDICT: B\n")
	win := writeVerdict(t, "Fine.\n\"A wins.\"\nVERDICT: A\n")
	for _, v := range []string{lose, win} {
		if out, err := execCerne(t, "gauntlet", "record", "F-0001", "--verdict", v, "--ours", "A", "--dir", projDir); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	body, _ := os.ReadFile(res)
	if !strings.Contains(string(body), "Round 1 — loss: \"B wins.\"\nRound 2 — win: \"A wins.\"\n") {
		t.Fatalf("result:\n%s", body)
	}
	f, _ := store.LoadFeature(repoDir, "F-0001")
	os.WriteFile(filepath.Join(repoDir, "content", "features", "F-0001", "result.md"), body, 0o644)
	if err := app.CheckCompleteResult(repoDir, f, project.GatesConfig{}); err != nil {
		t.Errorf("gate refused the English result: %v", err)
	}
}

func TestGauntletArtifactEvidence(t *testing.T) {
	projDir, _ := gauntletFixture(t)
	tmp := t.TempDir()
	ev := filepath.Join(tmp, "runtime.txt")
	os.WriteFile(ev, []byte("saida real\n"), 0o644)
	dest := filepath.Join(tmp, "a.txt")
	_, stderr, err := execCerneSplit(t, "gauntlet", "artifact", "F-0001", "--evidence", ev, "--out", dest, "--dir", projDir)
	if err != nil {
		t.Fatalf("artifact: %v\n%s", err, stderr)
	}
	data, _ := os.ReadFile(dest)
	for _, want := range []string{"=== EVIDÊNCIA: runtime.txt (início) ===", "saida real", "=== EVIDÊNCIA: runtime.txt (fim) ==="} {
		if !strings.Contains(string(data), want) {
			t.Errorf("bundle missing %q", want)
		}
	}
	if !strings.Contains(stderr, "evidence: runtime.txt 11 bytes") {
		t.Errorf("stderr lacks the evidence size: %s", stderr)
	}
}

// critério 2: a missing evidence file refuses, names the file, writes nothing.
func TestGauntletArtifactEvidenceMissing(t *testing.T) {
	projDir, _ := gauntletFixture(t)
	dest := filepath.Join(t.TempDir(), "a.txt")
	missing := filepath.Join(t.TempDir(), "ghost.txt")
	out, err := execCerne(t, "gauntlet", "artifact", "F-0001", "--evidence", missing, "--out", dest, "--dir", projDir)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), missing) {
		t.Errorf("err=%v out=%s", err, out)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("--out written despite the missing evidence")
	}
}

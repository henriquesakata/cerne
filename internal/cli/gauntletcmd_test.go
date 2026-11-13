package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gauntlet"
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

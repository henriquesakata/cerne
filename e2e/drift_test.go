package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// commitInRepo writes files into a go-git repo and commits them, returning HEAD.
func commitInRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("PlainOpen %s: %v", dir, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	h, err := wt.Commit("c", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return h.String()
}

// featureDir returns the single feature directory under .docs/current/features.
func featureDir(t *testing.T, proj string) string {
	t.Helper()
	root := filepath.Join(proj, ".docs", "current", "features")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", root, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(root, e.Name())
		}
	}
	t.Fatalf("no feature dir under %s", root)
	return ""
}

func writeReal(t *testing.T, dir, name, heading string) {
	t.Helper()
	body := "---\ncerne_id: F-0001\n---\n\n# " + heading + "\n\nConteúdo real desta seção, suficiente para o gate.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória: \"fixture\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestDriftSuspectGoldenPath: an implemented feature whose module code changes
// after the last sync is flagged by `cerne drift`; before the change there is
// no drift. Exercises F-0113 crit. 1, 5, 8 end to end through cli.Root().
func TestDriftSuspectGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()

	// The user project is its own git repo with some code committed.
	if _, err := git.PlainInit(proj, false); err != nil {
		t.Fatalf("PlainInit proj: %v", err)
	}
	commitInRepo(t, proj, map[string]string{"internal/core/core.go": "package core\n"})

	// cerne init, then declare module_paths for the drift rule.
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--module", "core", "--dir", proj)
	appendToFile(t, filepath.Join(proj, ".cernerc"), "\n[module_paths]\ncore = [\"internal/core/**\"]\n")

	// Drive F-0001 all the way to implemented.
	mustCerne(t, "feature", "new", "Motor de core", "--context", "ctx de teste", "--module", "core", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	fdir := featureDir(t, proj)
	writeReal(t, fdir, "spec.md", "Motor de core")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "approve", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "plan.md", "Plano do motor")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "start", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "result.md", "Resultado do motor")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "complete", "F-0001", "--dir", proj)

	proposal := filepath.Join(proj, ".docs", ".drift-proposals", "F-0001.prompt.md")

	// Baseline is the project HEAD recorded during the pushes above. No code has
	// changed since, so there is no drift yet — and --draft writes nothing (crit. 3).
	if out := mustCerne(t, "drift", "--dir", proj); !strings.Contains(out, "No drift detected.") {
		t.Fatalf("expected no drift before code change, got:\n%s", out)
	}
	mustCerne(t, "drift", "--draft", "--dir", proj)
	if _, err := os.Stat(proposal); !os.IsNotExist(err) {
		t.Fatalf("no-drift --draft should not write a proposal, stat err = %v", err)
	}

	// Now the code under module core changes → F-0001's shipped docs may be stale.
	commitInRepo(t, proj, map[string]string{"internal/core/core.go": "package core\n\nfunc New() {}\n"})

	out := mustCerne(t, "drift", "--json", "--dir", proj)
	if !strings.Contains(out, `"entity": "F-0001"`) {
		t.Fatalf("drift --json missing F-0001:\n%s", out)
	}
	if !strings.Contains(out, `"rule": "drift-suspect"`) {
		t.Fatalf("drift --json missing drift-suspect rule:\n%s", out)
	}
	if !strings.Contains(out, "internal/core/core.go") {
		t.Fatalf("drift --json detail missing changed path:\n%s", out)
	}

	// --draft emits a delegation prompt bundle per finding, applies nothing (crit. 1,2,8).
	out = mustCerne(t, "drift", "--draft", "--dir", proj)
	if !strings.Contains(out, "cerne feature errata") {
		t.Fatalf("drift --draft stdout missing apply contract:\n%s", out)
	}
	bundle := mustRead(t, proposal)
	for _, want := range []string{"cerne feature errata F-0001 --file .docs/.drift-proposals/F-0001.md", "internal/core/core.go"} {
		if !strings.Contains(bundle, want) {
			t.Fatalf("prompt bundle missing %q:\n%s", want, bundle)
		}
	}

	// --draft with --json is a usage error.
	if _, err := cerne(t, "drift", "--draft", "--json", "--dir", proj); err == nil {
		t.Fatalf("drift --draft --json should error")
	}
}

func appendToFile(t *testing.T, path, extra string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(extra); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

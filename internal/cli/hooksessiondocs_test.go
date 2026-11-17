package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// worktreeWithCerncerc returns a project whose .cernerc is committed and a
// linked worktree of it: .docs/ is gitignored, so the worktree has none.
func worktreeWithCerncerc(t *testing.T) (projDir, wt string) {
	t.Helper()
	t.Setenv("CERNE_ROOT", "")
	projDir, _, _ = setupProject(t)
	gitRun(t, projDir, "init", "-q")
	if err := os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte(".docs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, projDir, "add", "-A")
	gitRun(t, projDir, "commit", "-q", "-m", "init")
	wt = filepath.Join(projDir, ".claude", "worktrees", "wt1")
	gitRun(t, projDir, "worktree", "add", "-q", "-b", "wt1", wt)
	if _, err := os.Stat(filepath.Join(wt, ".cernerc")); err != nil {
		t.Fatalf("worktree lacks .cernerc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs")); err == nil {
		t.Fatal("worktree already has .docs/")
	}
	return projDir, wt
}

func sessionOut(t *testing.T, dir, format string) string {
	t.Helper()
	out, err := execCerneStdin(t, hookJSON(t, "s", dir, "", nil, nil), "hook", "session-context", "--format", format)
	if err != nil {
		t.Fatalf("%s: %v\n%s", format, err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("%s: output is not valid JSON:\n%s", format, out)
	}
	return out
}

// B-0017: a worktree with its own .cernerc and no .docs/ gets the projection
// materialized by SessionStart, in every format, with a clean envelope.
func TestSessionContextMaterializesMissingDocs(t *testing.T) {
	for _, format := range []string{"claude", "codex", "cursor", "gemini", "copilot"} {
		t.Run(format, func(t *testing.T) {
			_, wt := worktreeWithCerncerc(t)
			out := sessionOut(t, wt, format)
			if _, err := os.Stat(filepath.Join(wt, ".docs", "CONTEXT.md")); err != nil {
				t.Fatalf("CONTEXT.md not materialized: %v\n%s", err, out)
			}
			if strings.Contains(out, "Materialized:") || strings.Contains(out, "run `cerne pull`") {
				t.Fatalf("pull output or notice leaked:\n%s", out)
			}
		})
	}
}

// B-0017: a failing or too slow pull never fails the hook: one notice line.
func TestSessionContextPullFailureOrTimeoutNotice(t *testing.T) {
	_, wt := worktreeWithCerncerc(t)
	oldPull, oldBudget := sessionPull, sessionPullBudget
	t.Cleanup(func() { sessionPull, sessionPullBudget = oldPull, oldBudget })

	sessionPull = func(string) error { return os.ErrPermission }
	if out := sessionOut(t, wt, "claude"); strings.Count(out, "run `cerne pull`") != 1 {
		t.Fatalf("failed pull: want one notice:\n%s", out)
	}

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	sessionPull = func(string) error { <-release; return nil }
	sessionPullBudget = 50 * time.Millisecond
	start := time.Now()
	out := sessionOut(t, wt, "cursor")
	if strings.Count(out, "run `cerne pull`") != 1 || time.Since(start) > 5*time.Second {
		t.Fatalf("slow pull: want one notice, quickly (%v):\n%s", time.Since(start), out)
	}
}

// B-0017: with .docs/ present nothing changes and no pull runs; a worktree
// without its own .cernerc resolves to the main checkout (B-0016), whose
// .docs/ exists.
func TestSessionContextNoPullWhenDocsPresent(t *testing.T) {
	projDir, wt := worktreeWithCerncerc(t)
	oldPull := sessionPull
	t.Cleanup(func() { sessionPull = oldPull })
	sessionPull = func(string) error { t.Error("pull ran although .docs/CONTEXT.md exists"); return nil }

	docs := filepath.Join(projDir, ".docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "CONTEXT.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := sessionOut(t, projDir, "claude"); strings.Contains(out, "run `cerne pull`") {
		t.Fatalf("notice with .docs/ present:\n%s", out)
	}

	// Worktree without .cernerc: remove it there so the walk goes to projDir.
	if err := os.Remove(filepath.Join(wt, ".cernerc")); err != nil {
		t.Fatal(err)
	}
	if out := sessionOut(t, wt, "claude"); strings.Contains(out, "run `cerne pull`") {
		t.Fatalf("notice for a worktree resolving to the main checkout:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs")); err == nil {
		t.Fatal(".docs/ created in a worktree that uses the main project")
	}
}

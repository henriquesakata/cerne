package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
)

// B-0080: git runs a commit hook at the root of the tree that commits, and
// hands it a message file in that tree's admin dir, which for a linked
// worktree or a submodule lives under another tree's .git. These tests build
// the real layouts with git and call the hooks the way git does.

// gitDir is the absolute admin dir git uses for the tree at dir.
func gitDir(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, dir, "rev-parse", "--absolute-git-dir"))
}

// commitAll makes dir a git repository on branch holding every file in it.
func commitAll(t *testing.T, dir, branch string) {
	t.Helper()
	initGitRepo(t, dir, branch)
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "init")
}

// addWorktree adds a linked worktree of repo on a new branch and returns its
// root and the message file git hands its hooks
// (<repo>/.git/worktrees/<name>/COMMIT_EDITMSG), holding msg.
func addWorktree(t *testing.T, repo, branch, msg string) (wt, msgPath string) {
	t.Helper()
	wt = filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", branch, wt)
	msgPath = filepath.Join(gitDir(t, wt), "COMMIT_EDITMSG")
	if !strings.Contains(filepath.ToSlash(msgPath), "/.git/worktrees/") {
		t.Fatalf("git admin dir of the worktree = %s, want one under .git/worktrees", msgPath)
	}
	if err := os.WriteFile(msgPath, []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	return wt, msgPath
}

// B-0080 crit. 1: the trailer follows the worktree's close_on, not the main
// checkout's.
func TestHookCommitTrailer_WorktreeCloseOn(t *testing.T) {
	projDir, _ := refsProject(t, "", false)
	commitAll(t, projDir, "main")
	wt, msg := addWorktree(t, projDir, "feat/f-0001-x", "fix: y\n\nCerne: F-0001\n")
	if err := project.SetSettingValue(wt, "mirror.close_on", project.ScopeProject, "merge"); err != nil {
		t.Fatalf("set mirror.close_on in the worktree: %v", err)
	}
	t.Chdir(wt)
	if got, want := runTrailerHook(t, msg), "fix: y\n\nCerne: F-0001\nCloses: "+issueRef+"\n"; got != want {
		t.Fatalf("message =\n%q\nwant\n%q", got, want)
	}
}

// B-0080 crit. 2: the worktree's [gates] commit_trailer decides the Cerne:
// stamp.
func TestHookCommitTrailer_WorktreeGate(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), "sess-a", "W-0001"); err != nil {
		t.Fatal(err)
	}
	commitAll(t, projDir, "main")
	wt, msg := addWorktree(t, projDir, "feat/x", "feat: coisa\n")
	enableCommitTrailer(t, wt)
	t.Chdir(wt)
	if got := runTrailerHook(t, msg); !strings.Contains(got, "\n\nCerne: W-0001\n") {
		t.Fatalf("the worktree's gate is on: want the Cerne: stamp\n%s", got)
	}
}

// B-0080 crit. 3: the guard reads the worktree's branch and its [guard] mode.
func TestGuardCommitMsg_WorktreeBranch(t *testing.T) {
	t.Run("an id on the worktree's branch blocks", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		putFeature(t, repoDir, "F-0001", domain.StateCandidate)
		commitAll(t, projDir, "main")
		wt, msg := addWorktree(t, projDir, "claude/f-0001-x", "feat: coisa\n")
		t.Chdir(wt)
		if out, err := execCerneStdin(t, "", "guard", "commit-msg", msg); err == nil {
			t.Fatalf("the worktree's branch cites a candidate: want a block\noutput: %s", out)
		}
	})
	t.Run("the worktree's warn mode warns", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		putFeature(t, repoDir, "F-0001", domain.StateCandidate)
		commitAll(t, projDir, "main")
		wt, msg := addWorktree(t, projDir, "claude/f-0001-x", "feat: coisa\n")
		cfg, _, err := project.LoadConfig(wt)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Guard.Mode = "warn"
		if err := project.SaveConfig(wt, cfg); err != nil {
			t.Fatal(err)
		}
		t.Chdir(wt)
		out, err := execCerneStdin(t, "", "guard", "commit-msg", msg)
		if err != nil {
			t.Fatalf("the worktree's guard mode is warn: want exit 0, got %v\noutput: %s", err, out)
		}
		if !strings.Contains(out, "F-0001") {
			t.Fatalf("warn mode must print the notice:\n%s", out)
		}
	})
}

// B-0080 crit. 4: an id on the main checkout's branch is not charged on a
// commit in a worktree whose branch cites none.
func TestGuardCommitMsg_MainBranchNotCharged(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	putFeature(t, repoDir, "F-0001", domain.StateCandidate)
	commitAll(t, projDir, "claude/f-0001-x")
	wt, msg := addWorktree(t, projDir, "chore/cleanup", "chore: limpeza\n")
	t.Chdir(wt)
	out, err := execCerneStdin(t, "", "guard", "commit-msg", msg)
	if err != nil || strings.Contains(out, "F-0001") {
		t.Fatalf("the main checkout's branch is not this commit's: want a silent pass, got %v\n%s", err, out)
	}
}

// B-0080 crit. 5: in a submodule with its own .cernerc, the hooks use the
// submodule's project and branch, not the superproject's.
func TestCommitHooks_Submodule(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableCommitTrailer(t, projDir)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), "sess-a", "W-0001"); err != nil {
		t.Fatal(err)
	}
	putFeature(t, repoDir, "F-0001", domain.StateCandidate) // after work start, which resyncs the clone
	commitAll(t, projDir, "main")
	// The superproject is another cerne project, with no clone and no gate.
	super := t.TempDir()
	if err := os.WriteFile(filepath.Join(super, ".cernerc"), []byte("project_id = \"other\"\nbackend = \""+filepath.ToSlash(filepath.Join(t.TempDir(), "other.git"))+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, super, "main")
	gitIn(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", projDir, "mod")
	mod := filepath.Join(super, "mod")
	gitIn(t, mod, "checkout", "-q", "-b", "claude/f-0001-x")
	admin := gitDir(t, mod)
	if !strings.Contains(filepath.ToSlash(admin), "/.git/modules/") {
		t.Fatalf("git admin dir of the submodule = %s, want one under .git/modules", admin)
	}
	msg := filepath.Join(admin, "COMMIT_EDITMSG")
	t.Chdir(mod)

	t.Run("trailer", func(t *testing.T) {
		if err := os.WriteFile(msg, []byte("feat: coisa\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := runTrailerHook(t, msg); !strings.Contains(got, "\n\nCerne: W-0001\n") {
			t.Fatalf("the submodule's gate is on: want the Cerne: stamp\n%s", got)
		}
	})
	t.Run("guard", func(t *testing.T) {
		if err := os.WriteFile(msg, []byte("feat: coisa\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := execCerneStdin(t, "", "guard", "commit-msg", msg); err == nil {
			t.Fatalf("the submodule's branch cites a candidate: want a block\noutput: %s", out)
		}
	})
}

// B-0080 crit. 6: in the main checkout git hands the relative path
// .git/COMMIT_EDITMSG, run from the checkout's root: unchanged.
func TestCommitHooks_MainCheckoutRelativePath(t *testing.T) {
	projDir, _ := refsProject(t, "merge", false)
	commitAll(t, projDir, "main")
	msg := filepath.Join(".git", "COMMIT_EDITMSG")
	t.Chdir(projDir)
	if err := os.WriteFile(msg, []byte("fix: y\n\nCerne: F-0001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := runTrailerHook(t, msg), "fix: y\n\nCerne: F-0001\nCloses: "+issueRef+"\n"; got != want {
		t.Fatalf("message =\n%q\nwant\n%q", got, want)
	}
}

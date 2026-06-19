package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

func writeTempLock(t *testing.T, l project.Lock) string {
	t.Helper()
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	p := filepath.Join(t.TempDir(), "docs.lock")
	if err := os.WriteFile(p, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write temp lock: %v", err)
	}
	return p
}

func readLockAt(t *testing.T, path string) project.Lock {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock %s: %v", path, err)
	}
	var l project.Lock
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatalf("unmarshal lock %s: %v", path, err)
	}
	return l
}

// commitSiblingOf creates, in repoDir, a commit whose parent is `parent`
// (on a detached HEAD): a sibling of parent's other child, so neither is an
// ancestor of the other.
func commitSiblingOf(t *testing.T, repoDir, parent, filename string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(parent), Force: true}); err != nil {
		t.Fatalf("Checkout %s: %v", parent, err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, filename), []byte("sibling"), 0o644); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	if _, err := wt.Add(filename); err != nil {
		t.Fatalf("Add: %v", err)
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	h, err := wt.Commit("sibling", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("Commit sibling: %v", err)
	}
	return h.String()
}

func TestLockMergeChoosesDescendant(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	a, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA A: %v", err)
	}
	if out, err := execCerne(t, "feature", "new", "Descendant", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	b, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA B: %v", err)
	}
	if a == b {
		t.Fatalf("backend did not advance")
	}

	// ours at the ancestor, theirs at the descendant → resolve to theirs.
	base := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	ours := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	theirs := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: b})
	if out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours, theirs); err != nil {
		t.Fatalf("lock merge: %v\n%s", err, out)
	}
	if got := readLockAt(t, ours).Snapshot; got != b {
		t.Fatalf("resolved snapshot = %s, want descendant %s", got, b)
	}

	// Symmetric: ours at the descendant, theirs at the ancestor → keep ours.
	ours2 := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: b})
	theirs2 := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	if out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours2, theirs2); err != nil {
		t.Fatalf("lock merge symmetric: %v\n%s", err, out)
	}
	if got := readLockAt(t, ours2).Snapshot; got != b {
		t.Fatalf("symmetric resolved snapshot = %s, want descendant %s", got, b)
	}
}

func TestLockMergeBackendMismatch(t *testing.T) {
	projDir, origin, _ := setupProject(t)
	base := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: "a"})
	ours := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: "a"})
	theirs := writeTempLock(t, project.Lock{Version: 1, Backend: "git@other:repo.git", Snapshot: "b"})
	out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours, theirs)
	if err == nil {
		t.Fatalf("lock merge succeeded on backend mismatch, want error\n%s", out)
	}
	if got := readLockAt(t, ours).Snapshot; got != "a" {
		t.Fatalf("ours snapshot = %s, want untouched 'a' (driver must not overwrite on a real conflict)", got)
	}
}

func TestLockMergeDivergent(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	a, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA A: %v", err)
	}
	if out, err := execCerne(t, "feature", "new", "Branch B", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	b, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA B: %v", err)
	}
	c := commitSiblingOf(t, repoDir, a, "sibling.txt") // sibling of b: neither is ancestor

	base := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	ours := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: b})
	theirs := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: c})
	out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours, theirs)
	if err == nil {
		t.Fatalf("lock merge succeeded on divergent snapshots, want error\n%s", out)
	}
	if got := readLockAt(t, ours).Snapshot; got != b {
		t.Fatalf("ours snapshot = %s, want untouched %s", got, b)
	}
}

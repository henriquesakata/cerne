package gitback

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var shaHex = regexp.MustCompile(`^[0-9a-f]{40}$`)

// fixture creates a bare origin plus one clone with a single pushed file.
// Returns origin path, clone path, and the SHA of the initial commit.
func fixture(t *testing.T) (origin, clone1, sha string) {
	t.Helper()
	origin = filepath.Join(t.TempDir(), "origin.git")
	if err := InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	clone1 = filepath.Join(t.TempDir(), "clone1")
	if err := Clone(origin, clone1); err != nil {
		t.Fatalf("Clone empty: %v", err)
	}
	writeFile(t, clone1, "hello.txt", "hello")
	sha, err := CommitAndPush(clone1, "initial")
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	return origin, clone1, sha
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestCommitAndPushOnEmptyRemote(t *testing.T) {
	_, clone1, sha := fixture(t)
	if !shaHex.MatchString(sha) {
		t.Fatalf("CommitAndPush sha = %q, want 40-char hex", sha)
	}
	head, err := HeadSHA(clone1)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != sha {
		t.Fatalf("HeadSHA = %q, want %q", head, sha)
	}
}

func TestSecondCloneSeesPushedFile(t *testing.T) {
	origin, _, sha := fixture(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(clone2, "hello.txt"))
	if err != nil {
		t.Fatalf("read hello.txt in clone2: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("hello.txt = %q, want %q", data, "hello")
	}
	head, err := HeadSHA(clone2)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != sha {
		t.Fatalf("clone2 HeadSHA = %q, want %q", head, sha)
	}
}

func TestPullBringsExternalChange(t *testing.T) {
	origin, clone1, _ := fixture(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	writeFile(t, clone2, "external.txt", "from clone2")
	sha2, err := CommitAndPush(clone2, "external change")
	if err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	if err := Pull(clone1); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(clone1, "external.txt"))
	if err != nil {
		t.Fatalf("read external.txt after pull: %v", err)
	}
	if string(data) != "from clone2" {
		t.Fatalf("external.txt = %q, want %q", data, "from clone2")
	}
	head, err := HeadSHA(clone1)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != sha2 {
		t.Fatalf("clone1 HeadSHA after pull = %q, want %q", head, sha2)
	}
}

func TestPullUpToDate(t *testing.T) {
	_, clone1, _ := fixture(t)
	if err := Pull(clone1); err != nil {
		t.Fatalf("Pull up-to-date: %v, want nil", err)
	}
}

func TestCommitAndPushNothingToCommit(t *testing.T) {
	_, clone1, _ := fixture(t)
	_, err := CommitAndPush(clone1, "noop")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("CommitAndPush err = %v, want ErrNothingToCommit", err)
	}
}

func TestDeletionPropagates(t *testing.T) {
	origin, clone1, _ := fixture(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if err := os.Remove(filepath.Join(clone1, "hello.txt")); err != nil {
		t.Fatalf("remove hello.txt: %v", err)
	}
	if _, err := CommitAndPush(clone1, "delete hello"); err != nil {
		t.Fatalf("CommitAndPush deletion: %v", err)
	}

	if err := Pull(clone2); err != nil {
		t.Fatalf("Pull clone2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clone2, "hello.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hello.txt in clone2 after pull: err = %v, want ErrNotExist", err)
	}
}

func TestRemoteHeadSHA(t *testing.T) {
	origin, clone1, sha := fixture(t)
	got, err := RemoteHeadSHA(clone1)
	if err != nil {
		t.Fatalf("RemoteHeadSHA: %v", err)
	}
	if got != sha {
		t.Fatalf("RemoteHeadSHA = %q, want %q", got, sha)
	}

	// A second clone pushes: RemoteHeadSHA sees the new remote head while
	// the local HEAD of clone1 stays behind (no fetch/pull side effect).
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	writeFile(t, clone2, "other.txt", "from clone2")
	sha2, err := CommitAndPush(clone2, "external change")
	if err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}
	got, err = RemoteHeadSHA(clone1)
	if err != nil {
		t.Fatalf("RemoteHeadSHA after external push: %v", err)
	}
	if got != sha2 {
		t.Fatalf("RemoteHeadSHA = %q, want remote head %q", got, sha2)
	}
	localHead, err := HeadSHA(clone1)
	if err != nil {
		t.Fatalf("HeadSHA clone1: %v", err)
	}
	if localHead != sha {
		t.Fatalf("clone1 HEAD = %q, want unchanged %q (RemoteHeadSHA must not pull)", localHead, sha)
	}
}

func TestRemoteHeadSHAEmptyRemote(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	workdir := filepath.Join(t.TempDir(), "clone")
	if err := Clone(origin, workdir); err != nil {
		t.Fatalf("Clone empty: %v", err)
	}
	_, err := RemoteHeadSHA(workdir)
	if !errors.Is(err, ErrNoCommits) {
		t.Fatalf("RemoteHeadSHA err = %v, want ErrNoCommits", err)
	}
}

func TestHeadSHAUnborn(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	workdir := filepath.Join(t.TempDir(), "clone")
	if err := Clone(origin, workdir); err != nil {
		t.Fatalf("Clone empty: %v", err)
	}
	_, err := HeadSHA(workdir)
	if !errors.Is(err, ErrNoCommits) {
		t.Fatalf("HeadSHA err = %v, want ErrNoCommits", err)
	}
}

// commitFileAt stages name=content and commits it with an explicit committer
// time. Git commit times have one-second resolution and no sub-second part, so
// distinct backdated times (not sleeps) make time-ordering assertions
// deterministic.
func commitFileAt(t *testing.T, workdir, name, content string, when time.Time) {
	t.Helper()
	writeFile(t, workdir, name, content)
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("Add %s: %v", name, err)
	}
	sig := &object.Signature{Name: "cerne", Email: "cerne@local", When: when}
	if _, err := wt.Commit("test "+name, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("Commit %s: %v", name, err)
	}
}

// LastCommitTime resolves per file: a.txt keeps its own (older) commit time
// even after a later commit moves HEAD by touching b.txt — proving it does not
// just report the HEAD commit's time.
func TestLastCommitTimePerFile(t *testing.T) {
	_, clone1, _ := fixture(t)
	jan := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	commitFileAt(t, clone1, "a.txt", "a", jan)
	commitFileAt(t, clone1, "b.txt", "b", feb)

	gotA, err := LastCommitTime(clone1, "a.txt")
	if err != nil {
		t.Fatalf("LastCommitTime(a.txt): %v", err)
	}
	if gotA.Unix() != jan.Unix() {
		t.Fatalf("a.txt time = %v, want %v (per file, not HEAD)", gotA.UTC(), jan)
	}
	gotB, err := LastCommitTime(clone1, "b.txt")
	if err != nil {
		t.Fatalf("LastCommitTime(b.txt): %v", err)
	}
	if gotB.Unix() != feb.Unix() {
		t.Fatalf("b.txt time = %v, want %v", gotB.UTC(), feb)
	}
}

func TestLastCommitTimeUnknownPath(t *testing.T) {
	_, clone1, _ := fixture(t)
	_, err := LastCommitTime(clone1, "never-committed.txt")
	if !errors.Is(err, ErrNoCommits) {
		t.Fatalf("LastCommitTime(unknown) err = %v, want ErrNoCommits", err)
	}
}

func TestLastCommitTimeUnborn(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	workdir := filepath.Join(t.TempDir(), "clone")
	if err := Clone(origin, workdir); err != nil {
		t.Fatalf("Clone empty: %v", err)
	}
	_, err := LastCommitTime(workdir, "anything.txt")
	if !errors.Is(err, ErrNoCommits) {
		t.Fatalf("LastCommitTime on unborn HEAD err = %v, want ErrNoCommits", err)
	}
}

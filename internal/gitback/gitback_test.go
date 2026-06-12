package gitback

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
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

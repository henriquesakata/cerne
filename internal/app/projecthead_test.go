package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

func TestRecordProjectHeadWritesSidecar(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, _ := repo.Worktree()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("a.txt"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
	want, _ := gitback.HeadSHA(dir)

	if err := RecordProjectHead(dir); err != nil {
		t.Fatalf("RecordProjectHead: %v", err)
	}
	got, err := project.LoadProjectHead(dir)
	if err != nil {
		t.Fatalf("LoadProjectHead: %v", err)
	}
	if got != want {
		t.Fatalf("project_head = %q, want %q", got, want)
	}
}

func TestRecordProjectHeadNonRepoIsInert(t *testing.T) {
	dir := t.TempDir() // not a git repo
	if err := RecordProjectHead(dir); err != nil {
		t.Fatalf("RecordProjectHead on non-repo should be nil, got %v", err)
	}
	if _, err := project.LoadProjectHead(dir); err == nil {
		t.Fatalf("expected no project_head sidecar on non-repo")
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// When the cached backend clone is gone (a reboot cleared it, a fresh machine,
// a manually deleted cache) any command must recreate it from the backend in
// .cernerc — not dead-end the user into 'cerne init', which refuses on an
// already-initialized project.
func TestResolvePathsRecreatesMissingCache(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Login social", Module: "identity", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	// Simulate the lost cache.
	if err := os.RemoveAll(repoDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status after cache loss should recover, got: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001") {
		t.Fatalf("status did not recover the feature after cache recreate:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Fatalf("cache clone was not recreated: %v", err)
	}
}

// TestResolveProjectDirtyCacheCloneDoesNotPoison pins the N-0027 poison at the
// resolver every command starts from: refreshing the shared cache clone while
// it holds an uncommitted write (a synthesis in flight in another process) must
// neither fail the command nor use raw Pull — which moves refs/heads/main and
// then aborts, leaving ref and files diverged so the next sweep commit reverts
// the remote advance. The refresh is skipped and the command runs on the
// cached state.
func TestResolveProjectDirtyCacheCloneDoesNotPoison(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)

	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Login social", Module: "identity", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	notes := filepath.Join(repoDir, "notes.md")
	if err := os.WriteFile(notes, []byte("notes v1\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	// A teammate advances the remote.
	mate := filepath.Join(t.TempDir(), "mate")
	if err := gitback.Clone(origin, mate); err != nil {
		t.Fatalf("Clone mate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mate, "wiki.md"), []byte("wiki v2\n"), 0o644); err != nil {
		t.Fatalf("write mate wiki: %v", err)
	}
	if _, err := gitback.CommitAndPush(mate, "remote update"); err != nil {
		t.Fatalf("CommitAndPush mate: %v", err)
	}

	// Poison window: an uncommitted write in the shared clone.
	if err := os.WriteFile(notes, []byte("notes: synthesis in flight\n"), 0o644); err != nil {
		t.Fatalf("poison notes: %v", err)
	}
	baseline, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA baseline: %v", err)
	}

	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status over a dirty cache clone must run on the cached state, got: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001") {
		t.Fatalf("status output missing the cached feature:\n%s", out)
	}

	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA after status: %v", err)
	}
	if head != baseline {
		t.Fatalf("resolveProject moved HEAD %s -> %s while the worktree was uncommitted: ref diverged from files", baseline[:8], head[:8])
	}
	if got, _ := os.ReadFile(notes); string(got) != "notes: synthesis in flight\n" {
		t.Fatalf("resolveProject clobbered the uncommitted write: notes.md = %q", got)
	}
}

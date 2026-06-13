package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
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

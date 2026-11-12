package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestPushRemintsAutoWorkTakenUpstream (F-0349 crit. 1+2): another session
// publishes W-0001 after our pull; the plan.md push mints W-0001 on the stale
// snapshot, loses the race, redoes it on the new tip and reports the new id.
func TestPushRemintsAutoWorkTakenUpstream(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := gitback.Clone(origin, other); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveWork(other, store.Work{ID: "W-0001", Title: "theirs", State: domain.WorkActive, Actor: "outro@host", Refs: []string{id}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if _, err := gitback.CommitAndPush(other, "theirs W-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	planPath := filepath.Join(projDir, ".docs", "current", "features", "F-0001-feature-a", "plan.md")
	mustWrite(t, planPath, "---\ncerne_id: F-0001\n---\n\n# Plano\n\nPasso 1: implementar.\n")

	_, stderr, err := execCerneSplit(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push: %v\nstderr: %s", err, stderr)
	}
	if want := "cerne: auto-work id W-0001 was taken upstream; registered as W-0002\n"; !strings.Contains(stderr, want) {
		t.Fatalf("stderr missing %q:\n%s", want, stderr)
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone verify: %v", err)
	}
	works, err := store.LoadAllWorks(verify)
	if err != nil || len(works) != 2 {
		t.Fatalf("works = %+v err=%v, want W-0001 (theirs) + W-0002", works, err)
	}
}

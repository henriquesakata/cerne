package app

import (
	"errors"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// mintWork is a mutate that mints the next work id on whatever tree it runs on.
func mintWork(repoDir string, onMint func(id string)) func() (string, error) {
	return func() (string, error) {
		id, err := store.NextID(repoDir, "W")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		w := store.Work{ID: id, Title: "mine", State: domain.WorkActive, Actor: "me@host", CreatedAt: now, UpdatedAt: now}
		if err := store.SaveWork(repoDir, w); err != nil {
			return "", err
		}
		if onMint != nil {
			onMint(id)
		}
		return "mint " + id, nil
	}
}

// F-0356 crit. 1: the id taken upstream is minted again on the new tip.
func TestMutateAndSnapshotRemintsTakenID(t *testing.T) {
	origin, repoDir, projDir, _ := remintFixture(t)
	remoteWork(t, origin, "W-0001", "other@host")
	if err := MutateAndSnapshot(repoDir, projDir, mintWork(repoDir, nil)); err != nil {
		t.Fatalf("MutateAndSnapshot: %v", err)
	}
	if _, ok := originHas(t, origin, "entities/works/W-0002.json"); !ok {
		t.Fatal("W-0002 not pushed")
	}
}

// F-0356 crit. 4: a conflict on a path the mutate did not create aborts with a
// ConflictError, nothing sent.
func TestMutateAndSnapshotConflictOnExistingDocAborts(t *testing.T) {
	origin, repoDir, projDir, _ := remintFixture(t)
	advanceRemote(t, origin, remintPlan, "edição remota\n")
	theirHead, _ := gitback.RemoteHeadSHA(repoDir)
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		writeBackendFile(t, repoDir, remintPlan, "edição local\n")
		return "edit plan", nil
	})
	var ce *ConflictError
	if !errors.As(err, &ce) || !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want *ConflictError", err)
	}
	if rh, _ := gitback.RemoteHeadSHA(repoDir); rh != theirHead {
		t.Fatal("remote head moved: something was sent")
	}
}

// F-0356 crit. 5: a third session takes every id this one mints; after 3
// re-mints it gives up, nothing sent, the clone back at base.
func TestMutateAndSnapshotGivesUpAfterThreeRemints(t *testing.T) {
	origin, repoDir, projDir, base := remintFixture(t)
	remoteWork(t, origin, "W-0001", "other@host")
	theirHead, _ := gitback.RemoteHeadSHA(repoDir)
	calls := 0
	err := MutateAndSnapshot(repoDir, projDir, mintWork(repoDir, func(id string) {
		calls++
		remoteWork(t, origin, id, "third@host")
		theirHead, _ = gitback.RemoteHeadSHA(repoDir)
	}))
	if !errors.Is(err, ErrIDRaceLost) {
		t.Fatalf("err = %v, want ErrIDRaceLost", err)
	}
	if err.Error() != "id race lost 3 times; run 'cerne pull' and retry" {
		t.Fatalf("message = %q", err)
	}
	if calls != 4 {
		t.Fatalf("mutate ran %d times, want 4 (1 + 3 re-mints)", calls)
	}
	if rh, _ := gitback.RemoteHeadSHA(repoDir); rh != theirHead {
		t.Fatal("remote head moved: we sent something")
	}
	if head, _ := gitback.HeadSHA(repoDir); head != base {
		t.Fatalf("clone head = %s, want base %s", head, base)
	}
}

// F-0356 crit. 6: the conflict message is English.
func TestConflictErrorMessageIsEnglish(t *testing.T) {
	e := &ConflictError{Conflicts: []Conflict{{Path: "a.md"}, {Path: "b.md"}}}
	if want := "conflict in 2 document(s): a.md, b.md"; e.Error() != want {
		t.Fatalf("Error() = %q, want %q", e.Error(), want)
	}
}

package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

const remintPlan = "content/features/F-0001/plan.md"

// remintFixture is reconcileFixture with an approved F-0001 (with a plan)
// already pushed, so base is the snapshot the pusher pulled.
func remintFixture(t *testing.T) (origin, repoDir, projDir, base string) {
	t.Helper()
	origin, repoDir, projDir, _ = reconcileFixture(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "plan.md")
	base, err := gitback.RemoteHeadSHA(repoDir)
	if err != nil {
		t.Fatalf("RemoteHeadSHA: %v", err)
	}
	return origin, repoDir, projDir, base
}

// remoteWork publishes an active work from ANOTHER clone, after the pusher's
// snapshot — the id race of P-0020.
func remoteWork(t *testing.T, origin, id, actor string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	if err := gitback.Clone(origin, other); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	now := time.Now().UTC()
	w := store.Work{ID: id, Title: "theirs", State: domain.WorkActive, Actor: actor, Refs: []string{"F-0001"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWork(other, w); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if _, err := gitback.CommitAndPush(other, "theirs "+id); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func remintPush(repoDir, projDir, base string, mutate func() error) (string, []Conflict, map[string]string, error) {
	changes := []FileChange{{Path: remintPlan, Content: []byte("# plano novo\n")}}
	if mutate == nil {
		mutate = func() error { return OpenAutoWorkForPush(repoDir, projDir, changes) }
	}
	return ReconcileCommitPushMutate(repoDir, projDir, base, "push plan", changes, mutate)
}

// F-0349 crit. 1+2: the id minted on the stale snapshot was taken upstream, so
// the mutate is redone on the new tip and the push lands with the next free id.
func TestReconcileRemintsAutoWorkOnNewTip(t *testing.T) {
	origin, repoDir, projDir, base := remintFixture(t)
	remoteWork(t, origin, "W-0001", "other@host")

	head, conflicts, remints, err := remintPush(repoDir, projDir, base, nil)
	if err != nil || len(conflicts) != 0 || head == "" {
		t.Fatalf("head=%q conflicts=%+v err=%v, want clean push", head, conflicts, err)
	}
	if len(remints) != 1 || remints["W-0001"] != "W-0002" {
		t.Fatalf("remints = %v, want W-0001 -> W-0002", remints)
	}
	for _, p := range []string{"entities/works/W-0001.json", "entities/works/W-0002.json", remintPlan} {
		if _, ok := originHas(t, origin, p); !ok {
			t.Fatalf("origin lacks %s", p)
		}
	}
}

// F-0349 crit. 3: a document the caller changed is a real conflict: no re-mint,
// nothing sent.
func TestReconcileNoRemintWhenCallerDocConflicts(t *testing.T) {
	origin, repoDir, projDir, base := remintFixture(t)
	remoteWork(t, origin, "W-0001", "other@host")
	theirHead := advanceRemote(t, origin, remintPlan, "plano deles\n")

	head, conflicts, remints, err := remintPush(repoDir, projDir, base, nil)
	if err != nil || head != "" || len(remints) != 0 {
		t.Fatalf("head=%q remints=%v err=%v, want abort", head, remints, err)
	}
	found := false
	for _, c := range conflicts {
		found = found || c.Path == remintPlan
	}
	if !found {
		t.Fatalf("conflicts = %+v, want plan.md listed", conflicts)
	}
	if rh, _ := gitback.RemoteHeadSHA(repoDir); rh != theirHead {
		t.Fatalf("remote head moved: %q != %q", rh, theirHead)
	}
}

// F-0349 crit. 4: the new tip already has this actor's active work on the
// feature, so the redone mutate dedups and mints nothing.
func TestReconcileRemintDedupsOnNewTip(t *testing.T) {
	origin, repoDir, projDir, base := remintFixture(t)
	remoteWork(t, origin, "W-0001", autoWorkActor())

	head, conflicts, remints, err := remintPush(repoDir, projDir, base, nil)
	if err != nil || len(conflicts) != 0 || head == "" {
		t.Fatalf("head=%q conflicts=%+v err=%v, want clean push", head, conflicts, err)
	}
	if len(remints) != 0 {
		t.Fatalf("remints = %v, want none", remints)
	}
	if _, ok := originHas(t, origin, "entities/works/W-0002.json"); ok {
		t.Fatal("a second work was minted despite the active one")
	}
}

// F-0349 crit. 5: a mutate that collides on every try is dropped after 3
// re-mints, with nothing sent.
func TestReconcileRemintGivesUpAfterThree(t *testing.T) {
	origin, repoDir, projDir, base := remintFixture(t)
	remoteWork(t, origin, "W-0001", "other@host")
	calls := 0
	always := func() error {
		calls++
		now := time.Now().UTC()
		return store.SaveWork(repoDir, store.Work{ID: "W-0001", Title: "x" + time.Now().String(), State: domain.WorkActive, Actor: "me", CreatedAt: now, UpdatedAt: now})
	}
	theirHead, _ := gitback.RemoteHeadSHA(repoDir)
	head, _, _, err := remintPush(repoDir, projDir, base, always)
	if !errors.Is(err, ErrAutoWorkRaceLost) || head != "" {
		t.Fatalf("head=%q err=%v, want ErrAutoWorkRaceLost", head, err)
	}
	if err.Error() != "auto-work id race lost 3 times; run 'cerne pull' and 'cerne push' again" {
		t.Fatalf("message = %q", err)
	}
	if calls != 4 {
		t.Fatalf("mutate ran %d times, want 4 (1 + 3 re-mints)", calls)
	}
	if rh, _ := gitback.RemoteHeadSHA(repoDir); rh != theirHead {
		t.Fatalf("remote head moved")
	}
	_ = origin
}

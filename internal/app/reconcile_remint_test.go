package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// A1: the shared clone's tree sits at the pushed head, not back at base.
	if ws, _ := store.LoadAllWorks(repoDir); len(ws) != 2 {
		t.Fatalf("clone has %d works, want 2 (tree at head)", len(ws))
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

	calls := 0
	changes := []FileChange{{Path: remintPlan, Content: []byte("# plano novo\n")}}
	head, conflicts, _, err := ReconcileCommitPushMutate(repoDir, projDir, base, "push plan", changes, func() error {
		calls++
		return OpenAutoWorkForPush(repoDir, projDir, changes)
	})
	if err != nil || head != "" {
		t.Fatalf("head=%q err=%v, want abort", head, err)
	}
	if calls != 1 {
		t.Fatalf("mutate ran %d times, want 1 (no re-mint)", calls)
	}
	got := map[string]bool{}
	for _, c := range conflicts {
		got[c.Path] = true
	}
	if len(got) != 2 || !got[remintPlan] || !got["entities/works/W-0001.json"] {
		t.Fatalf("conflicts = %+v, want plan.md and W-0001.json", conflicts)
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

// F-0349 crit. 5: a third session takes the next id after every mint, so the
// redone mutate collides on each round; it is dropped after 3 re-mints, with
// nothing sent.
func TestReconcileRemintGivesUpAfterThree(t *testing.T) {
	origin, repoDir, projDir, base := remintFixture(t)
	remoteWork(t, origin, "W-0001", "other@host")
	theirHead, _ := gitback.RemoteHeadSHA(repoDir)
	calls := 0
	changes := []FileChange{{Path: remintPlan, Content: []byte("# plano novo\n")}}
	mutate := func() error {
		calls++
		next, err := store.NextID(repoDir, "W")
		if err != nil {
			return err
		}
		if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
			return err
		}
		remoteWork(t, origin, next, "third@host") // the id just minted is taken upstream
		theirHead, _ = gitback.RemoteHeadSHA(repoDir)
		return nil
	}
	head, _, _, err := ReconcileCommitPushMutate(repoDir, projDir, base, "push plan", changes, mutate)
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
		t.Fatalf("remote head moved: we sent something")
	}
}

// F-0349 crit. 3: altering another session's existing work is not minting an
// id: the conflict is listed and the mutate is not redone.
func TestReconcileNoRemintWhenMutateAltersExistingWork(t *testing.T) {
	origin, repoDir, projDir, _ := remintFixture(t)
	seedActiveWork(t, repoDir, "W-0001", "other@host", "F-0001", false)
	base, _ := gitback.RemoteHeadSHA(repoDir)
	remoteWork(t, origin, "W-0001", "third@host") // remote rewrites it after base
	calls := 0
	mutate := func() error {
		calls++
		ws, err := store.LoadAllWorks(repoDir)
		if err != nil || len(ws) != 1 {
			return fmt.Errorf("works=%v err=%v", ws, err)
		}
		ws[0].Title = "mine"
		return store.SaveWork(repoDir, ws[0])
	}
	changes := []FileChange{{Path: remintPlan, Content: []byte("# plano novo\n")}}
	head, conflicts, _, err := ReconcileCommitPushMutate(repoDir, projDir, base, "push plan", changes, mutate)
	if err != nil || head != "" || calls != 1 {
		t.Fatalf("head=%q err=%v calls=%d, want abort after one run", head, err, calls)
	}
	if len(conflicts) != 1 || conflicts[0].Path != "entities/works/W-0001.json" {
		t.Fatalf("conflicts = %+v, want W-0001.json", conflicts)
	}
}

// F-0349: the redone mutate writes on the new tip, so what it rewrites there is
// reconciled against that tip, not against the stale base: a remote change
// base->tip to a file the mutate rewrites is not ours, and the event log keeps
// every line exactly once.
func TestReconcileRemintReconcilesFromTip(t *testing.T) {
	origin, repoDir, projDir, _ := remintFixture(t)
	writeBackendFile(t, repoDir, "content/notes/X.md", "v1\n")
	writeBackendFile(t, repoDir, "events/2026-11.ndjson", "{\"id\":\"a\"}\n")
	if _, err := gitback.CommitAndPush(repoDir, "seed x"); err != nil {
		t.Fatal(err)
	}
	base, _ := gitback.RemoteHeadSHA(repoDir)
	remoteWork(t, origin, "W-0001", "other@host")
	advanceRemote(t, origin, "content/notes/X.md", "v2\n")

	calls := 0
	changes := []FileChange{{Path: remintPlan, Content: []byte("# plano novo\n")}}
	mutate := func() error {
		calls++
		if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
			return err
		}
		line := fmt.Sprintf("{\"id\":\"m%d\"}\n", calls)
		if err := appendFile(repoDir, "events/2026-11.ndjson", line); err != nil {
			return err
		}
		if calls > 1 {
			return appendFile(repoDir, "content/notes/X.md", "mine\n")
		}
		return nil
	}
	head, conflicts, remints, err := ReconcileCommitPushMutate(repoDir, projDir, base, "push plan", changes, mutate)
	if err != nil || len(conflicts) != 0 || head == "" || len(remints) != 1 {
		t.Fatalf("head=%q conflicts=%+v remints=%v err=%v, want clean re-mint", head, conflicts, remints, err)
	}
	if got, _ := originHas(t, origin, "content/notes/X.md"); got != "v2\nmine\n" {
		t.Fatalf("X.md = %q, want v2+mine", got)
	}
	log, _ := originHas(t, origin, "events/2026-11.ndjson")
	for _, l := range strings.Split(strings.TrimSpace(log), "\n") {
		if strings.Count(log, l) != 1 {
			t.Fatalf("event line %s repeated:\n%s", l, log)
		}
	}
	if strings.Contains(log, "m1") {
		t.Fatalf("event of the discarded mint leaked:\n%s", log)
	}
}

func appendFile(repoDir, rel, text string) error {
	f, err := os.OpenFile(filepath.Join(repoDir, filepath.FromSlash(rel)), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}

// F-0349 crit. 2: a push that opens two works (two approved features) loses
// only the id of the SECOND feature's work (the remote takes W-0002), whose
// entity AND content both collide upstream; the report pairs the old id with
// the new one of the SAME feature, not with the one by position.
func TestReconcileRemintReportsPairByFeature(t *testing.T) {
	origin, repoDir, projDir, _ := remintFixture(t)
	seedFeatureInState(t, repoDir, "F-0002", domain.StateApproved, "plan.md")
	base, _ := gitback.RemoteHeadSHA(repoDir)
	// Remote takes W-0002 for F-0002 (another actor), plus its content.
	other := filepath.Join(t.TempDir(), "other")
	if err := gitback.Clone(origin, other); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveWork(other, store.Work{ID: "W-0002", Title: "theirs", State: domain.WorkActive, Actor: "other@host", Refs: []string{"F-0002"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeBackendFile(t, other, "content/works/W-0002/note.md", "theirs\n")
	if _, err := gitback.CommitAndPush(other, "theirs W-0002"); err != nil {
		t.Fatal(err)
	}

	plan2 := "content/features/F-0002/plan.md"
	changes := []FileChange{
		{Path: remintPlan, Content: []byte("# plano 1\n")},
		{Path: plan2, Content: []byte("# plano 2\n")},
	}
	mutate := func() error {
		if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
			return err
		}
		ws, err := store.LoadAllWorks(repoDir)
		if err != nil {
			return err
		}
		for _, w := range ws {
			if w.Auto {
				writeBackendFile(t, repoDir, "content/works/"+w.ID+"/note.md", "mine "+w.ID+"\n")
			}
		}
		return nil
	}
	head, conflicts, remints, err := ReconcileCommitPushMutate(repoDir, projDir, base, "push plans", changes, mutate)
	if err != nil || len(conflicts) != 0 || head == "" {
		t.Fatalf("head=%q conflicts=%+v err=%v, want clean push", head, conflicts, err)
	}
	// Round 0 minted W-0001 (F-0001) and W-0002 (F-0002); W-0002 was taken. On
	// the tip F-0001 -> W-0003 and F-0002 -> W-0004: the pair is F-0002's.
	if len(remints) != 1 || remints["W-0002"] != "W-0004" {
		t.Fatalf("remints = %v, want only W-0002 -> W-0004 (F-0002's new work)", remints)
	}
}

// F-0349: a caller doc byte-equal to base (a no-op push of it) must not revert
// the remote's edit of it when a re-mint round reconciles against the tip.
func TestReconcileRemintKeepsRemoteEditOfNoopDoc(t *testing.T) {
	origin, repoDir, projDir, base := remintFixture(t)
	baseBody, _ := originHas(t, origin, remintPlan)
	remoteWork(t, origin, "W-0001", "other@host")
	advanceRemote(t, origin, remintPlan, "edição remota\n")

	changes := []FileChange{{Path: remintPlan, Content: []byte(baseBody)}}
	mutate := func() error {
		return OpenAutoWorkForPush(repoDir, projDir, changes)
	}
	head, conflicts, remints, err := ReconcileCommitPushMutate(repoDir, projDir, base, "push plan", changes, mutate)
	if err != nil || len(conflicts) != 0 || head == "" || len(remints) != 1 {
		t.Fatalf("head=%q conflicts=%+v remints=%v err=%v, want clean re-mint", head, conflicts, remints, err)
	}
	if got, _ := originHas(t, origin, remintPlan); got != "edição remota\n" {
		t.Fatalf("plan.md = %q, remote edit was reverted", got)
	}
}

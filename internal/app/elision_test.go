package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// widenElisionWindow pins "the mark is fresh" by construction: on a loaded
// machine more than the production 30s can pass between the fixture's seed
// push (which stamps the mark) and reconcile's SyncedWithin check, flipping
// an elision test into a real fetch. Any window that outlives the test run
// keeps these tests about skip-vs-fetch behavior, not scheduling; the
// window's expiry semantics stay pinned in gitback's syncmark tests under a
// fake clock.
func widenElisionWindow(t *testing.T) {
	t.Helper()
	prev := elisionWindow
	elisionWindow = 24 * time.Hour
	t.Cleanup(func() { elisionWindow = prev })
}

func mutateSpec(t *testing.T, repoDir, projDir, content string) *telemetry.Op {
	t.Helper()
	op := telemetry.StartOp("http")
	err := MutateAndSnapshotOp(op, repoDir, projDir, func() (string, error) {
		writeBackendFile(t, repoDir, "content/features/F-0001/spec.md", content)
		return "update spec", nil
	})
	if err != nil {
		t.Fatalf("MutateAndSnapshotOp: %v", err)
	}
	op.End("ok")
	return op
}

// phaseSet reads the phases of THIS op's ring record (by id — the newest
// record can belong to a neighbouring test's autocommit worker on a loaded
// machine; see ringRecord).
func phaseSet(t *testing.T, op *telemetry.Op) map[string]int {
	t.Helper()
	rec := ringRecord(t, op.ID)
	out := map[string]int{}
	for _, p := range rec.Phases {
		out[p.Name]++
	}
	return out
}

// TestElisionSkipsFetchOnFreshSync pins criteria 1+5 (F-0156): with a fresh
// sync mark (the fixture's seed push), attempt 0 skips the fetch and records
// fetch_skip instead; the mutation lands on origin normally.
func TestElisionSkipsFetchOnFreshSync(t *testing.T) {
	widenElisionWindow(t)
	origin, repoDir, projDir, _ := reconcileFixture(t)
	// The fixture's CommitAndPush marked the sync; the mark is fresh.
	op := mutateSpec(t, repoDir, projDir, "v2\n")
	ph := phaseSet(t, op)
	if ph["fetch_skip"] != 1 || ph["fetch"] != 0 {
		t.Fatalf("phases = %v, want one fetch_skip and no fetch", ph)
	}
	if got, ok := originHas(t, origin, "content/features/F-0001/spec.md"); !ok || got != "v2\n" {
		t.Fatalf("origin content = %q, %v — mutation did not land", got, ok)
	}
}

// TestElisionConvergesWhenRemoteAdvanced pins criterion 2: eliding against a
// stale tracking head costs one non-fast-forward retry — which fetches — and
// the final origin state is the correct merge of both sides.
func TestElisionConvergesWhenRemoteAdvanced(t *testing.T) {
	widenElisionWindow(t)
	origin, repoDir, projDir, _ := reconcileFixture(t)
	// Remote advances behind our back (another clone; our mark stays fresh).
	advanceRemote(t, origin, "content/other.md", "deles\n")

	op := mutateSpec(t, repoDir, projDir, "v3\n")
	rec := ringRecord(t, op.ID)
	if rec.Retries != 1 {
		t.Fatalf("retries = %d, want exactly 1 (the elision's cost)", rec.Retries)
	}
	ph := phaseSet(t, op)
	if ph["fetch_skip"] != 1 || ph["fetch"] != 1 {
		t.Fatalf("phases = %v, want fetch_skip (attempt 0) + fetch (retry)", ph)
	}
	if got, ok := originHas(t, origin, "content/features/F-0001/spec.md"); !ok || got != "v3\n" {
		t.Fatalf("our change missing on origin: %q, %v", got, ok)
	}
	if got, ok := originHas(t, origin, "content/other.md"); !ok || got != "deles\n" {
		t.Fatalf("their change lost on origin: %q, %v", got, ok)
	}
}

// TestNoElisionWithoutMark pins criterion 3: no recent sync (a fresh process
// — every CLI one-shot) keeps today's fetch on attempt 0.
func TestNoElisionWithoutMark(t *testing.T) {
	_, repoDir, projDir, _ := reconcileFixture(t)
	gitback.ResetSyncMarks()
	op := mutateSpec(t, repoDir, projDir, "v4\n")
	ph := phaseSet(t, op)
	if ph["fetch"] != 1 || ph["fetch_skip"] != 0 {
		t.Fatalf("phases = %v, want a real fetch and no fetch_skip", ph)
	}
}

// TestElisionConfirmsHeadOnNothingToApply pins F-0285 criterion 1: the
// len(apply) == 0 exit never pushes, so the non-fast-forward retry cannot
// correct a plan made against an elided (stale) tracking head. With the mark
// fresh and the remote advanced, the snapshot must be the confirmed remote
// head — never the stale one.
func TestElisionConfirmsHeadOnNothingToApply(t *testing.T) {
	widenElisionWindow(t)
	origin, repoDir, projDir, base := reconcileFixture(t)
	theirHead := advanceRemote(t, origin, "content/other.md", "deles\n")

	if !gitback.SyncedWithin(repoDir, elisionWindow) {
		t.Fatal("no fresh sync mark: the elision under test never happened, so this test proves nothing")
	}
	// Byte-identical to base: plan3Way applies nothing.
	changes := []FileChange{{Path: "content/features/F-0001/spec.md", Content: []byte("base spec\n")}}
	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "no-op", changes)
	if err != nil {
		t.Fatalf("ReconcileCommitPush: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
	if head == base {
		t.Fatalf("head = base %q — the stale elided tracking head was written as the snapshot", base)
	}
	if head != theirHead {
		t.Fatalf("head = %q, want confirmed remote head %q", head, theirHead)
	}
	if snap := loadSnapshot(t, projDir); snap != theirHead {
		t.Fatalf("docs.lock snapshot = %q, want confirmed remote head %q", snap, theirHead)
	}
}

// TestElisionConfirmsHeadOnNothingToCommit pins F-0285 criterion 2: same
// hazard on the ErrNothingToCommit exit (merged content equals theirs
// byte-for-byte), which also returns without ever pushing.
func TestElisionConfirmsHeadOnNothingToCommit(t *testing.T) {
	widenElisionWindow(t)
	origin, repoDir, projDir, _ := reconcileFixture(t)
	// An append-only log always enters `apply`, so the exit reached is the
	// commit's ErrNothingToCommit rather than an empty plan.
	writeBackendFile(t, repoDir, "events/log.ndjson", "{\"id\":\"a\"}\n")
	base, err := gitback.CommitAndPush(repoDir, "seed events")
	if err != nil {
		t.Fatalf("seed events: %v", err)
	}
	theirHead := advanceRemote(t, origin, "content/other.md", "deles\n")

	if !gitback.SyncedWithin(repoDir, elisionWindow) {
		t.Fatal("no fresh sync mark: the elision under test never happened, so this test proves nothing")
	}
	changes := []FileChange{{Path: "events/log.ndjson", Content: []byte("{\"id\":\"a\"}\n")}}
	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "re-push same events", changes)
	if err != nil {
		t.Fatalf("ReconcileCommitPush: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
	if head == base {
		t.Fatalf("head = base %q — the stale elided tracking head was written as the snapshot", base)
	}
	if head != theirHead {
		t.Fatalf("head = %q, want confirmed remote head %q", head, theirHead)
	}
	if snap := loadSnapshot(t, projDir); snap != theirHead {
		t.Fatalf("docs.lock snapshot = %q, want confirmed remote head %q", snap, theirHead)
	}
}

// TestNoElisionSkipsConfirmation pins F-0285 criterion 3: with no fresh mark
// the plan already came from a real fetch, so the no-op exit must not pay a
// second round trip.
func TestNoElisionSkipsConfirmation(t *testing.T) {
	origin, repoDir, projDir, _ := reconcileFixture(t)
	// Our write is already present remotely: plan3Way applies nothing, the
	// exit F-0285 guards.
	theirHead := advanceRemote(t, origin, "content/features/F-0001/spec.md", "shared\n")
	gitback.ResetSyncMarks()
	op := mutateSpec(t, repoDir, projDir, "shared\n")
	ph := phaseSet(t, op)
	if ph["fetch"] != 1 || ph["fetch_skip"] != 0 || ph["fetch_confirm"] != 0 {
		t.Fatalf("phases = %v, want exactly one fetch and no elision/confirmation", ph)
	}
	// The exit reached is the guarded one, and its head is still recorded.
	if snap := loadSnapshot(t, projDir); snap != theirHead {
		t.Fatalf("docs.lock snapshot = %q, want the fetched remote head %q", snap, theirHead)
	}
}

// TestElisionConfirmsHeadOnConflict pins the third exit that returns without
// pushing (F-0285): a conflict is reported to the caller AND stashed as "the
// remote version". Planned against an elided tracking head that is stale AND
// already divergent, that is a conflict against content the remote no longer
// has — a false conflict with a bogus stash. Here a peer then converged the
// document to exactly our bytes, so the confirmed plan has nothing to report.
func TestElisionConfirmsHeadOnConflict(t *testing.T) {
	widenElisionWindow(t)
	origin, repoDir, projDir, base := reconcileFixture(t)
	// The remote diverges on OUR document, and we fetch it: the tracking ref
	// now holds a version that conflicts with ours, and the mark is fresh.
	advanceRemote(t, origin, "content/features/F-0001/spec.md", "deles\n")
	if err := gitback.Fetch(repoDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// Behind our back, a peer converges that document to what we are pushing.
	theirHead := advanceRemote(t, origin, "content/features/F-0001/spec.md", "mine\n")
	if !gitback.SyncedWithin(repoDir, elisionWindow) {
		t.Fatal("no fresh sync mark: the elision under test never happened, so this test proves nothing")
	}

	changes := []FileChange{{Path: "content/features/F-0001/spec.md", Content: []byte("mine\n")}}
	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "push spec", changes)
	if err != nil {
		t.Fatalf("ReconcileCommitPush: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("false conflict planned against a stale elided head: %+v", conflicts)
	}
	if head != theirHead {
		t.Fatalf("head = %q, want confirmed remote head %q", head, theirHead)
	}
	if snap := loadSnapshot(t, projDir); snap != theirHead {
		t.Fatalf("docs.lock snapshot = %q, want confirmed remote head %q", snap, theirHead)
	}
}

// TestUnconfirmedHeadBackstop pins the structural half of F-0285: whatever
// exit produces a head, recording it is refused unless this operation saw it
// on the remote. The exits that exist today all confirm — so the whole
// reconcile suite is this guard's negative case — and this pins the positive
// one, which only a future unconfirmed exit would reach.
func TestUnconfirmedHeadBackstop(t *testing.T) {
	for _, tc := range []struct {
		name      string
		head      string
		confirmed bool
		wantErr   bool
	}{
		{"confirmed head is recordable", "abc123", true, false},
		{"unconfirmed head is refused", "abc123", false, true},
		{"empty head records nothing", "", false, false},
		{"empty head, confirmed", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := unconfirmedHead(tc.head, tc.confirmed)
			if got := errors.Is(err, ErrUnconfirmedHead); got != tc.wantErr {
				t.Fatalf("errors.Is(%v, ErrUnconfirmedHead) = %v, want %v", err, got, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.head) {
				t.Fatalf("error %q does not name the offending head %q", err, tc.head)
			}
		})
	}
}

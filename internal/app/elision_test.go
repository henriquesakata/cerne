package app

import (
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

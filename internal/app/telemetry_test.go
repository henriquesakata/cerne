package app

import (
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// TestMutateAndSnapshotOpRecordsPhases pins criterion 2 (F-0149): a backend
// mutation records the full phase sequence on the caller's Op.
func TestMutateAndSnapshotOpRecordsPhases(t *testing.T) {
	_, repoDir, projDir, _ := reconcileFixture(t)
	// The fixture's seed push marks the sync (F-0156) and attempt 0 would
	// elide the fetch; this golden pins the FULL phase story, so forget it.
	gitback.ResetSyncMarks()
	op := telemetry.StartOp("http")
	err := MutateAndSnapshotOp(op, repoDir, projDir, func() (string, error) {
		writeBackendFile(t, repoDir, "content/features/F-0001/spec.md", "v2\n")
		return "update spec", nil
	})
	if err != nil {
		t.Fatalf("MutateAndSnapshotOp: %v", err)
	}
	op.End("ok")
	recs := telemetry.Snapshot()
	if len(recs) == 0 {
		t.Fatal("no record in ring")
	}
	rec := recs[0]
	if rec.ID != op.ID {
		t.Fatalf("newest ring record is %s, want %s", rec.ID, op.ID)
	}
	want := []string{"lock_wait", "reset", "mutate", "fetch", "merge", "commit", "push", "snapshot"}
	if len(rec.Phases) != len(want) {
		t.Fatalf("phases = %v, want %v", names(rec.Phases), want)
	}
	for i, w := range want {
		if rec.Phases[i].Name != w {
			t.Fatalf("phase[%d] = %s, want %s (all: %v)", i, rec.Phases[i].Name, w, names(rec.Phases))
		}
	}
}

// TestReconcileRetryCountsOnOp pins the retry accounting: a remote advance
// between fetch and push forces one non-fast-forward retry, visible on the Op.
func TestReconcileRetryCountsOnOp(t *testing.T) {
	origin, repoDir, projDir, _ := reconcileFixture(t)
	raced := false
	reconcileHookBeforePush = func(attempt int) {
		if attempt == 0 && !raced {
			raced = true
			advanceRemote(t, origin, "content/other.md", "concurrent\n")
		}
	}
	defer func() { reconcileHookBeforePush = nil }()

	op := telemetry.StartOp("http")
	err := MutateAndSnapshotOp(op, repoDir, projDir, func() (string, error) {
		writeBackendFile(t, repoDir, "content/features/F-0001/spec.md", "v3\n")
		return "update spec", nil
	})
	if err != nil {
		t.Fatalf("MutateAndSnapshotOp: %v", err)
	}
	op.End("ok")
	rec := telemetry.Snapshot()[0]
	if rec.Retries != 1 {
		t.Fatalf("retries = %d, want 1", rec.Retries)
	}
	pushes := 0
	for _, p := range rec.Phases {
		if p.Name == "push" {
			pushes++
		}
	}
	if pushes != 2 {
		t.Fatalf("push phases = %d, want 2 (failed attempt + landed retry)", pushes)
	}
}

// TestMutateAndSnapshotStillWorksWithoutOp pins the nil-op path: the classic
// signature keeps working and mints its own record.
func TestMutateAndSnapshotStillWorksWithoutOp(t *testing.T) {
	_, repoDir, projDir, _ := reconcileFixture(t)
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		writeBackendFile(t, repoDir, "content/features/F-0001/spec.md", "v4\n")
		return "update spec", nil
	})
	if err != nil {
		t.Fatalf("MutateAndSnapshot: %v", err)
	}
	rec := telemetry.Snapshot()[0]
	if rec.Kind != "backend" {
		t.Fatalf("kind = %s, want backend", rec.Kind)
	}
	if len(rec.Phases) == 0 {
		t.Fatal("classic path recorded no phases")
	}
}

func names(ps []telemetry.PhaseRecord) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

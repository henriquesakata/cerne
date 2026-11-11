package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// ringRecord finds the ring record for a specific op id. The ring is global
// to the process and the autocommit worker of a NEIGHBOURING test can land
// its own record after ours (seen on the loaded CI runner), so "newest"
// (Snapshot()[0]) is a race — identity is not.
func ringRecord(t *testing.T, opID string) telemetry.Record {
	t.Helper()
	for _, r := range telemetry.Snapshot() {
		if r.ID == opID {
			return r
		}
	}
	t.Fatalf("op %s not found in the telemetry ring", opID)
	return telemetry.Record{}
}

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
	rec := ringRecord(t, op.ID)
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
	rec := ringRecord(t, op.ID)
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
// signature keeps working and mints its own record. The op id is internal
// here, so identify OUR record as the one that appeared during the call and
// carries a mutate phase (autocommit-worker records never do).
func TestMutateAndSnapshotStillWorksWithoutOp(t *testing.T) {
	_, repoDir, projDir, _ := reconcileFixture(t)
	before := map[string]bool{}
	for _, r := range telemetry.Snapshot() {
		before[r.ID] = true
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		writeBackendFile(t, repoDir, "content/features/F-0001/spec.md", "v4\n")
		return "update spec", nil
	})
	if err != nil {
		t.Fatalf("MutateAndSnapshot: %v", err)
	}
	var rec telemetry.Record
	found := false
	for _, r := range telemetry.Snapshot() {
		if before[r.ID] {
			continue
		}
		for _, p := range r.Phases {
			if p.Name == "mutate" {
				rec, found = r, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no new ring record with a mutate phase — classic path recorded nothing")
	}
	if rec.Kind != "backend" {
		t.Fatalf("kind = %s, want backend", rec.Kind)
	}
}

func names(ps []telemetry.PhaseRecord) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

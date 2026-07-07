package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestOpPhasesOrderedAndTimed(t *testing.T) {
	op := StartOp("transition")
	if op.ID == "" {
		t.Fatal("op must mint a non-empty id")
	}
	stop := op.Phase("fetch")
	time.Sleep(time.Millisecond)
	stop()
	stop = op.Phase("push")
	time.Sleep(time.Millisecond)
	stop()
	rec := op.snapshot()
	if len(rec.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(rec.Phases))
	}
	if rec.Phases[0].Name != "fetch" || rec.Phases[1].Name != "push" {
		t.Fatalf("phase order = %q,%q, want fetch,push", rec.Phases[0].Name, rec.Phases[1].Name)
	}
	for _, p := range rec.Phases {
		if p.Dur <= 0 {
			t.Fatalf("phase %s duration must be > 0, got %v", p.Name, p.Dur)
		}
	}
}

func TestOpRetries(t *testing.T) {
	op := StartOp("transition")
	op.AddRetry()
	op.AddRetry()
	if got := op.snapshot().Retries; got != 2 {
		t.Fatalf("retries = %d, want 2", got)
	}
}

func TestRingEvictsOldest(t *testing.T) {
	r := newRing(3)
	for i := 0; i < 4; i++ {
		r.add(Record{ID: fmt.Sprintf("op-%d", i)})
	}
	snap := r.snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snap))
	}
	// Newest first; op-0 evicted.
	if snap[0].ID != "op-3" || snap[2].ID != "op-1" {
		t.Fatalf("snapshot order = %s..%s, want op-3..op-1", snap[0].ID, snap[2].ID)
	}
}

func TestEndLogsJSONWithOpIDAndPhases(t *testing.T) {
	var buf bytes.Buffer
	prev := SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer SetLogger(prev)

	op := StartOp("transition")
	op.Phase("push")()
	op.End("ok")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, buf.String())
	}
	if line["op_id"] != op.ID {
		t.Fatalf("op_id = %v, want %s", line["op_id"], op.ID)
	}
	if line["status"] != "ok" {
		t.Fatalf("status = %v, want ok", line["status"])
	}
	phases, ok := line["phases"].([]any)
	if !ok || len(phases) != 1 {
		t.Fatalf("phases = %v, want 1-element array", line["phases"])
	}
	first, _ := phases[0].(map[string]any)
	if first["name"] != "push" {
		t.Fatalf("phase name = %v, want push", first["name"])
	}
}

func TestEndFeedsDefaultRing(t *testing.T) {
	op := StartOp("lint")
	op.End("ok")
	snap := DefaultRing.snapshot()
	if len(snap) == 0 || snap[0].ID != op.ID {
		t.Fatalf("default ring newest = %+v, want op %s", snap, op.ID)
	}
}

func TestNilOpIsSafe(t *testing.T) {
	var op *Op
	op.Phase("fetch")() // must not panic
	op.AddRetry()
	op.End("ok")
}

package materialize

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0339 crit. 10: an active work with a handoff ends its line with the
// handoff date and how to read it; one without keeps the old line.
func TestContextWorkLineHandoffSuffix(t *testing.T) {
	backend := seedBackend(t)
	started := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	addWork(t, backend, "W-0001", "Com passagem", "ator", domain.WorkActive, started)
	addWork(t, backend, "W-0002", "Sem passagem", "ator", domain.WorkActive, started)
	at := started.Add(48 * time.Hour)
	w, _ := store.LoadWork(backend, "W-0001")
	w.HandoffAt = &at
	if err := store.SaveWork(backend, w); err != nil {
		t.Fatal(err)
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := MaterializeAt(backend, docsDir, "", false, 14, started.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ctx := readContext(t, docsDir)
	if want := "W-0001 — Com passagem (ator, since 2026-07-01) — handoff 2026-07-03: cerne show W-0001\n"; !strings.Contains(ctx, want) {
		t.Fatalf("missing %q:\n%s", want, ctx)
	}
	if want := "W-0002 — Sem passagem (ator, since 2026-07-01)\n"; !strings.Contains(ctx, want) {
		t.Fatalf("missing %q:\n%s", want, ctx)
	}
}

// F-0368 crit. 10: pending handoffs get their own block (at most 10 lines, the
// `handoff list` format, oldest first) and the W- line names its pending HO.
func TestContextPendingHandoffs(t *testing.T) {
	backend := seedBackend(t)
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	addWork(t, backend, "W-0001", "Migração", "ator", domain.WorkActive, now.Add(-5*time.Hour))
	for i := 1; i <= 12; i++ {
		at := now.Add(-time.Duration(13-i) * time.Hour)
		h := store.Handoff{ID: fmt.Sprintf("HO-%04d", i), State: domain.HandoffSuperseded, FromWork: "W-0001", Actor: "agent:a@b", CreatedAt: at, UpdatedAt: at}
		if i >= 2 && i != 12 { // HO-0002..HO-0011 pending (another work each, to stay honest about "one per work")
			h.State, h.FromWork = domain.HandoffPending, fmt.Sprintf("W-%04d", 100+i)
		}
		if i == 12 {
			h.State = domain.HandoffPending
		}
		if err := store.SaveHandoff(backend, h); err != nil {
			t.Fatal(err)
		}
	}
	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := MaterializeAt(backend, docsDir, "", false, 14, now); err != nil {
		t.Fatal(err)
	}
	ctx := readContext(t, docsDir)
	if want := "- W-0001 — Migração (ator, since 2026-11-12) — handoff HO-0012 pending\n"; !strings.Contains(ctx, want) {
		t.Fatalf("missing %q:\n%s", want, ctx)
	}
	if want := "## Pending handoffs\n\n- HO-0002 pending 11h (since 2026-11-12 01:00 UTC) — W-0102 — from agent:a@b\n"; !strings.Contains(ctx, want) {
		t.Fatalf("missing %q:\n%s", want, ctx)
	}
	if strings.Contains(ctx, "HO-0001") || strings.Contains(ctx, "HO-0012 pending 1h") {
		t.Fatalf("superseded or 11th pending listed:\n%s", ctx)
	}
	if want := "- HO-0011 pending 2h"; !strings.Contains(ctx, want) {
		t.Fatalf("10th pending missing:\n%s", ctx)
	}
	if want := "- 1 more: cerne handoff list\n"; !strings.Contains(ctx, want) {
		t.Fatalf("missing %q:\n%s", want, ctx)
	}
	empty := seedBackend(t)
	if _, err := MaterializeAt(empty, docsDir, "", false, 14, now); err != nil {
		t.Fatal(err)
	}
	if ctx := readContext(t, docsDir); strings.Contains(ctx, "Pending handoffs") {
		t.Fatalf("block without pending handoffs:\n%s", ctx)
	}
}

// F-0368 critic round 1, item 3: after a cancel the W- line carries no handoff
// suffix — the F-0339 fallback is only for a work with no HO- at all.
func TestContextCancelledHandoffNoSuffix(t *testing.T) {
	backend := seedBackend(t)
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	addWork(t, backend, "W-0001", "Cancelada", "ator", domain.WorkActive, now.Add(-5*time.Hour))
	w, _ := store.LoadWork(backend, "W-0001")
	at := now.Add(-time.Hour)
	w.HandoffAt = &at
	if err := store.SaveWork(backend, w); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveHandoff(backend, store.Handoff{ID: "HO-0001", State: domain.HandoffCancelled, FromWork: "W-0001", CancelReason: "x", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := MaterializeAt(backend, docsDir, "", false, 14, now); err != nil {
		t.Fatal(err)
	}
	if want := "- W-0001 — Cancelada (ator, since 2026-11-12)\n"; !strings.Contains(readContext(t, docsDir), want) {
		t.Fatalf("missing %q:\n%s", want, readContext(t, docsDir))
	}
}

package materialize

import (
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

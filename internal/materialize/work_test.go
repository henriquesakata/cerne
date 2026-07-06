package materialize

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func addWork(t *testing.T, backendDir, id, title, actor string, state domain.WorkState, createdAt time.Time) {
	t.Helper()
	w := store.Work{ID: id, Title: title, State: state, Actor: actor, CreatedAt: createdAt, UpdatedAt: createdAt}
	if err := store.SaveWork(backendDir, w); err != nil {
		t.Fatalf("SaveWork %s: %v", id, err)
	}
}

// TestContextWorkSectionListsActive: CONTEXT.md gains a '## In progress'
// section listing active works only (not done/abandoned), each rendered as
// "id — title (actor, since YYYY-MM-DD)".
func TestContextWorkSectionListsActive(t *testing.T) {
	backend := seedBackend(t)
	started := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	addWork(t, backend, "W-0001", "Investigar erro X", "henrique@laptop", domain.WorkActive, started)
	addWork(t, backend, "W-0002", "Já terminou", "claude-cli", domain.WorkDone, started)

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)

	if !strings.Contains(ctx, "## In progress") {
		t.Fatalf("missing '## In progress' section:\n%s", ctx)
	}
	if !strings.Contains(ctx, "W-0001 — Investigar erro X (henrique@laptop, since 2026-07-01)") {
		t.Fatalf("missing active work line:\n%s", ctx)
	}
	if strings.Contains(ctx, "W-0002") {
		t.Fatalf("done work must not be listed:\n%s", ctx)
	}
}

// TestContextWorkSectionOmittedWhenEmpty: with no active work, the section is
// omitted entirely (not rendered as an empty header) — this is what keeps
// TestMaterializeContextGolden byte-identical for backends with no work.
func TestContextWorkSectionOmittedWhenEmpty(t *testing.T) {
	backend := seedBackend(t)

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)

	if strings.Contains(ctx, "In progress") {
		t.Fatalf("section should be omitted when no active work:\n%s", ctx)
	}
}

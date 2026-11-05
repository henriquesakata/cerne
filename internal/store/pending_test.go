package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
)

func testPending(id string) Pending {
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	return Pending{
		ID:         id,
		Title:      "Tratar o caso Y",
		Body:       "ficou faltando no fechamento",
		From:       "F-0001",
		State:      domain.PendingOpen,
		Facets:     []string{"cli"},
		Components: []string{"lint"},
		CreatedAt:  at,
		UpdatedAt:  at,
	}
}

func TestPendingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := testPending("P-0001")
	want.State = domain.PendingPromoted
	want.PromotedTo = "T-0003"
	closed := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	want.ClosedAt = &closed
	want.Reason = "virou task"
	if err := SavePending(dir, want); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "pendings", "P-0001.json")); err != nil {
		t.Fatalf("expected entities/pendings/P-0001.json (subdir created on demand): %v", err)
	}
	got, err := LoadPending(dir, "P-0001")
	if err != nil {
		t.Fatalf("LoadPending: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.Body != want.Body || got.From != want.From ||
		got.State != want.State || got.PromotedTo != want.PromotedTo || got.Reason != want.Reason {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "cli" || len(got.Components) != 1 || got.Components[0] != "lint" {
		t.Fatalf("tags = %v / %v, want [cli] / [lint]", got.Facets, got.Components)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ClosedAt == nil || !got.ClosedAt.Equal(closed) {
		t.Fatalf("times: created %v closed %v, want %v / %v", got.CreatedAt, got.ClosedAt, want.CreatedAt, closed)
	}
}

func TestLoadAllPendingsSortedAndLegacyEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadAllPendings(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("LoadAllPendings on a backend without pendings/ = %v, %v; want empty, nil", got, err)
	}
	for _, id := range []string{"P-0002", "P-0001"} {
		if err := SavePending(dir, testPending(id)); err != nil {
			t.Fatalf("SavePending(%s): %v", id, err)
		}
	}
	got, err = LoadAllPendings(dir)
	if err != nil {
		t.Fatalf("LoadAllPendings: %v", err)
	}
	if len(got) != 2 || got[0].ID != "P-0001" || got[1].ID != "P-0002" {
		t.Fatalf("got %+v, want [P-0001 P-0002]", got)
	}
}

func TestNextIDPendingPrefix(t *testing.T) {
	dir := t.TempDir()
	if got, err := NextID(dir, "P"); err != nil || got != "P-0001" {
		t.Fatalf("NextID(P) = %q, %v; want P-0001", got, err)
	}
	if err := SavePending(dir, testPending("P-0001")); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	if got, err := NextID(dir, "P"); err != nil || got != "P-0002" {
		t.Fatalf("NextID(P) = %q, %v; want P-0002", got, err)
	}
}

func TestInitLayoutCreatesPendingsSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, testProject()); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "pendings", ".gitkeep")); err != nil {
		t.Fatalf("missing entities/pendings/.gitkeep: %v", err)
	}
}

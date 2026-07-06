package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
)

func testWork(id string) Work {
	return Work{
		ID:        id,
		Title:     "Investigar erro X",
		State:     domain.WorkActive,
		Actor:     "henrique@laptop",
		Refs:      []string{"F-0001", "D-0002"},
		CreatedAt: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
}

func TestWorkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := testWork("W-0001")
	want.State = domain.WorkDone
	want.Note = "resolvido: era um race condition"
	if err := SaveWork(dir, want); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	got, err := LoadWork(dir, "W-0001")
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.State != want.State ||
		got.Actor != want.Actor || got.Note != want.Note {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(got.Refs) != 2 || got.Refs[0] != "F-0001" || got.Refs[1] != "D-0002" {
		t.Fatalf("refs = %v, want [F-0001 D-0002]", got.Refs)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times: got %v / %v, want %v / %v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

func TestLoadWorkNotFound(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadWork(dir, "W-0001"); err == nil {
		t.Fatal("LoadWork on empty backend: want error, got nil")
	}
}

func TestLoadAllWorksSortedByID(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"W-0002", "W-0001"} {
		if err := SaveWork(dir, testWork(id)); err != nil {
			t.Fatalf("SaveWork(%s): %v", id, err)
		}
	}
	got, err := LoadAllWorks(dir)
	if err != nil {
		t.Fatalf("LoadAllWorks: %v", err)
	}
	if len(got) != 2 || got[0].ID != "W-0001" || got[1].ID != "W-0002" {
		t.Fatalf("got %+v, want [W-0001 W-0002]", got)
	}
}

// TestLoadAllWorksMissingSubdir: a legacy backend (works/ not yet created,
// e.g. InitLayout run before F-0130) yields an empty slice, not an error.
func TestLoadAllWorksMissingSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := LoadAllWorks(dir)
	if err != nil {
		t.Fatalf("LoadAllWorks on legacy backend: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

// TestSaveWorkCreatesSubdirOnDemand: SaveWork on a legacy backend (no
// works/ subdir from InitLayout) creates it rather than failing.
func TestSaveWorkCreatesSubdirOnDemand(t *testing.T) {
	dir := t.TempDir()
	if err := SaveWork(dir, testWork("W-0001")); err != nil {
		t.Fatalf("SaveWork on legacy backend: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "works", "W-0001.json")); err != nil {
		t.Fatalf("expected file created: %v", err)
	}
}

func TestNextIDWorkPrefix(t *testing.T) {
	dir := t.TempDir()
	got, err := NextID(dir, "W")
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if got != "W-0001" {
		t.Fatalf("got %q, want W-0001", got)
	}
	if err := SaveWork(dir, testWork("W-0001")); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	got, err = NextID(dir, "W")
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if got != "W-0002" {
		t.Fatalf("got %q, want W-0002", got)
	}
}

func TestInitLayoutCreatesWorksSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, testProject()); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "works", ".gitkeep")); err != nil {
		t.Fatalf("missing entities/works/.gitkeep: %v", err)
	}
}

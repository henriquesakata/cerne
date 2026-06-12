package store

import (
	"testing"
	"time"
)

// Pins the update path: os.Rename over an existing entity file must work
// on every platform (notably Windows, where replace semantics differ).
func TestSaveFeatureOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	f := Feature{ID: "F-0001", Title: "v1", State: "candidate", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := SaveFeature(dir, f); err != nil {
		t.Fatalf("first save: %v", err)
	}
	f.Title = "v2"
	if err := SaveFeature(dir, f); err != nil {
		t.Fatalf("second save (overwrite): %v", err)
	}
	got, err := LoadFeature(dir, "F-0001")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Title != "v2" {
		t.Fatalf("got %q, want v2", got.Title)
	}
}

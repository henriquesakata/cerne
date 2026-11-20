package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestEntityPathRefusesMalformedID pins B-0034: an id that is not PREFIX-NNNN
// never reaches a path. "../../entities/features/F-0001" resolves back to the
// real F-0001.json, so a load by it used to succeed and a write keyed by it
// landed outside its directory.
func TestEntityPathRefusesMalformedID(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFeature(dir, testFeature("F-0001")); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	for _, id := range []string{"../../entities/features/F-0001", "F-0001/../F-0001", "", "..", "f-0001"} {
		if _, err := LoadFeature(dir, id); !errors.Is(err, errInvalidID) || !errors.Is(err, ErrNotFound) {
			t.Errorf("LoadFeature(%q) err = %v, want errInvalidID wrapping ErrNotFound", id, err)
		}
		if err := DeleteReference(dir, id); !errors.Is(err, errInvalidID) {
			t.Errorf("DeleteReference(%q) err = %v, want errInvalidID", id, err)
		}
	}
	f := testFeature("../../content/F-0001")
	if err := SaveFeature(dir, f); !errors.Is(err, errInvalidID) {
		t.Errorf("SaveFeature(%q) err = %v, want errInvalidID", f.ID, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "content", "F-0001.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a malformed id wrote outside entities/: stat err = %v", err)
	}
	if _, err := LoadFeature(dir, "F-0001"); err != nil {
		t.Errorf("LoadFeature(F-0001) = %v, a well-formed id must still load", err)
	}
}

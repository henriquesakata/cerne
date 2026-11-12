package store

import (
	"testing"
	"time"
)

// A header with an empty author still reads back: the entry never vanishes.
func TestLoadHandoffsEmptyAuthor(t *testing.T) {
	dir := t.TempDir()
	if err := AppendHandoff(dir, "W-0001", "brief", "", time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if hs, err := LoadHandoffs(dir, "W-0001"); err != nil || len(hs) != 1 || hs[0].Text != "brief" {
		t.Fatalf("entries = %+v, %v; want the one with empty author", hs, err)
	}
}

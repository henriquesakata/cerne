package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkLogAppendReadInOrder(t *testing.T) {
	dir := t.TempDir()
	if got, err := LoadLog(dir, "W-0001"); err != nil || len(got) != 0 {
		t.Fatalf("empty log = %v, %v; want none", got, err)
	}
	if n, err := NextLogSeq(dir, "W-0001"); err != nil || n != 1 {
		t.Fatalf("first NextLogSeq = %d, want 1", n)
	}
	for i := 1; i <= 11; i++ {
		seq, err := NextLogSeq(dir, "W-0001")
		if err != nil {
			t.Fatal(err)
		}
		e := LogEntry{Seq: seq, UTC: "2026-11-13T15:33:00Z", Actor: "owner", RecordedBy: "a@h", Text: "t"}
		if e.Seq != i {
			t.Fatalf("NextLogSeq = %d, want %d", e.Seq, i)
		}
		if i == 11 {
			e.Corrects = 2
		}
		if err := SaveLogEntry(dir, "W-0001", e); err != nil {
			t.Fatalf("SaveLogEntry %d: %v", i, err)
		}
	}
	got, err := LoadLog(dir, "W-0001")
	if err != nil || len(got) != 11 {
		t.Fatalf("LoadLog = %d entries, %v", len(got), err)
	}
	for i, e := range got {
		if e.Seq != i+1 {
			t.Fatalf("entry %d has seq %d (numeric order, 9 before 10)", i, e.Seq)
		}
	}
	if got[10].Corrects != 2 || got[0].Actor != "owner" || got[0].RecordedBy != "a@h" {
		t.Fatalf("fields lost: %+v %+v", got[0], got[10])
	}
	if other, _ := LoadLog(dir, "W-0002"); len(other) != 0 {
		t.Fatalf("W-0002 log leaked: %v", other)
	}
}

func TestSaveLogEntryRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	e := LogEntry{Seq: 1, UTC: "2026-11-13T15:33:00Z", Actor: "owner", Text: "um"}
	if err := SaveLogEntry(dir, "W-0001", e); err != nil {
		t.Fatal(err)
	}
	e.Text = "dois"
	if err := SaveLogEntry(dir, "W-0001", e); !errors.Is(err, ErrLogEntryExists) {
		t.Fatalf("overwrite err = %v, want ErrLogEntryExists", err)
	}
	got, _ := LoadLog(dir, "W-0001")
	if len(got) != 1 || got[0].Text != "um" {
		t.Fatalf("entry changed: %+v", got)
	}
}

// A corrupt entry is a read error, not a free number that SaveLogEntry would
// then refuse as "already exists".
func TestNextLogSeqSurfacesCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLogEntry(dir, "W-0001", LogEntry{Seq: 1, UTC: "2026-11-13T15:33:00Z", Text: "um"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "content", "worklogs", "W-0001", "2.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NextLogSeq(dir, "W-0001"); err == nil {
		t.Fatal("NextLogSeq swallowed a corrupt entry")
	}
}

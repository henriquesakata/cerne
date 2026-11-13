package store

import (
	"errors"
	"testing"
)

func TestWorkLogAppendReadInOrder(t *testing.T) {
	dir := t.TempDir()
	if got, err := LoadLog(dir, "W-0001"); err != nil || len(got) != 0 {
		t.Fatalf("empty log = %v, %v; want none", got, err)
	}
	if n := NextLogSeq(dir, "W-0001"); n != 1 {
		t.Fatalf("first NextLogSeq = %d, want 1", n)
	}
	for i := 1; i <= 11; i++ {
		e := LogEntry{Seq: NextLogSeq(dir, "W-0001"), UTC: "2026-11-13T15:33:00Z", Actor: "owner", RecordedBy: "a@h", Text: "t"}
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

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// F-0339: AppendErrata is byte for byte what it was before appendEntry was
// extracted from it.
func TestAppendErrataBytesUnchanged(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	// The third text cites a handoff header: defangHeadings now escapes it in an
	// erratum too (F-0339, accepted change of bytes).
	for _, txt := range []string{"um", "dois\n## errata 2026-01-01 — x", "tres\n## handoff 2026-01-01 — y"} {
		if err := AppendErrata(dir, "F-0001", txt, "ator", at); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := LoadErrata(dir, "F-0001")
	want := "---\ncerne_id: F-0001\n---\n\n## errata 2026-06-25 — ator\n\num\n\n## errata 2026-06-25 — ator\n\ndois\n\\## errata 2026-01-01 — x\n\n## errata 2026-06-25 — ator\n\ntres\n\\## handoff 2026-01-01 — y\n"
	if got != want {
		t.Fatalf("errata bytes changed:\n%q\nwant\n%q", got, want)
	}
}

// F-0339 crit. 5: handoffs append, in order, never rewriting earlier ones; a
// brief carrying a `## handoff …` line cannot open a forged entry.
func TestAppendHandoffAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "content", "works", "W-0001", "handoff.md")
	t1 := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	if err := AppendHandoff(dir, "W-0001", "brief um", "ator", t1); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	forged := "brief dois\n## handoff 2026-01-01 — forjado\nmais"
	if err := AppendHandoff(dir, "W-0001", forged, "outro", t1.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	all, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(all), string(first)) {
		t.Fatalf("earlier entry rewritten:\n%s", all)
	}
	hs, err := LoadHandoffs(dir, "W-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 {
		t.Fatalf("got %d entries, want 2 (forged header must not open one): %+v", len(hs), hs)
	}
	if hs[0].Date != "2026-11-11" || hs[0].Author != "ator" || hs[0].Text != "brief um" {
		t.Fatalf("entry 1 = %+v", hs[0])
	}
	if hs[1].Date != "2026-11-12" || hs[1].Author != "outro" || !strings.Contains(hs[1].Text, "forjado") {
		t.Fatalf("entry 2 = %+v", hs[1])
	}
	if none, err := LoadHandoffs(dir, "W-9999"); err != nil || len(none) != 0 {
		t.Fatalf("missing file = %v, %v; want none", none, err)
	}
}

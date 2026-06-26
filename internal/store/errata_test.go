package store

import (
	"strings"
	"testing"
	"time"
)

func TestAppendErrataCreatesFileAndAppendsInOrder(t *testing.T) {
	dir := t.TempDir()
	t1 := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	if err := AppendErrata(dir, "F-0001", "primeira correção", "ator", t1); err != nil {
		t.Fatalf("AppendErrata 1: %v", err)
	}
	got, err := LoadErrata(dir, "F-0001")
	if err != nil {
		t.Fatalf("LoadErrata: %v", err)
	}
	if !strings.Contains(got, "primeira correção") {
		t.Fatalf("errata missing first entry:\n%s", got)
	}
	if !strings.Contains(got, "2026-06-25") {
		t.Fatalf("errata missing dated header:\n%s", got)
	}

	t2 := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	if err := AppendErrata(dir, "F-0001", "segunda correção", "ator", t2); err != nil {
		t.Fatalf("AppendErrata 2: %v", err)
	}
	got, _ = LoadErrata(dir, "F-0001")
	i1, i2 := strings.Index(got, "primeira correção"), strings.Index(got, "segunda correção")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Fatalf("entries not in chronological order:\n%s", got)
	}
	if n := strings.Count(got, "cerne_id: F-0001"); n != 1 {
		t.Fatalf("frontmatter should appear exactly once, got %d:\n%s", n, got)
	}
}

func TestLoadErrataMissingReturnsEmpty(t *testing.T) {
	got, err := LoadErrata(t.TempDir(), "F-9999")
	if err != nil {
		t.Fatalf("LoadErrata missing: unexpected err %v", err)
	}
	if got != "" {
		t.Fatalf("LoadErrata missing = %q, want empty", got)
	}
}

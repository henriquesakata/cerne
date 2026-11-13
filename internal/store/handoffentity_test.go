package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

// F-0368: HO- entities live in entities/handoffs/ with their brief in
// content/handoffs/, minted by NextID("HO") apart from the H- historicals.
func TestHandoffEntityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, Project{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"entities/handoffs", "content/handoffs"} {
		if st, err := os.Stat(filepath.Join(dir, sub)); err != nil || !st.IsDir() {
			t.Fatalf("InitLayout did not create %s: %v", sub, err)
		}
	}
	if err := SaveHistorical(dir, Historical{ID: "H-0007"}); err != nil {
		t.Fatal(err)
	}
	id, err := NextID(dir, "HO")
	if err != nil || id != "HO-0001" {
		t.Fatalf("NextID(HO) = %q, %v; want HO-0001", id, err)
	}
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	h := Handoff{ID: id, State: domain.HandoffPending, FromWork: "W-0001", Refs: []string{"F-0001"}, Actor: "a", Next: "n", CreatedAt: at, UpdatedAt: at}
	if err := SaveHandoff(dir, h); err != nil {
		t.Fatal(err)
	}
	if err := SaveHandoffContent(dir, id, "brief\n"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHandoff(dir, id)
	if err != nil || got.FromWork != "W-0001" || got.State != domain.HandoffPending || !got.CreatedAt.Equal(at) {
		t.Fatalf("LoadHandoff = %+v, %v", got, err)
	}
	if body, err := LoadHandoffContent(dir, id); err != nil || body != "brief\n" {
		t.Fatalf("content = %q, %v", body, err)
	}
	if !EntityExists(dir, id) {
		t.Fatalf("EntityExists(%s) = false", id)
	}
	if next, _ := NextID(dir, "HO"); next != "HO-0002" {
		t.Fatalf("second NextID = %s", next)
	}
	all, err := LoadAllHandoffs(dir)
	if err != nil || len(all) != 1 {
		t.Fatalf("LoadAllHandoffs = %+v, %v", all, err)
	}
	if _, err := LoadHandoff(dir, "HO-0009"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

// F-0368 crit. 7: the list lines, on a fixed clock.
func TestHandoffLines(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	taken := now.Add(-26 * time.Hour)
	p := Handoff{ID: "HO-0007", State: domain.HandoffPending, FromWork: "W-1234", Actor: "agent:a@b", CreatedAt: now.Add(-3 * time.Hour)}
	if got, want := p.Line("Migração", now), "HO-0007 pending 3h — W-1234 Migração — from agent:a@b"; got != want {
		t.Fatalf("pending line = %q, want %q", got, want)
	}
	tk := Handoff{ID: "HO-0006", State: domain.HandoffTaken, FromWork: "W-1230", TakenBy: "u@h", TakenWork: "W-1240", TakenAt: &taken}
	if got, want := tk.Line("", now), "HO-0006 taken 1d ago by u@h (W-1240) — from W-1230"; got != want {
		t.Fatalf("taken line = %q, want %q", got, want)
	}
	s := Handoff{ID: "HO-0005", State: domain.HandoffSuperseded, FromWork: "W-1230", SupersededBy: "HO-0006"}
	if got, want := s.Line("", now), "HO-0005 superseded by HO-0006 — from W-1230"; got != want {
		t.Fatalf("superseded line = %q, want %q", got, want)
	}
	c := Handoff{ID: "HO-0004", State: domain.HandoffCancelled, FromWork: "W-1230", CancelReason: "plano mudou", UpdatedAt: now.Add(-45 * time.Minute)}
	if got, want := c.Line("", now), "HO-0004 cancelled 45m ago — from W-1230: plano mudou"; got != want {
		t.Fatalf("cancelled line = %q, want %q", got, want)
	}
}

// F-0368 critic round 1, item 7: a handoff from a machine whose clock runs
// ahead reads as 0m, never a negative age.
func TestShortAgeClampsSkew(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	h := Handoff{ID: "HO-0001", State: domain.HandoffPending, FromWork: "W-0001", Actor: "a", CreatedAt: now.Add(2 * time.Hour)}
	if got, want := h.Line("t", now), "HO-0001 pending 0m — W-0001 t — from a"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

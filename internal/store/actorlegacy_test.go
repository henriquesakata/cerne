package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

// F-0442 criterion 4: a record written with a login@host author reads back
// with the machine pseudonym, and the file on disk is not touched.
func TestLegacyActorsReadAsPseudonym(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, Project{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	const old = "agent:alice@mac.local"
	want := "agent:" + domain.MachinePseudonym("alice@mac.local")
	if err := SaveWork(dir, Work{ID: "W-0001", State: domain.WorkActive, Actor: old, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := SaveHandoff(dir, Handoff{ID: "HO-0001", State: domain.HandoffPending, Actor: "alice@mac.local", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvent(dir, domain.NewEvent("W-0001", "work.started", nil, old)); err != nil {
		t.Fatal(err)
	}
	if err := AppendHandoff(dir, "W-0001", "brief", old, at); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, dir)

	w, _ := LoadWork(dir, "W-0001")
	ws, _ := LoadAllWorks(dir)
	h, _ := LoadHandoff(dir, "HO-0001")
	hs, _ := LoadAllHandoffs(dir)
	evs, err := ReadAllEvents(dir)
	if err != nil || len(evs) != 1 {
		t.Fatalf("events = %v, %v", evs, err)
	}
	es, _ := LoadHandoffs(dir, "W-0001")
	revs, _ := ReadEvents(dir)
	checks := map[string]string{
		"LoadWork": w.Actor, "LoadAllWorks": ws[0].Actor, "LoadHandoff": h.Actor,
		"LoadAllHandoffs": hs[0].Actor, "ReadAllEvents": evs[0].Actor,
		"ReadEvents": revs[0].Actor, "LoadHandoffs": es[0].Author,
	}
	for name, got := range checks {
		if strings.Contains(got, "@") || !strings.HasPrefix(strings.TrimPrefix(got, "agent:"), "m-") {
			t.Errorf("%s actor = %q, want a pseudonym", name, got)
		}
	}
	if w.Actor != want {
		t.Errorf("LoadWork actor = %q, want %q", w.Actor, want)
	}
	if after := snapshotTree(t, dir); after != before {
		t.Fatal("reading rewrote a file")
	}
}

func snapshotTree(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		b.WriteString(p + "\x00" + string(data) + "\x00")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

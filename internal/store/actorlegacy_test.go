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
	if err := SaveHandoff(dir, Handoff{ID: "HO-0001", State: domain.HandoffPending, Actor: "alice@mac.local", TakenBy: "bob@mac.local", CreatedAt: at, UpdatedAt: at}); err != nil {
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
	checks["LoadHandoff TakenBy"], checks["LoadAllHandoffs TakenBy"] = h.TakenBy, hs[0].TakenBy
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

// A plain name round-trips unchanged (F-0442): only an "@" is swapped.
func TestPlainActorRoundTripsUnchanged(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	for _, a := range []string{"fulano", "agent", "human", "agent:session-0123abcd"} {
		if err := SaveWork(dir, Work{ID: "W-0001", State: domain.WorkActive, Actor: a, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if w, _ := LoadWork(dir, "W-0001"); w.Actor != a {
			t.Errorf("actor %q read back as %q", a, w.Actor)
		}
	}
}

// The handoff.md headers and the work log are labeled for every reader; a
// write never stores a login@host author.
func TestHandoffBodyLogAndWritesLabeled(t *testing.T) {
	dir := t.TempDir()
	p := domain.MachinePseudonym("alice@mac.local")
	body := "---\ncerne_id: W-0001\n---\n\n## handoff 2026-11-11 — agent:alice@mac.local\n\ntexto com alice@mac.local no corpo\n"
	got := LabelHandoffHeaders(body)
	if !strings.Contains(got, "## handoff 2026-11-11 — agent:"+p+"\n") || strings.Contains(strings.SplitN(got, "\n\n", 3)[1], "@") {
		t.Errorf("header not labeled:\n%s", got)
	}
	if !strings.Contains(got, "texto com alice@mac.local no corpo") {
		t.Errorf("free text must stay as written:\n%s", got)
	}
	if err := AppendHandoff(dir, "W-0002", "brief", "alice@mac.local", time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "content", "works", "W-0002", "handoff.md"))
	if strings.Contains(string(raw), "@") {
		t.Errorf("AppendHandoff stored a login@host author:\n%s", raw)
	}
	if err := SaveLogEntry(dir, "W-0001", LogEntry{Seq: 1, UTC: "2026-11-01T10:00:00Z", Actor: "alice@mac.local", RecordedBy: "agent:alice@mac.local", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	es, err := LoadLog(dir, "W-0001")
	if err != nil || len(es) != 1 || es[0].Actor != p || es[0].RecordedBy != "agent:"+p {
		t.Errorf("log = %+v, %v", es, err)
	}
}

// F-0442: the actor inside a legacy work.started payload is read as the
// pseudonym too; the file stays byte-identical and a content key is untouched.
func TestLegacyPayloadActorReadAsPseudonym(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, Project{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	ev := domain.NewEvent("W-0001", "work.started", map[string]string{"title": "alice@mac.local fala", "actor": "agent:alice@mac.local", "by": "D-0052"}, "agent:alice@mac.local")
	if err := AppendEvent(dir, ev); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, dir)
	evs, err := ReadEvents(dir)
	if err != nil || len(evs) != 1 {
		t.Fatalf("events = %v, %v", evs, err)
	}
	want := "agent:" + domain.MachinePseudonym("alice@mac.local")
	if got := evs[0].Payload["actor"]; got != want {
		t.Errorf("payload actor = %q, want %q", got, want)
	}
	if evs[0].Payload["by"] != "D-0052" || evs[0].Payload["title"] != "alice@mac.local fala" {
		t.Errorf("non-actor payload keys changed: %v", evs[0].Payload)
	}
	if snapshotTree(t, dir) != before {
		t.Fatal("reading rewrote a file")
	}
}

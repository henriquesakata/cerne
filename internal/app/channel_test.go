package app

import (
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/room"
	"gitlab.com/rockt/cerne/internal/store"
)

var chanT0 = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

// saveChanWork persists an active work with the given refs/paths.
func saveChanWork(t *testing.T, repoDir, id string, refs, paths []string) {
	t.Helper()
	if err := store.SaveWork(repoDir, store.Work{
		ID: id, Title: "atividade " + id, State: domain.WorkActive, Actor: "agent",
		Refs: refs, Paths: paths, CreatedAt: chanT0, UpdatedAt: chanT0,
	}); err != nil {
		t.Fatalf("SaveWork(%s): %v", id, err)
	}
}

// TestSessionWork (F-0325): a session's channel identity is its live work —
// directly for a W- binding, via its own auto work for an F- binding, and
// nothing for a dead binding.
func TestSessionWork(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	saveChanWork(t, repoDir, "W-0001", []string{"F-0009"}, []string{"internal/server/**"})
	if err := store.SaveFeature(repoDir, store.Feature{
		ID: "F-0002", Title: "viva", State: domain.StateInDevelopment, CreatedAt: chanT0, UpdatedAt: chanT0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWork(repoDir, store.Work{
		ID: "W-0003", Title: "auto", State: domain.WorkActive, Actor: "agent", Auto: true,
		Refs: []string{"F-0002"}, CreatedAt: chanT0, UpdatedAt: chanT0,
	}); err != nil {
		t.Fatal(err)
	}
	bindTestWork(t, repoDir, "W-0004", domain.WorkDone)
	for sess, id := range map[string]string{"s-w": "W-0001", "s-f": "F-0002", "s-dead": "W-0004"} {
		if err := BindSession(cacheDir, sess, id); err != nil {
			t.Fatal(err)
		}
	}

	got, ok := sessionWork(cacheDir, repoDir, "s-w")
	if !ok || got.Work != "W-0001" || len(got.Refs) != 1 || got.Paths[0] != "internal/server/**" {
		t.Fatalf("W- binding: %+v, %v", got, ok)
	}
	if got, ok := sessionWork(cacheDir, repoDir, "s-f"); !ok || got.Work != "W-0003" {
		t.Fatalf("F- binding: %+v, %v; want auto work W-0003", got, ok)
	}
	if got, ok := sessionWork(cacheDir, repoDir, "s-dead"); ok {
		t.Fatalf("dead binding resolved: %+v", got)
	}
	if got, ok := sessionWork(cacheDir, repoDir, "s-none"); ok {
		t.Fatalf("unbound session resolved: %+v", got)
	}
}

// TestChannelDirectedTo (F-0325 crit. 9): a message is addressed to a session
// when it names the session's work, one of the work's refs, or a path inside
// its declared scope — never by a near-miss id, and never to its own sender.
func TestChannelDirectedTo(t *testing.T) {
	me := channelIdentity{Work: "W-0290", Refs: []string{"F-0325"}, Paths: []string{"internal/server/**", "ui/src/App.tsx"}}
	for _, tc := range []struct {
		name string
		nick string
		body string
		want bool
	}{
		{"names my work", "W-0291", "W-0290, segura aí", true},
		{"names my work with @", "W-0291", "@W-0290 pode consumir", true},
		{"names one of my refs", "W-0291", "terminei o contrato de F-0325", true},
		{"names another ref", "W-0291", "terminei F-0326", false},
		{"path inside my scope", "W-0291", "vou mexer em internal/server/rooms.go, segura", true},
		{"path in backticks", "W-0291", "vou mexer em `internal/server/rooms.go`.", true},
		{"exact declared path", "W-0291", "mexendo em ui/src/App.tsx", true},
		{"path outside my scope", "W-0291", "vou mexer em internal/cli/root.go", false},
		{"near-miss id", "W-0291", "W-02901 terminou", false},
		{"own message", "W-0290", "W-0290 aqui, mexendo em internal/server/rooms.go", false},
		{"background", "W-0291", "build verde no main", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := room.Message{Nick: tc.nick, Body: tc.body}
			if got := directedTo(m, me); got != tc.want {
				t.Fatalf("directedTo(%q from %s) = %v, want %v", tc.body, tc.nick, got, tc.want)
			}
		})
	}
}

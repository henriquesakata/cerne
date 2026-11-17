package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0442 crit. 7: a backend with login@host authors (work, event, handoff)
// shows only the pseudonym on every CLI surface; the files stay untouched.
func TestLegacyActorsNeverSurface(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	t.Setenv("CURSOR_AGENT", "")
	projDir, _, repoDir := setupProject(t)
	const old = "agent:alice@mac.local"
	now := time.Now().UTC()
	if err := store.SaveWork(repoDir, store.Work{ID: "W-0001", Title: "Legado", State: domain.WorkActive, Actor: old, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveHandoff(repoDir, store.Handoff{ID: "HO-0001", State: domain.HandoffPending, FromWork: "W-0001", Actor: old, Next: "n", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendHandoff(repoDir, "W-0001", "brief", old, now); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent("W-0001", "work.started", nil, old)); err != nil {
		t.Fatal(err)
	}
	pseudo := domain.MachinePseudonym("alice@mac.local")
	for _, args := range [][]string{
		{"show", "W-0001"},
		{"show", "HO-0001"},
		{"work", "list"},
		{"search", "Legado"},
	} {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if strings.Contains(out, "alice") || strings.Contains(out, "mac.local") {
			t.Errorf("%v leaks the login or host:\n%s", args, out)
		}
	}
	out, _ := execCerne(t, "work", "list", "--dir", projDir)
	if !strings.Contains(out, "agent:"+pseudo) {
		t.Errorf("work list lacks agent:%s:\n%s", pseudo, out)
	}
	// the session-start board reads the stored legacy work
	board, err := execCerneStdin(t, sessionPayload("sess-x", projDir, "startup"), "hook", "session-context")
	if err != nil {
		t.Fatalf("session-context: %v\n%s", err, board)
	}
	if !strings.Contains(board, "W-0001") || strings.Contains(board, "alice") || strings.Contains(board, "mac.local") {
		t.Errorf("session-start board leaks the login or host (or lacks the work):\n%s", board)
	}
	raw, err := os.ReadFile(filepath.Join(repoDir, "entities", "works", "W-0001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "alice@mac.local") {
		t.Errorf("reading rewrote the work file: %s", raw)
	}
}

// F-0442 crit. 3: `work start` prints the actor it recorded, transformed.
func TestWorkStartOutputShowsRecordedActor(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	t.Setenv("CURSOR_AGENT", "")
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "work", "start", "Revisar", "--agent", "bob@x.local", "--dir", projDir)
	if err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	if !strings.Contains(out, "actor: "+domain.MachinePseudonym("bob@x.local")) || strings.Contains(out, "@") {
		t.Errorf("work start output does not show the recorded pseudonym:\n%s", out)
	}
}

package index

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func saveWhenNote(t *testing.T, repoDir, id, title string, state domain.NoteState, when ...string) {
	t.Helper()
	now := time.Now().UTC()
	if err := store.SaveNote(repoDir, store.Note{ID: id, Title: title, State: state, When: when, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func triggerRows(t *testing.T, db string) []string {
	t.Helper()
	got, err := NoteTriggers(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, h := range got {
		out = append(out, h.ID+" "+h.Title+" "+h.Trig)
	}
	slices.Sort(out)
	return out
}

// F-0457: Rebuild indexes the declared triggers of active notes only.
func TestNoteTriggersRebuild(t *testing.T) {
	repoDir := t.TempDir()
	saveWhenNote(t, repoDir, "N-0001", "Tag", domain.NoteActive, "bash:git tag", "edit:docs.lock")
	saveWhenNote(t, repoDir, "N-0002", "Old", domain.NoteSuperseded, "bash:git tag")
	saveWhenNote(t, repoDir, "N-0003", "Plain", domain.NoteActive)
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"N-0001 Tag bash:git tag", "N-0001 Tag edit:docs.lock"}
	if got := triggerRows(t, db); !slices.Equal(got, want) {
		t.Fatalf("triggers = %q, want %q", got, want)
	}
}

// F-0457: Update rewrites the triggers of the touched ids — a new trigger
// shows up, a superseded note's triggers leave.
func TestNoteTriggersUpdate(t *testing.T) {
	repoDir := t.TempDir()
	saveWhenNote(t, repoDir, "N-0001", "Tag", domain.NoteActive, "bash:git tag")
	saveWhenNote(t, repoDir, "N-0002", "Lock", domain.NoteActive)
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	saveWhenNote(t, repoDir, "N-0001", "Tag", domain.NoteSuperseded, "bash:git tag")
	saveWhenNote(t, repoDir, "N-0002", "Lock", domain.NoteActive, "edit:docs.lock")
	if err := Update(repoDir, db, []string{"N-0001", "N-0002"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got, want := triggerRows(t, db), []string{"N-0002 Lock edit:docs.lock"}; !slices.Equal(got, want) {
		t.Fatalf("triggers = %q, want %q", got, want)
	}
}

// F-0457: an index built before the triggers table makes Update fail, so the
// caller rebuilds it whole instead of leaving it half-filled; reading it is
// "no triggers", never an error.
func TestUpdateNoWhenTable(t *testing.T) {
	repoDir := t.TempDir()
	saveWhenNote(t, repoDir, "N-0001", "Tag", domain.NoteActive, "bash:git tag")
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`DROP TABLE note_when`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if got, err := NoteTriggers(context.Background(), db); got != nil || err != nil {
		t.Fatalf("old index: got %v, err %v", got, err)
	}
	if err := Update(repoDir, db, []string{"N-0001"}, time.Time{}); err == nil {
		t.Fatal("Update on an index without note_when must fail (caller rebuilds)")
	}
}

func TestNoteTriggersMissingDB(t *testing.T) {
	got, err := NoteTriggers(context.Background(), filepath.Join(t.TempDir(), "none.db"))
	if got != nil || err != nil {
		t.Fatalf("got %v, err %v", got, err)
	}
}

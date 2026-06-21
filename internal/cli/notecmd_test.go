package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// newNote creates a note via the CLI and returns its expected ID.
func newNote(t *testing.T, projDir, title string, seq int) string {
	t.Helper()
	out, err := execCerne(t, "note", "new", title, "--dir", projDir)
	if err != nil {
		t.Fatalf("note new %q: %v\noutput: %s", title, err, out)
	}
	return fmt.Sprintf("N-%04d", seq)
}

func TestNoteNew(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)

	out, err := execCerne(t, "note", "new", "go-git não lê ~/.ssh/config", "--dir", projDir)
	if err != nil {
		t.Fatalf("note new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "N-0001 criada (active)") {
		t.Fatalf("note new output missing N-0001 confirmation:\n%s", out)
	}

	n, err := store.LoadNote(repoDir, "N-0001")
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if n.State != domain.NoteActive || n.Title != "go-git não lê ~/.ssh/config" {
		t.Fatalf("note = %+v, want active with the given title", n)
	}

	// A note with no body is born a stub: it carries the cerne_id frontmatter
	// and the title heading, but doccheck reads it as a stub (no real content).
	body, err := os.ReadFile(filepath.Join(repoDir, "content", "notes", "N-0001.md"))
	if err != nil {
		t.Fatalf("read note doc: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\ncerne_id: N-0001\n---") {
		t.Fatalf("note doc missing cerne_id frontmatter:\n%s", body)
	}
	if doccheck.HasRealContent(body) {
		t.Fatalf("a freshly scaffolded note must read as a stub:\n%s", body)
	}

	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "note.created" || last.EntityID != "N-0001" || last.Payload["title"] != "go-git não lê ~/.ssh/config" {
		t.Fatalf("last event = %+v, want note.created for N-0001 with title", last)
	}

	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	head, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if lock.Snapshot != head {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, head)
	}

	if _, err := execCerne(t, "note", "new", "   ", "--dir", projDir); err == nil {
		t.Fatalf("note new with blank title succeeded, want error")
	}
}

func TestNoteNewWithFileBody(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("O índice SQLite é cache derivado: apague e reconstrua.\n"), 0o644); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	if _, err := execCerne(t, "note", "new", "Índice é derivado", "--file", bodyFile, "--dir", projDir); err != nil {
		t.Fatalf("note new --file: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(repoDir, "content", "notes", "N-0001.md"))
	if err != nil {
		t.Fatalf("read note doc: %v", err)
	}
	if !strings.Contains(string(body), "cache derivado") {
		t.Fatalf("note body not written from --file:\n%s", body)
	}
	// A note with a real body reads as real content (not a stub).
	if !doccheck.HasRealContent(body) {
		t.Fatalf("a note with a --file body must read as real content:\n%s", body)
	}
}

func TestNoteInforms(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	// A feature must exist for --informs to backlink to it.
	if _, err := execCerne(t, "feature", "new", "Alvo", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}

	// Unknown target is rejected (no dangling backlink).
	if _, err := execCerne(t, "note", "new", "Nota X", "--informs", "F-9999", "--dir", projDir); err == nil {
		t.Fatalf("note new --informs F-9999 succeeded, want error")
	}

	if _, err := execCerne(t, "note", "new", "Convenção do build", "--informs", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("note new --informs F-0001: %v", err)
	}
	n, err := store.LoadNote(repoDir, "N-0001")
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if len(n.Informs) != 1 || n.Informs[0] != "F-0001" {
		t.Fatalf("note.Informs = %v, want [F-0001]", n.Informs)
	}
}

func TestNoteSubstituir(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newNote(t, projDir, "Nota A", 1)
	id2 := newNote(t, projDir, "Nota B", 2)

	// --reason is required.
	if _, err := execCerne(t, "note", "substituir", id, "--by", id2, "--dir", projDir); err == nil {
		t.Fatalf("substituir without --reason succeeded, want error")
	}
	if _, err := execCerne(t, "note", "substituir", id, "--by", id, "--reason", "dup", "--dir", projDir); err == nil {
		t.Fatalf("substituir --by self succeeded, want error")
	}
	if _, err := execCerne(t, "note", "substituir", id, "--by", "N-9999", "--reason", "dup", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "N-9999") {
		t.Fatalf("substituir --by nonexistent err = %v, want error naming N-9999", err)
	}

	// With --by: the successor replaces the old note.
	out, err := execCerne(t, "note", "substituir", id, "--by", id2, "--reason", "corrigida", "--dir", projDir)
	if err != nil {
		t.Fatalf("substituir: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing old→new state:\n%s", out)
	}
	n, err := store.LoadNote(repoDir, id)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if n.State != domain.NoteSuperseded || n.SupersededBy != id2 || n.Reason != "corrigida" {
		t.Fatalf("note = %+v, want superseded by %s with reason", n, id2)
	}

	// a terminal (already superseded) note cannot be superseded again.
	if _, err := execCerne(t, "note", "substituir", id, "--by", id2, "--reason", "again", "--dir", projDir); err == nil {
		t.Fatalf("substituir already-superseded note succeeded, want error")
	}
}

func TestNoteSubstituirRetire(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newNote(t, projDir, "Conhecimento obsoleto", 1)

	// Without --by: the note is merely retired (superseded, no successor).
	out, err := execCerne(t, "note", "substituir", id, "--reason", "não vale mais", "--dir", projDir)
	if err != nil {
		t.Fatalf("substituir (retire): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("retire output missing old→new state:\n%s", out)
	}
	n, err := store.LoadNote(repoDir, id)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if n.State != domain.NoteSuperseded || n.SupersededBy != "" || n.Reason != "não vale mais" {
		t.Fatalf("note = %+v, want superseded with no successor and the reason", n)
	}
}

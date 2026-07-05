package cli

import (
	"encoding/json"
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
	// A bodyless note is born a stub: the CLI keeps pointing the author at the
	// fill + push flow.
	if !strings.Contains(out, "preencha") || !strings.Contains(out, "cerne push") {
		t.Fatalf("note new (stub) output missing the fill+push next step:\n%s", out)
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

	out, err := execCerne(t, "note", "new", "Índice é derivado", "--file", bodyFile, "--dir", projDir)
	if err != nil {
		t.Fatalf("note new --file: %v", err)
	}
	// A note created with a body is already real content, committed and pushed
	// (CreateNote → CommitAndSnapshot). The CLI must confirm it is synced and
	// point at 'cerne pull', NOT the stub fill+push hint — otherwise the agent
	// pulls and cats the note just to verify a complete operation.
	if !strings.Contains(out, "já sincronizada") || !strings.Contains(out, "com corpo") {
		t.Fatalf("note new --file output must confirm a synced body:\n%s", out)
	}
	if !strings.Contains(out, "cerne pull") {
		t.Fatalf("note new --file output must point at 'cerne pull' to materialize:\n%s", out)
	}
	if strings.Contains(out, "preencha") || strings.Contains(out, "cerne push") {
		t.Fatalf("note new --file must not print the stub fill+push hint:\n%s", out)
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

// TestNoteListJSON: --json emits the notes as a parseable object so an agent can
// consume the roster without scraping the decorated text table.
func TestNoteListJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "n.md")
	if err := os.WriteFile(bodyFile, []byte("O índice é cache derivado.\n"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if _, err := execCerne(t, "note", "new", "Índice é derivado", "--file", bodyFile, "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}

	out, err := execCerne(t, "note", "list", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("note list --json: %v\noutput: %s", err, out)
	}
	var data struct {
		Notes []struct {
			ID    string `json:"id"`
			State string `json:"state"`
			Title string `json:"title"`
		} `json:"notes"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("note list --json is not valid JSON: %v\n%s", err, out)
	}
	if len(data.Notes) != 1 || data.Notes[0].ID != "N-0001" || data.Notes[0].Title != "Índice é derivado" {
		t.Fatalf("note list --json notes = %+v, want one N-0001", data.Notes)
	}
}

func TestNoteInforms(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	// A feature must exist for --informs to backlink to it.
	if _, err := execCerne(t, "feature", "new", "Alvo", "--context", "ctx de teste", "--dir", projDir); err != nil {
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

func TestNoteRetire(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newNote(t, projDir, "Conhecimento que envelheceu", 1)

	// --reason is required.
	if _, err := execCerne(t, "note", "retire", id, "--dir", projDir); err == nil {
		t.Fatalf("note retire without --reason succeeded, want error")
	}

	// retire moves the note to superseded with NO successor (like substituir
	// sem --by), and the output reads as a retirement.
	out, err := execCerne(t, "note", "retire", id, "--reason", "resolvida pela F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("note retire: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "aposentada") || !strings.Contains(out, "superseded") {
		t.Fatalf("note retire output missing aposentada/superseded:\n%s", out)
	}
	n, err := store.LoadNote(repoDir, id)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if n.State != domain.NoteSuperseded || n.SupersededBy != "" || n.Reason != "resolvida pela F-0001" {
		t.Fatalf("note = %+v, want superseded with no successor and the reason", n)
	}
}

func TestNoteKeep(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newNote(t, projDir, "Conhecimento durável", 1)

	out, err := execCerne(t, "note", "keep", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("note keep: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "durável") {
		t.Fatalf("note keep output missing durável confirmation:\n%s", out)
	}
	n, err := store.LoadNote(repoDir, id)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if !n.Durable || n.State != domain.NoteActive {
		t.Fatalf("note = %+v, want active and durable after keep", n)
	}
	// The note.kept event was appended.
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if last := evs[len(evs)-1]; last.Type != "note.kept" || last.EntityID != id {
		t.Fatalf("last event = %+v, want note.kept for %s", last, id)
	}

	// keep is idempotent: a second keep succeeds without error.
	if _, err := execCerne(t, "note", "keep", id, "--dir", projDir); err != nil {
		t.Fatalf("second note keep (idempotent) failed: %v", err)
	}

	// keep refuses a non-active (superseded) note.
	id2 := newNote(t, projDir, "Para aposentar", 2)
	if _, err := execCerne(t, "note", "retire", id2, "--reason", "obsoleta", "--dir", projDir); err != nil {
		t.Fatalf("note retire: %v", err)
	}
	if _, err := execCerne(t, "note", "keep", id2, "--dir", projDir); err == nil {
		t.Fatalf("note keep on a superseded note succeeded, want error")
	}
}

func TestNoteList(t *testing.T) {
	projDir, _, _ := setupProject(t)

	// Empty project: a clear "no notes" signal. This is the gap the read command
	// closes — without it an agent flails looking for a list path and concludes
	// nothing exists; here it gets an explicit, greppable answer.
	out, err := execCerne(t, "note", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("note list (empty): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nenhuma") {
		t.Fatalf("empty note list must state there are no notes:\n%s", out)
	}

	a := newNote(t, projDir, "go-git não lê ssh config", 1)
	b := newNote(t, projDir, "Índice é derivado", 2)
	c := newNote(t, projDir, "Conhecimento obsoleto", 3)
	// Retire c so it is non-active, and mark a durable so the marker shows.
	if _, err := execCerne(t, "note", "retire", c, "--reason", "obsoleta", "--dir", projDir); err != nil {
		t.Fatalf("note retire: %v", err)
	}
	if _, err := execCerne(t, "note", "keep", a, "--dir", projDir); err != nil {
		t.Fatalf("note keep: %v", err)
	}

	// Default lists active notes only, read from the backend store.
	out, err = execCerne(t, "note", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("note list: %v\noutput: %s", err, out)
	}
	for _, want := range []string{a, "go-git não lê ssh config", b, "Índice é derivado", "durá"} {
		if !strings.Contains(out, want) {
			t.Fatalf("note list missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, c) {
		t.Fatalf("note list (default) must omit the retired note %s:\n%s", c, out)
	}

	// --all includes the superseded one with its state.
	out, err = execCerne(t, "note", "list", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("note list --all: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, c) || !strings.Contains(out, "superseded") {
		t.Fatalf("note list --all must include the superseded note with its state:\n%s", out)
	}
}

func TestNoteShow(t *testing.T) {
	projDir, _, _ := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("O índice SQLite é cache derivado: apague e reconstrua.\n"), 0o644); err != nil {
		t.Fatalf("write body file: %v", err)
	}
	if _, err := execCerne(t, "note", "new", "Índice é derivado", "--file", bodyFile, "--dir", projDir); err != nil {
		t.Fatalf("note new --file: %v", err)
	}

	// show prints metadata (id, state, title) and the full body — both read
	// straight from the backend store, so they are immune to a stale .docs/.
	out, err := execCerne(t, "note", "show", "N-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("note show: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"N-0001", "active", "Índice é derivado", "cache derivado"} {
		if !strings.Contains(out, want) {
			t.Fatalf("note show output missing %q:\n%s", want, out)
		}
	}

	// Unknown id errors, naming the id.
	if _, err := execCerne(t, "note", "show", "N-9999", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "N-9999") {
		t.Fatalf("note show N-9999 err = %v, want error naming N-9999", err)
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

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// NoteOutcome reports a note transition that was applied and synced.
type NoteOutcome struct {
	ID   string
	From domain.NoteState
	To   domain.NoteState
}

// CreateNote mints a new active note in the backend clone at repoDir: validates
// each --informs target (F-/D-) exists, allocates the next N- id, persists the
// entity, writes the note body (from body, or a minimal stub when empty),
// appends the note.created event, commits+pushes the backend and refreshes the
// docs.lock snapshot in projDir. An empty body yields a scaffold that reads as a
// stub (doccheck.HasRealContent false) until the author fills it.
func CreateNote(repoDir, projDir, title, body string, informs, facets, components []string) (store.Note, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return store.Note{}, fmt.Errorf("o título da nota não pode ser vazio")
	}
	cleaned, err := validateInforms(repoDir, informs)
	if err != nil {
		return store.Note{}, err
	}
	// Validate/normalize tags before any side effect (NextID, save): an unknown
	// facet must abort without minting an id or writing a file.
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Note{}, err
	}
	normComponents := domain.NormalizeComponents(components)
	id, err := store.NextID(repoDir, "N")
	if err != nil {
		return store.Note{}, err
	}
	now := time.Now().UTC()
	n := store.Note{
		ID:         id,
		Title:      title,
		State:      domain.NoteActive,
		Informs:    cleaned,
		Facets:     normFacets,
		Components: normComponents,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.SaveNote(repoDir, n); err != nil {
		return store.Note{}, err
	}
	if err := writeNoteBody(repoDir, id, title, body); err != nil {
		return store.Note{}, err
	}
	ev := domain.NewEvent(id, "note.created", map[string]string{"title": title}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return store.Note{}, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: create %s (%s)", id, title)); err != nil {
		return store.Note{}, err
	}
	return n, nil
}

// TransitionNote supersedes note id by note `by` in the backend clone at
// repoDir. Unlike a gated entity, `by` is OPTIONAL: with it, the successor note
// replaces id; without it, id is merely retired (still moved to superseded, no
// successor). It validates the flags, loads id (errors when missing, when by is
// the note itself, or when id is already terminal), validates by exists when
// given, applies the domain transition, persists state+superseded_by+reason,
// appends the note.superseded event and syncs the backend + docs.lock in projDir.
func TransitionNote(repoDir, projDir, id, by, reason string) (NoteOutcome, error) {
	var out NoteOutcome
	if strings.TrimSpace(reason) == "" {
		return out, fmt.Errorf("--reason é obrigatório")
	}
	n, err := store.LoadNote(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return out, fmt.Errorf("nota %s não encontrada no backend", id)
	}
	if err != nil {
		return out, err
	}
	if by != "" {
		if by == id {
			return out, fmt.Errorf("--by não pode ser a própria nota (%s)", id)
		}
		if _, err := store.LoadNote(repoDir, by); errors.Is(err, store.ErrNotFound) {
			return out, fmt.Errorf("nota %s (--by) não encontrada no backend", by)
		} else if err != nil {
			return out, err
		}
	}

	newState, err := domain.TransitionNote(n.State, domain.ActionSupersede)
	if err != nil {
		return out, err
	}
	oldState := n.State
	n.State = newState
	n.SupersededBy = by
	n.Reason = reason
	n.UpdatedAt = time.Now().UTC()
	if err := store.SaveNote(repoDir, n); err != nil {
		return out, err
	}
	ev := domain.NewEvent(id, "note.superseded", map[string]string{"by": by, "reason": reason}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return out, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: supersede %s", id)); err != nil {
		return out, err
	}
	return NoteOutcome{ID: id, From: oldState, To: newState}, nil
}

// KeepNote marks note id as durable in the backend clone at repoDir: the note
// has been reviewed as durable team knowledge, so lint's stale-note rule no
// longer flags it even when every --informs target has concluded. It requires an
// active note (a superseded one is history), is idempotent (a no-op with no new
// commit when already durable), appends the note.kept event and syncs the backend
// + docs.lock in projDir.
func KeepNote(repoDir, projDir, id string) (store.Note, error) {
	n, err := store.LoadNote(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Note{}, fmt.Errorf("nota %s não encontrada no backend", id)
	}
	if err != nil {
		return store.Note{}, err
	}
	if n.State != domain.NoteActive {
		return store.Note{}, fmt.Errorf("nota %s não está active (está %s) — keep só vale para nota ativa", id, n.State)
	}
	if n.Durable {
		return n, nil // idempotent: already durable, nothing to commit
	}
	n.Durable = true
	n.UpdatedAt = time.Now().UTC()
	if err := store.SaveNote(repoDir, n); err != nil {
		return store.Note{}, err
	}
	ev := domain.NewEvent(id, "note.kept", map[string]string{}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return store.Note{}, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: keep %s (durable)", id)); err != nil {
		return store.Note{}, err
	}
	return n, nil
}

// NoteDocPath returns the backend path of a note's content document.
func NoteDocPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "notes", id+".md")
}

// writeNoteBody writes content/notes/<id>.md as cerne's stamped cerne_id
// frontmatter followed by the body. A note is freeform (no template), so an
// empty body falls back to writeNoteStub: a minimal title scaffold that
// doccheck reads as a stub until the author fills it.
func writeNoteBody(repoDir, id, title, body string) error {
	if strings.TrimSpace(body) == "" {
		return writeNoteStub(repoDir, id, title)
	}
	path := NoteDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// writeNoteStub creates content/notes/<id>.md with the minimal scaffold for a
// freeform note: the title heading plus an HTML-comment prompt. doccheck treats
// the heading and comment as scaffold, so the stub reads as pending (no real
// content) until the author writes the knowledge.
func writeNoteStub(repoDir, id, title string) error {
	path := NoteDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n# " + strings.TrimSpace(title) + "\n\n" +
		"<!-- Registre aqui o conhecimento durável: a gotcha, o invariante, a convenção tácita " +
		"e o porquê. Texto livre — sem seções obrigatórias. Substitua esta linha. -->\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

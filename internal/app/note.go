package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
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
//
// A live note with a similar title refuses the creation (F-0324, same guard as
// feature new); force overrides it and records guard=forced on note.created.
//
// durable (F-0249) marks the note as reviewed-durable at birth, so lint's
// stale-note never flags it: knowledge that does not expire with a feature
// (domain vocabulary, rejected scope) would otherwise need a second KeepNote —
// a whole extra commit+push to set one bool.
func CreateNote(repoDir, projDir, title, body string, informs, facets, components []string, durable, force bool) (store.Note, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return store.Note{}, fmt.Errorf("the note title cannot be empty")
	}
	// Validate/normalize tags before any side effect (NextID, save): an unknown
	// facet must abort without minting an id or writing a file.
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Note{}, err
	}
	normComponents := domain.NormalizeComponents(components)
	// Reads, id mint and writes run inside the backend lock's critical section
	// (F-0122): concurrent creations serialize instead of minting the same id.
	var n store.Note
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		cleaned, err := validateInforms(repoDir, informs)
		if err != nil {
			return "", err
		}
		// Duplicate guard (F-0324): before the id mint and any write.
		forced, err := guardNote(repoDir, title, tagSet{normFacets, normComponents, cleaned}, force)
		if err != nil {
			return "", err
		}
		id, err := store.NextID(repoDir, "N")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		n = store.Note{
			ID:         id,
			Title:      title,
			State:      domain.NoteActive,
			Informs:    cleaned,
			Facets:     normFacets,
			Components: normComponents,
			Durable:    durable,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := store.SaveNote(repoDir, n); err != nil {
			return "", err
		}
		if err := writeNoteBody(repoDir, id, title, body); err != nil {
			return "", err
		}
		payload := map[string]string{"title": title}
		if forced {
			payload["guard"] = "forced"
		}
		ev := domain.NewEvent(id, "note.created", payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: create %s (%s)", id, title), nil
	})
	if err != nil {
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
		return out, fmt.Errorf("--reason is required")
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		n, err := store.LoadNote(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("note %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if by != "" {
			if by == id {
				return "", fmt.Errorf("--by cannot be the note itself (%s)", id)
			}
			if _, err := store.LoadNote(repoDir, by); errors.Is(err, store.ErrNotFound) {
				return "", fmt.Errorf("note %s (--by) not found in backend", by)
			} else if err != nil {
				return "", err
			}
		}

		newState, err := domain.TransitionNote(n.State, domain.ActionSupersede)
		if err != nil {
			return "", err
		}
		oldState := n.State
		n.State = newState
		n.SupersededBy = by
		n.Reason = reason
		n.UpdatedAt = time.Now().UTC()
		if err := store.SaveNote(repoDir, n); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "note.superseded", map[string]string{"by": by, "reason": reason}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		out = NoteOutcome{ID: id, From: oldState, To: newState}
		return fmt.Sprintf("cerne: supersede %s", id), nil
	})
	if err != nil {
		return NoteOutcome{}, err
	}
	return out, nil
}

// KeepNote marks note id as durable in the backend clone at repoDir: the note
// has been reviewed as durable team knowledge, so lint's stale-note rule no
// longer flags it even when every --informs target has concluded. It requires an
// active note (a superseded one is history), is idempotent (a no-op with no new
// commit when already durable), appends the note.kept event and syncs the backend
// + docs.lock in projDir.
func KeepNote(repoDir, projDir, id string) (store.Note, error) {
	var n store.Note
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		n, err = store.LoadNote(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("note %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if n.State != domain.NoteActive {
			return "", fmt.Errorf("note %s is not active (it is %s) — keep only applies to an active note", id, n.State)
		}
		if n.Durable {
			return "", nil // idempotent: writes nothing, surfaces as ErrNothingToCommit
		}
		n.Durable = true
		n.UpdatedAt = time.Now().UTC()
		if err := store.SaveNote(repoDir, n); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "note.kept", map[string]string{}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: keep %s (durable)", id), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return n, nil // already durable: a successful no-op
	}
	if err != nil {
		return store.Note{}, err
	}
	return n, nil
}

// RetagNote sets the tag axes of an active note in the backend clone at repoDir.
// Each axis is replaced only when its set flag is true, so a caller can update
// facets without touching components (or clear an axis by passing an empty slice
// with set=true); state, durable, informs and the body are preserved. Facets are
// validated against the closed set before any write, so an unknown facet aborts
// leaving the note untouched. Appends note.retagged and syncs the backend +
// docs.lock in projDir.
func RetagNote(repoDir, projDir, id string, facets, components []string, setFacets, setComponents bool) (store.Note, error) {
	var n store.Note
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		n, err = store.LoadNote(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("note %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if n.State != domain.NoteActive {
			return "", fmt.Errorf("note %s is not active (it is %s) — retag only applies to an active note", id, n.State)
		}
		if setFacets {
			nf, err := domain.NormalizeFacets(facets)
			if err != nil {
				return "", err // unknown facet: note untouched
			}
			n.Facets = nf
		}
		if setComponents {
			n.Components = domain.NormalizeComponents(components)
		}
		n.UpdatedAt = time.Now().UTC()
		if err := store.SaveNote(repoDir, n); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "note.retagged", map[string]string{
			"facets":     strings.Join(n.Facets, ","),
			"components": strings.Join(n.Components, ","),
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: retag %s", id), nil
	})
	if err != nil {
		return store.Note{}, err
	}
	return n, nil
}

// TagCatalog is the tag menu across active notes: each distinct facet and
// component with the count of active notes carrying it. It is the surface an
// agent reads to self-select which conventions/knowledge to pull
// (cerne note tags).
type TagCatalog struct {
	Facets     map[string]int `json:"facets"`
	Components map[string]int `json:"components"`
}

// NoteTagCatalog aggregates the tag catalog over ACTIVE notes in the backend
// clone at repoDir. A superseded note's tags are history and are not counted.
// Maps are always non-nil so the JSON encoding is {} rather than null.
func NoteTagCatalog(repoDir string) (TagCatalog, error) {
	cat := TagCatalog{Facets: map[string]int{}, Components: map[string]int{}}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return TagCatalog{}, err
	}
	for _, n := range notes {
		if n.State != domain.NoteActive {
			continue
		}
		for _, f := range n.Facets {
			cat.Facets[f]++
		}
		for _, c := range n.Components {
			cat.Components[c]++
		}
	}
	return cat, nil
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
		"<!-- Record the durable knowledge here: the gotcha, the invariant, the tacit convention " +
		"and the why. Free text — no required sections. Replace this line. -->\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

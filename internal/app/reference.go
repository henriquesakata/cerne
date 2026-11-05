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

// ReferenceDocPath returns the backend path of a reference's content document.
func ReferenceDocPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "references", id+".md")
}

// CreateExternalReference registers an external source (article/doc) as a
// reference in the backend clone at repoDir: validates title/url and that each
// informs target (F-/D-) exists, allocates the next R- id, persists the entity
// with URL/External/Informs, writes the pasted content as the reference's
// content doc (indexed, so ask can cite it), appends reference.created, and
// commits+pushes the backend refreshing the docs.lock snapshot in projDir. A
// live reference with the same normalized URL refuses the creation (F-0324);
// force overrides it and records guard=forced on reference.created.
func CreateExternalReference(repoDir, projDir, title, url, content string, informs, facets, components []string, force bool) (store.Reference, error) {
	title = strings.TrimSpace(title)
	url = strings.TrimSpace(url)
	if title == "" {
		return store.Reference{}, fmt.Errorf("title cannot be empty")
	}
	if url == "" {
		return store.Reference{}, fmt.Errorf("url cannot be empty (external reference)")
	}
	// Normalize tags before allocating an id, so an unknown facet aborts the
	// whole create without consuming an R- id or writing anything (mirrors notes).
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Reference{}, err
	}
	normComponents := domain.NormalizeComponents(components)
	// Reads, id mint and writes run inside the backend lock's critical section
	// (F-0122): concurrent creations serialize instead of minting the same id.
	var r store.Reference
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		cleaned, err := validateInforms(repoDir, informs)
		if err != nil {
			return "", err
		}
		// Duplicate guard (F-0324): a reference is its source, so only a live
		// reference with the same normalized URL collides (never the title).
		forced, err := guardReference(repoDir, url, tagSet{normFacets, normComponents, cleaned}, force)
		if err != nil {
			return "", err
		}
		id, err := store.NextID(repoDir, "R")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		r = store.Reference{
			ID:         id,
			Title:      title,
			State:      domain.ReferenceCurrent,
			URL:        url,
			External:   true,
			Informs:    cleaned,
			Facets:     normFacets,
			Components: normComponents,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := store.SaveReference(repoDir, r); err != nil {
			return "", err
		}
		if err := writeReferenceDoc(repoDir, id, title, content); err != nil {
			return "", err
		}
		payload := map[string]string{"url": url, "source": "external"}
		if forced {
			payload["guard"] = "forced"
		}
		ev := domain.NewEvent(id, "reference.created", payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: create %s (%s)", id, title), nil
	})
	if err != nil {
		return store.Reference{}, err
	}
	return r, nil
}

// RetagReference sets the tag axes of a current reference in the backend clone
// at repoDir. Each axis is replaced only when its set flag is true, so a caller
// can update facets without touching components (or clear an axis by passing an
// empty slice with set=true); state, url, external, informs and the content doc
// are preserved. Facets are validated against the closed set before any write,
// so an unknown facet aborts leaving the reference untouched. Appends
// reference.retagged and syncs the backend + docs.lock in projDir.
func RetagReference(repoDir, projDir, id string, facets, components []string, setFacets, setComponents bool) (store.Reference, error) {
	var r store.Reference
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		r, err = store.LoadReference(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("reference %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if r.State != domain.ReferenceCurrent {
			return "", fmt.Errorf("reference %s is not current (it is %s) — retag only applies to a current reference", id, r.State)
		}
		if setFacets {
			nf, err := domain.NormalizeFacets(facets)
			if err != nil {
				return "", err // unknown facet: reference untouched
			}
			r.Facets = nf
		}
		if setComponents {
			r.Components = domain.NormalizeComponents(components)
		}
		r.UpdatedAt = time.Now().UTC()
		if err := store.SaveReference(repoDir, r); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "reference.retagged", map[string]string{
			"facets":     strings.Join(r.Facets, ","),
			"components": strings.Join(r.Components, ","),
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: retag %s", id), nil
	})
	if err != nil {
		return store.Reference{}, err
	}
	return r, nil
}

// validateInforms trims, dedupes (preserving order) and checks each backlink
// target: it must be a feature (F-), decision (D-), runbook (RB-) or note (N-)
// that exists in the backend, so no dangling backlink is stored (F-0108 added
// runbooks/notes — a source may inform an operational procedure or a piece of
// tacit knowledge, not only features/decisions). Returns the cleaned list (nil
// when empty).
func validateInforms(repoDir string, informs []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range informs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		switch {
		case domain.IsFeatureID(id):
			if _, err := store.LoadFeature(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--informs: feature %s not found in backend", id)
			} else if err != nil {
				return nil, err
			}
		case strings.HasPrefix(id, "D-"):
			if _, err := store.LoadDecision(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--informs: decision %s not found in backend", id)
			} else if err != nil {
				return nil, err
			}
		case strings.HasPrefix(id, "RB-"):
			if _, err := store.LoadRunbook(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--informs: runbook %s not found in backend", id)
			} else if err != nil {
				return nil, err
			}
		case strings.HasPrefix(id, "N-"):
			if _, err := store.LoadNote(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--informs: note %s not found in backend", id)
			} else if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("--informs: %q must be a feature (F-), decision (D-), runbook (RB-) or note (N-)", id)
		}
		out = append(out, id)
	}
	return out, nil
}

// writeReferenceDoc writes content/references/<id>.md with the pasted source
// text (cerne_id stamped). Empty content yields a minimal title stub.
func writeReferenceDoc(repoDir, id, title, content string) error {
	path := ReferenceDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := strings.TrimSpace(content)
	if body == "" {
		body = "# " + title
	}
	doc := fmt.Sprintf("---\ncerne_id: %s\n---\n\n%s\n", id, body)
	return os.WriteFile(path, []byte(doc), 0o644)
}

// ReferencesInforming returns the references whose Informs backlink contains
// entityID — the reverse lookup powering "Fontes" on a feature/decision page.
func ReferencesInforming(repoDir, entityID string) ([]store.Reference, error) {
	all, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	var out []store.Reference
	for _, r := range all {
		for _, id := range r.Informs {
			if id == entityID {
				out = append(out, r)
				break
			}
		}
	}
	return out, nil
}

// ReferenceOutcome reports a reference transition that was applied and synced.
type ReferenceOutcome struct {
	ID   string
	From domain.ReferenceState
	To   domain.ReferenceState
}

// RetireReference moves a current reference to obsolete (history, still
// searchable) — the remedy for a duplicate or a dead source (F-0324), parity
// with note retire. The reason is required and recorded on reference.obsoleted;
// an already-obsolete reference refuses with domain.ErrInvalidTransition.
func RetireReference(repoDir, projDir, id, reason string) (ReferenceOutcome, error) {
	var out ReferenceOutcome
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return out, fmt.Errorf("--reason is required")
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		r, err := store.LoadReference(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("reference %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		to, err := domain.TransitionReference(r.State, domain.ActionObsolete)
		if err != nil {
			return "", err
		}
		out = ReferenceOutcome{ID: id, From: r.State, To: to}
		r.State = to
		r.UpdatedAt = time.Now().UTC()
		if err := store.SaveReference(repoDir, r); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "reference.obsoleted", map[string]string{"reason": reason}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: retire %s", id), nil
	})
	if err != nil {
		return ReferenceOutcome{}, err
	}
	return out, nil
}

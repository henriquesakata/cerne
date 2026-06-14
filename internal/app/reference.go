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
// commits+pushes the backend refreshing the docs.lock snapshot in projDir.
func CreateExternalReference(repoDir, projDir, title, url, content string, informs []string) (store.Reference, error) {
	title = strings.TrimSpace(title)
	url = strings.TrimSpace(url)
	if title == "" {
		return store.Reference{}, fmt.Errorf("título não pode ser vazio")
	}
	if url == "" {
		return store.Reference{}, fmt.Errorf("url não pode ser vazia (referência externa)")
	}
	cleaned, err := validateInforms(repoDir, informs)
	if err != nil {
		return store.Reference{}, err
	}
	id, err := store.NextID(repoDir, "R")
	if err != nil {
		return store.Reference{}, err
	}
	now := time.Now().UTC()
	r := store.Reference{
		ID:        id,
		Title:     title,
		State:     domain.ReferenceCurrent,
		URL:       url,
		External:  true,
		Informs:   cleaned,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveReference(repoDir, r); err != nil {
		return store.Reference{}, err
	}
	if err := writeReferenceDoc(repoDir, id, title, content); err != nil {
		return store.Reference{}, err
	}
	ev := domain.NewEvent(id, "reference.created", map[string]string{"url": url, "source": "external"}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return store.Reference{}, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: create %s (%s)", id, title)); err != nil {
		return store.Reference{}, err
	}
	return r, nil
}

// validateInforms trims, dedupes (preserving order) and checks each backlink
// target: it must be a feature (F-) or decision (D-) that exists in the
// backend, so no dangling backlink is stored. Returns the cleaned list (nil
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
		case strings.HasPrefix(id, "F-"):
			if _, err := store.LoadFeature(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--informs: feature %s não encontrada no backend", id)
			} else if err != nil {
				return nil, err
			}
		case strings.HasPrefix(id, "D-"):
			if _, err := store.LoadDecision(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--informs: decisão %s não encontrada no backend", id)
			} else if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("--informs: %q deve ser uma feature (F-) ou decisão (D-)", id)
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

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrUnknownDoc is returned by SaveFeatureDoc for a doc name outside the
// spec/plan/result lifecycle set.
var ErrUnknownDoc = errors.New("documento de feature desconhecido")

// ErrParentNotFound is returned when a feature's parent (epic) does not exist.
var ErrParentNotFound = errors.New("feature-épico não encontrada")

// ErrParentTerminal is returned when the parent (epic) is in a terminal state
// (superseded/rejected): new work is not filed under a dead epic.
var ErrParentTerminal = errors.New("a feature-épico está encerrada")

// featureDocFilenames maps the API doc name to its backend filename.
var featureDocFilenames = map[string]string{
	"spec":   "spec.md",
	"plan":   "plan.md",
	"result": "result.md",
}

// CreateFeature creates a candidate feature with a spec stub, appends the
// feature.created event, commits+pushes the backend and refreshes the
// docs.lock snapshot. The shared core behind `cerne feature new` (CLI) and
// POST /api/features (server). Module rules are enforced (see ValidateModule).
func CreateFeature(repoDir, projDir, title, module, parent string) (store.Feature, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return store.Feature{}, fmt.Errorf("o título da feature não pode ser vazio")
	}
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		return store.Feature{}, err
	}
	if err := ValidateModule(module, proj.Modules); err != nil {
		return store.Feature{}, err
	}
	id, err := store.NextID(repoDir, "F")
	if err != nil {
		return store.Feature{}, err
	}
	if parent != "" {
		if err := validateParent(repoDir, id, parent); err != nil {
			return store.Feature{}, err
		}
	}
	now := time.Now().UTC()
	f := store.Feature{
		ID:        id,
		Title:     title,
		Module:    module,
		Parent:    parent,
		State:     domain.StateCandidate,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveFeature(repoDir, f); err != nil {
		return store.Feature{}, err
	}
	if err := writeSpecStub(repoDir, id, title); err != nil {
		return store.Feature{}, err
	}
	payload := map[string]string{"title": title, "module": module}
	if parent != "" {
		payload["parent"] = parent
	}
	ev := domain.NewEvent(id, "feature.created", payload, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return store.Feature{}, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: create %s (%s)", id, title)); err != nil {
		return store.Feature{}, err
	}
	return f, nil
}

// ValidateModule enforces the project module rules: with 2+ declared modules a
// non-empty module is required; whenever the project declares modules, a
// non-empty module must be one of them.
func ValidateModule(module string, modules []string) error {
	if len(modules) >= 2 && module == "" {
		return fmt.Errorf("--module é obrigatório neste projeto (módulos: %s)", strings.Join(modules, ", "))
	}
	if module != "" && len(modules) > 0 && !slices.Contains(modules, module) {
		return fmt.Errorf("módulo %q não existe no projeto (módulos: %s)", module, strings.Join(modules, ", "))
	}
	return nil
}

// validateParent checks that a non-empty parent is a usable epic for childID:
// it exists (ErrParentNotFound), is not terminal (ErrParentTerminal), and the
// link introduces no cycle (domain.ErrParentSelf/ErrParentCycle). childID is the
// feature being (re)parented — pass the freshly minted ID on create, where a
// cycle is impossible but the check stays uniform.
func validateParent(repoDir, childID, parent string) error {
	p, err := store.LoadFeature(repoDir, parent)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrParentNotFound, parent)
	}
	if err != nil {
		return err
	}
	if p.State == domain.StateSuperseded || p.State == domain.StateRejected {
		return fmt.Errorf("%w: %s (%s)", ErrParentTerminal, parent, p.State)
	}
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return err
	}
	parentOf := make(map[string]string, len(feats))
	for _, f := range feats {
		if f.Parent != "" {
			parentOf[f.ID] = f.Parent
		}
	}
	if err := domain.ValidateParentLink(childID, parent, parentOf); err != nil {
		return fmt.Errorf("vínculo de épico inválido: %w", err)
	}
	return nil
}

// SetFeatureParent reparents a feature under the epic parent (or clears it when
// parent is empty), appends a feature.reparented event, then commits, pushes
// and refreshes the docs.lock snapshot. A no-op when the parent is unchanged.
// store.ErrNotFound (wrapped) for an unknown feature; the validateParent
// sentinels for an invalid link.
func SetFeatureParent(repoDir, projDir, id, parent string) error {
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return err
	}
	if f.Parent == parent {
		return nil // unchanged: nothing to commit
	}
	if parent != "" {
		if err := validateParent(repoDir, id, parent); err != nil {
			return err
		}
	}
	from := f.Parent
	f.Parent = parent
	f.UpdatedAt = time.Now().UTC()
	if err := store.SaveFeature(repoDir, f); err != nil {
		return err
	}
	ev := domain.NewEvent(id, "feature.reparented", map[string]string{"from": from, "to": parent}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return err
	}
	msg := fmt.Sprintf("cerne: reparent %s to %s", id, parent)
	if parent == "" {
		msg = fmt.Sprintf("cerne: clear parent of %s", id)
	}
	return CommitAndSnapshot(repoDir, projDir, msg)
}

// SaveFeatureDoc writes the markdown content of a feature lifecycle document
// (doc ∈ spec|plan|result) to content/features/<id>/<doc>.md, then commits,
// pushes and refreshes the docs.lock snapshot. A byte-identical write is a
// successful no-op (like SaveArtifact). ErrUnknownDoc for an invalid doc name;
// store.ErrNotFound (wrapped) when the feature does not exist.
func SaveFeatureDoc(repoDir, projDir, id, doc, content string) error {
	filename, ok := featureDocFilenames[doc]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownDoc, doc)
	}
	if _, err := store.LoadFeature(repoDir, id); err != nil {
		return err
	}
	path := FeatureDocPath(repoDir, id, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: update %s/%s", id, filename))
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // backend already had exactly this content
	}
	return err
}

// writeSpecStub creates content/features/<id>/spec.md with the placeholder
// body the approve gate recognizes as "spec still pending".
func writeSpecStub(repoDir, id, title string) error {
	path := FeatureDocPath(repoDir, id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("---\ncerne_id: %s\n---\n\n# %s\n\n> Spec pendente — escreva o problema, objetivo e critérios de aceite.\n", id, title)
	return os.WriteFile(path, []byte(content), 0o644)
}

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// CreateFeature creates a candidate feature with a spec stub, appends the
// feature.created event, commits+pushes the backend and refreshes the
// docs.lock snapshot. The shared core behind `cerne feature new` (CLI) and
// POST /api/features (server). Module rules are enforced (see ValidateModule).
func CreateFeature(repoDir, projDir, title, module string) (store.Feature, error) {
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
	now := time.Now().UTC()
	f := store.Feature{
		ID:        id,
		Title:     title,
		Module:    module,
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
	ev := domain.NewEvent(id, "feature.created", map[string]string{"title": title, "module": module}, EventActor())
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

package app

import (
	"fmt"
	"os"
	"time"

	"gitlab.com/rockt/cerne/internal/classify"
	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// featureClassifiedEvent is appended whenever a module is assigned to a feature
// (heuristic backfill or manual single assignment).
const featureClassifiedEvent = "feature.classified"

// Assignment is one module-less feature that the heuristic mapped to a module.
type Assignment struct {
	ID     string
	Module string
}

// ClassifyBackfill assigns a module to every module-less feature whose folder
// (imported_from, read from the spec.md frontmatter) and content tags point to
// exactly one declared module. It only ever touches features with an empty
// Module — already-classified features are left untouched (idempotent).
//
// assigned lists the single-match features in feature-ID order; skipped counts
// the module-less features the heuristic could not resolve (zero or ambiguous
// signal). When apply is false (dry-run) it computes both and writes nothing.
// When apply is true it sets Module, appends a feature.classified event
// (source "heuristic") per assignment, then makes ONE commit+push and refreshes
// the docs.lock snapshot — only when at least one feature was assigned.
func ClassifyBackfill(repoDir, projDir string, apply bool) (assigned []Assignment, skipped int, err error) {
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		return nil, 0, err
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, 0, err
	}

	for _, f := range features {
		if f.Module != "" {
			continue // never reclassify an already-moduled feature
		}
		importedFrom, content := featureSpecSignal(repoDir, f.ID)
		module, ok := classify.InferModule(importedFrom, content, proj.Modules)
		if !ok {
			skipped++
			continue
		}
		assigned = append(assigned, Assignment{ID: f.ID, Module: module})
		if apply {
			if err := assignFeatureModule(repoDir, f, module, "heuristic"); err != nil {
				return nil, 0, err
			}
		}
	}

	if apply && len(assigned) > 0 {
		msg := fmt.Sprintf("cerne: classify %d feature(s)", len(assigned))
		if err := CommitAndSnapshot(repoDir, projDir, msg); err != nil {
			return nil, 0, err
		}
	}
	return assigned, skipped, nil
}

// ClassifyFeatureManually assigns module to feature id explicitly, bypassing the
// heuristic: it validates module against the declared modules and id against the
// backend, sets Module (reclassification is allowed in manual mode), appends a
// feature.classified event (source "manual"), then commits+pushes and refreshes
// the lock. Unknown module or id yields a pt-BR error and writes nothing.
func ClassifyFeatureManually(repoDir, projDir, id, module string) error {
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		return err
	}
	if !moduleDeclared(proj.Modules, module) {
		return fmt.Errorf("módulo desconhecido: %q (módulos declarados: %s)", module, joinModules(proj.Modules))
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return fmt.Errorf("feature %s não encontrada no backend", id)
	}
	if err := assignFeatureModule(repoDir, f, module, "manual"); err != nil {
		return err
	}
	return CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: classify %s", id))
}

// assignFeatureModule sets f.Module, persists the feature and appends the
// feature.classified event with the given source.
func assignFeatureModule(repoDir string, f store.Feature, module, source string) error {
	f.Module = module
	f.UpdatedAt = time.Now().UTC()
	if err := store.SaveFeature(repoDir, f); err != nil {
		return err
	}
	payload := map[string]string{"module": module, "source": source}
	return store.AppendEvent(repoDir, domain.NewEvent(f.ID, featureClassifiedEvent, payload, EventActor()))
}

// featureSpecSignal returns the imported_from path (from the spec.md
// frontmatter the importer injects) and the full spec.md content for inference;
// both empty when the spec is absent.
func featureSpecSignal(repoDir, id string) (importedFrom, content string) {
	data, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		return "", ""
	}
	return doccheck.FrontmatterValue(data, "imported_from"), string(data)
}

// moduleDeclared reports whether name is one of the declared modules.
func moduleDeclared(modules []string, name string) bool {
	for _, m := range modules {
		if m == name {
			return true
		}
	}
	return false
}

// joinModules renders the declared modules for an error message.
func joinModules(modules []string) string {
	if len(modules) == 0 {
		return "(nenhum)"
	}
	out := modules[0]
	for _, m := range modules[1:] {
		out += ", " + m
	}
	return out
}

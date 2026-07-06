package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/plantmpl"
	"gitlab.com/rockt/cerne/internal/restmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrUnknownDoc is returned by SaveFeatureDoc for a doc name outside the
// spec/plan/result lifecycle set.
var ErrUnknownDoc = errors.New("documento de feature desconhecido")

// ErrParentNotFound is returned when a feature's parent (epic) does not exist.
var ErrParentNotFound = errors.New("parent feature (epic) not found")

// ErrParentTerminal is returned when the parent (epic) is in a terminal state
// (superseded/rejected): new work is not filed under a dead epic.
var ErrParentTerminal = errors.New("the parent feature (epic) is closed")

// ErrTitleFrozen is returned when renaming a feature in a terminal state
// (implemented/rejected/superseded): its title is frozen history.
var ErrTitleFrozen = errors.New("the feature's title is frozen")

// ErrDuplicateTitle is returned when creating a feature whose normalized title
// (domain.NormalizeTitle, the F-0045 rule) matches a live feature: the usual
// cause is a retry of a creation that failed after the entity already reached
// the backend (F-0121). Terminal states never block — history is not a veto.
var ErrDuplicateTitle = errors.New("a live feature with this title already exists")

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
// facets/components tag the feature on the shared D-0002 axes (facet closed-set,
// component curated), extended to features by D-0003 — validated before any side
// effect so an unknown facet aborts without minting an id or writing a file.
func CreateFeature(repoDir, projDir, title, module, parent string, facets, components []string) (store.Feature, error) {
	return CreateFeatureWithSpec(repoDir, projDir, title, module, parent, facets, components, "")
}

// CreateFeatureWithSpec is CreateFeature with the one-shot spec path (F-0120):
// a non-empty specBody is stamped with the cerne_id frontmatter and written as
// the real spec — the feature is born with its context captured, nothing left
// to fill or push. An empty body scaffolds the usual stub.
func CreateFeatureWithSpec(repoDir, projDir, title, module, parent string, facets, components []string, specBody string) (store.Feature, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return store.Feature{}, fmt.Errorf("the feature title cannot be empty")
	}
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Feature{}, err
	}
	normComponents := domain.NormalizeComponents(components)
	// The whole creation — duplicate guard, id mint, entity+spec+event writes —
	// runs inside ONE critical section of the backend lock (F-0121): a
	// concurrent local process can neither sweep the half-written state into
	// its own commit nor mint the same id.
	var f store.Feature
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		proj, err := store.LoadProject(repoDir)
		if err != nil {
			return "", err
		}
		if err := ValidateModule(module, proj.Modules); err != nil {
			return "", err
		}
		if err := checkDuplicateTitle(repoDir, title); err != nil {
			return "", err
		}
		id, err := store.NextID(repoDir, "F")
		if err != nil {
			return "", err
		}
		if parent != "" {
			if err := validateParent(repoDir, id, parent); err != nil {
				return "", err
			}
		}
		now := time.Now().UTC()
		f = store.Feature{
			ID:         id,
			Title:      title,
			Module:     module,
			Parent:     parent,
			State:      domain.StateCandidate,
			Facets:     normFacets,
			Components: normComponents,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		if strings.TrimSpace(specBody) != "" {
			if err := writeSpecBody(repoDir, id, specBody); err != nil {
				return "", err
			}
		} else if err := writeSpecStub(repoDir, id, title); err != nil {
			return "", err
		}
		payload := map[string]string{"title": title, "module": module}
		if parent != "" {
			payload["parent"] = parent
		}
		ev := domain.NewEvent(id, "feature.created", payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: create %s (%s)", id, title), nil
	})
	if err != nil {
		return store.Feature{}, err
	}
	return f, nil
}

// RetagFeature sets the tag axes of a feature in the backend clone at repoDir.
// Each axis is replaced only when its set flag is true, so a caller can update
// facets without touching components (or clear an axis by passing an empty slice
// with set=true); every other field is preserved. Unlike a decision (retaggable
// only while active), a feature is retaggable in every state EXCEPT the dead
// history ones (superseded/rejected): the implemented backlog is living,
// searchable knowledge worth categorizing, so tagging it is allowed even though
// the untagged-live lint never nags about it. Facets are validated against the
// closed set before any write. Appends feature.retagged and syncs backend +
// docs.lock.
func RetagFeature(repoDir, projDir, id string, facets, components []string, setFacets, setComponents bool) (store.Feature, error) {
	var f store.Feature
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		f, err = store.LoadFeature(repoDir, id)
		if err != nil {
			return "", err
		}
		if f.State == domain.StateSuperseded || f.State == domain.StateRejected {
			return "", fmt.Errorf("feature %s is closed (%s) — retag only applies to a live or implemented feature", id, f.State)
		}
		if setFacets {
			nf, err := domain.NormalizeFacets(facets)
			if err != nil {
				return "", err // unknown facet: feature untouched
			}
			f.Facets = nf
		}
		if setComponents {
			f.Components = domain.NormalizeComponents(components)
		}
		f.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "feature.retagged", map[string]string{
			"facets":     strings.Join(f.Facets, ","),
			"components": strings.Join(f.Components, ","),
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: retag %s", id), nil
	})
	if err != nil {
		return store.Feature{}, err
	}
	return f, nil
}

// checkDuplicateTitle refuses a creation whose normalized title collides with
// a live (candidate/approved/in_development) feature — the F-0121 retry guard,
// same identity rule as the F-0045 import guard. Terminal features never block.
func checkDuplicateTitle(repoDir, title string) error {
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return err
	}
	norm := domain.NormalizeTitle(title)
	for _, f := range feats {
		switch f.State {
		case domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment:
			if domain.NormalizeTitle(f.Title) == norm {
				return fmt.Errorf("%w: %s (%s) — if this is a retry of a failed creation, the feature is already in the backend; advance the existing one or use a different title", ErrDuplicateTitle, f.ID, f.State)
			}
		}
	}
	return nil
}

// ValidateModule enforces the project module rules: with 2+ declared modules a
// non-empty module is required; whenever the project declares modules, a
// non-empty module must be one of them.
func ValidateModule(module string, modules []string) error {
	if len(modules) >= 2 && module == "" {
		return fmt.Errorf("--module is required in this project (modules: %s)", strings.Join(modules, ", "))
	}
	if module != "" && len(modules) > 0 && !slices.Contains(modules, module) {
		return fmt.Errorf("module %q does not exist in the project (modules: %s)", module, strings.Join(modules, ", "))
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
		return fmt.Errorf("invalid epic link: %w", err)
	}
	return nil
}

// SetFeatureParent reparents a feature under the epic parent (or clears it when
// parent is empty), appends a feature.reparented event, then commits, pushes
// and refreshes the docs.lock snapshot. A no-op when the parent is unchanged.
// store.ErrNotFound (wrapped) for an unknown feature; the validateParent
// sentinels for an invalid link.
func SetFeatureParent(repoDir, projDir, id, parent string) error {
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			return "", err
		}
		if f.Parent == parent {
			return "", nil // unchanged: writes nothing, surfaces as ErrNothingToCommit
		}
		if parent != "" {
			if err := validateParent(repoDir, id, parent); err != nil {
				return "", err
			}
		}
		from := f.Parent
		f.Parent = parent
		f.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "feature.reparented", map[string]string{"from": from, "to": parent}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("cerne: reparent %s to %s", id, parent)
		if parent == "" {
			msg = fmt.Sprintf("cerne: clear parent of %s", id)
		}
		return msg, nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // unchanged parent: a successful no-op
	}
	return err
}

// RenameFeature changes a live feature's title, appends a feature.renamed event,
// then commits, pushes and refreshes the docs.lock snapshot. A no-op (returns
// nil, nothing committed) when the trimmed title is unchanged. The title is
// data, mutable only while the feature is live: ErrTitleFrozen (wrapped) for a
// terminal state (implemented/rejected/superseded). An empty/whitespace title is
// rejected; store.ErrNotFound (wrapped) for an unknown feature.
func RenameFeature(repoDir, projDir, id, newTitle string) error {
	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" {
		return fmt.Errorf("the feature title cannot be empty")
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			return "", err
		}
		if f.Title == newTitle {
			return "", nil // unchanged: writes nothing, surfaces as ErrNothingToCommit
		}
		if !domain.TitleEditable(f.State) {
			return "", fmt.Errorf("%w: %s (%s)", ErrTitleFrozen, id, f.State)
		}
		from := f.Title
		f.Title = newTitle
		f.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "feature.renamed", map[string]string{"from": from, "to": newTitle}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: rename %s", id), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // unchanged title: a successful no-op
	}
	return err
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
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		if _, err := store.LoadFeature(repoDir, id); err != nil {
			return "", err
		}
		path := FeatureDocPath(repoDir, id, filename)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: update %s/%s", id, filename), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // backend already had exactly this content
	}
	return err
}

// writeSpecBody creates content/features/<id>/spec.md as cerne's stamped
// cerne_id frontmatter followed by the provided body — the one-shot spec of
// CreateFeatureWithSpec, mirroring writeNoteBody.
func writeSpecBody(repoDir, id, body string) error {
	path := FeatureDocPath(repoDir, id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// writeSpecStub creates content/features/<id>/spec.md scaffolded from the
// canonical spec template (spectmpl): the section skeleton with inline
// guidance. The approve gate (doccheck) treats that scaffold as "spec still
// pending" until the author fills a section.
func writeSpecStub(repoDir, id, title string) error {
	path := FeatureDocPath(repoDir, id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + spectmpl.Stub(title)
	return os.WriteFile(path, []byte(content), 0o644)
}

// writePlanStub creates content/features/<id>/plan.md scaffolded from the
// canonical plan template (plantmpl) — the execution counterpart of
// writeSpecStub. It runs when a feature is approved (the plan phase begins), so
// the author lands on a structured plan to fill rather than a blank file. It is
// a no-op when a real (non-stub) plan already exists, so it never clobbers work.
// The start gate (doccheck) treats the fresh scaffold as "plan still pending"
// until a section is filled.
func writePlanStub(repoDir, id, title string) error {
	path := FeatureDocPath(repoDir, id, "plan.md")
	data, err := os.ReadFile(path)
	if err == nil && doccheck.HasRealContent(data) {
		return nil // a real plan already exists — don't overwrite it
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + plantmpl.Stub(title)
	return os.WriteFile(path, []byte(content), 0o644)
}

// writeResultStub creates content/features/<id>/result.md scaffolded from the
// canonical result template (restmpl). It runs when a feature starts development
// (the phase where the result is authored), so the author lands on a structured
// closeout to fill rather than a blank file — the counterpart of writePlanStub
// at approve. It is a no-op when a real result already exists, so it never
// clobbers work. The complete gate (doccheck) treats the fresh scaffold as
// "result still pending" until a section is filled.
func writeResultStub(repoDir, id, title string) error {
	path := FeatureDocPath(repoDir, id, "result.md")
	data, err := os.ReadFile(path)
	if err == nil && doccheck.HasRealContent(data) {
		return nil // a real result already exists — don't overwrite it
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + restmpl.Stub(title)
	return os.WriteFile(path, []byte(content), 0o644)
}

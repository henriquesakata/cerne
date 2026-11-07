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
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/plantmpl"
	"gitlab.com/rockt/cerne/internal/restmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrUnknownDoc is returned by SaveFeatureDoc for a doc name outside the
// spec/plan/result lifecycle set.
var ErrUnknownDoc = errors.New("documento de feature desconhecido")

// ErrNotCandidate is returned by the Backlog chat's refine path (F-0256) and
// 'cerne feature spec' for a feature past approval: in development the spec is
// the contract being built, not a chat draft; the refusal names the state's
// path (DocFixPhrase). An approved spec stays open to the chat (F-0318, revoking
// F-0262's chat restriction for approved only) — rewriting it stales the
// approval like any other write.
var ErrNotCandidate = errors.New("the feature is past approval")

// ErrParentNotFound is returned when a feature's parent (epic) does not exist.
var ErrParentNotFound = errors.New("parent feature (epic) not found")

// ErrFixesNotFound is returned when --fixes names a feature that does not exist
// (F-0321): the correction link is refused before anything is written.
var ErrFixesNotFound = errors.New("the feature it fixes was not found")

// ErrFixesCycle is returned when --fixes names the feature itself, or the
// feature that already fixes it (F-0321): a correction link never loops.
var ErrFixesCycle = errors.New("a feature cannot fix itself or the feature that fixes it")

// ErrParentTerminal is returned when the parent (epic) is in a terminal state
// (superseded/rejected): new work is not filed under a dead epic.
var ErrParentTerminal = errors.New("the parent feature (epic) is closed")

// ErrParentRequired is returned when creating a task without a parent: a task
// is an execution unit of a PRD, never free-floating (F-0203).
var ErrParentRequired = errors.New("a task requires --parent (the F-/E- it decomposes)")

// ErrParentNotCommitted is returned when a task's parent is still a candidate:
// the task is born approved because the human gate was paid on the parent PRD,
// which therefore must be past its own candidacy (F-0203).
var ErrParentNotCommitted = errors.New("the parent feature is not committed yet (still candidate)")

// ErrTitleFrozen is returned when renaming a feature in a terminal state
// (implemented/rejected/superseded): its title is frozen history.
var ErrTitleFrozen = errors.New("the feature's title is frozen")

// ErrFeatureFrozen is returned for a feature past the live states
// (implemented/done/rejected/superseded, domain.TitleEditable's definition of
// terminal): by SetFeature (owner/priority), and by every document write of the
// feature — SaveFeatureDoc and the draft commands (CheckDocWritable) and
// `cerne push` (F-0305), whose refusal names the state's path (DocFixPhrase).
var ErrFeatureFrozen = errors.New("the feature is closed")

// DocFixPhrase renders, in English, how document doc of feature id in state s
// is corrected — the sentence every closed-feature refusal carries (F-0305).
// The path itself comes from lint.Correction, the one table (F-0307).
func DocFixPhrase(id, doc string, s domain.FeatureState) string {
	kind, cmd := lint.Correction(s, id, doc)
	switch kind {
	case lint.CorrectByPush:
		return "edit it under .docs/ ('cerne pull') and run '" + cmd + "'"
	case lint.CorrectByAmend:
		return "correct it with '" + cmd + "'"
	case lint.CorrectByErrata:
		return "append an erratum with '" + cmd + "'"
	}
	return NoCorrectionNote(id)
}

// NoCorrectionNote is the sentence for a document no command changes
// (lint.CorrectNone: rejected, superseded) — terminal history (F-0307).
func NoCorrectionNote(id string) string {
	return fmt.Sprintf("no command changes it: terminal history ('cerne feature errata %s' only annotates)", id)
}

// CheckDocWritable loads feature id and returns it when its document doc may be
// written — only while the feature is live (domain.TitleEditable). Past that it
// returns ErrFeatureFrozen wrapped, naming the state's path (DocFixPhrase). The
// one rule every whole-document write routes through (F-0305): SaveFeatureDoc
// and the draft commands, which call it again inside the backend lock because
// their provider call runs outside it.
func CheckDocWritable(repoDir, id, doc string) (store.Feature, error) {
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return store.Feature{}, err
	}
	if !domain.TitleEditable(f.State) {
		return f, fmt.Errorf("%w: %s (%s) — %s", ErrFeatureFrozen, id, f.State, DocFixPhrase(id, doc, f.State))
	}
	return f, nil
}

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
	return CreateFeatureWithSpec(repoDir, projDir, title, module, parent, facets, components, "", false, "", "")
}

// CreateFeatureWithSpec is CreateFeature with the one-shot spec path (F-0120):
// a non-empty specBody is stamped with the cerne_id frontmatter and written as
// the real spec — the feature is born with its context captured, nothing left
// to fill or push. An empty body scaffolds the usual stub. force bypasses the
// duplicate-registration guard (F-0147) for a consciously distinct problem —
// the exact-title guard (F-0121) is never bypassed.
func CreateFeatureWithSpec(repoDir, projDir, title, module, parent string, facets, components []string, specBody string, force bool, owner, priority string) (store.Feature, error) {
	return createFeatureCore(repoDir, projDir, "F", title, module, parent, facets, components, specBody, force, owner, priority, "")
}

// CreateFeatureFixes is CreateFeatureWithSpec with the correction link (F-0321):
// fixes names the delivered feature whose defect this one corrects, validated to
// exist before anything is written (ErrFixesNotFound, wrapped).
func CreateFeatureFixes(repoDir, projDir, title, module, parent, fixes string, facets, components []string, specBody string, force bool, owner, priority string) (store.Feature, error) {
	return createFeatureCore(repoDir, projDir, "F", title, module, parent, facets, components, specBody, force, owner, priority, strings.TrimSpace(fixes))
}

// CreateEpicWithSpec creates a declared epic (E-, F-0203): a full feature
// lifecycle under its own id sequence — a semantic signal at birth for a PRD
// that is known to decompose. Epic behavior itself stays structural (children),
// never derived from the prefix.
func CreateEpicWithSpec(repoDir, projDir, title, module, parent string, facets, components []string, specBody string, force bool, owner, priority string) (store.Feature, error) {
	return createFeatureCore(repoDir, projDir, "E", title, module, parent, facets, components, specBody, force, owner, priority, "")
}

// CreateTaskWithSpec creates a task (T-, F-0203): an execution unit of a
// committed PRD, born approved — the human gate was paid on the parent. The
// parent is mandatory and must be past candidacy; every refusal happens before
// anything is written.
func CreateTaskWithSpec(repoDir, projDir, title, module, parent string, facets, components []string, specBody string, force bool, owner, priority string) (store.Feature, error) {
	if strings.TrimSpace(parent) == "" {
		return store.Feature{}, ErrParentRequired
	}
	return createFeatureCore(repoDir, projDir, "T", title, module, parent, facets, components, specBody, force, owner, priority, "")
}

// createFeatureCore is the shared creation flow behind F-/E-/T- (F-0203). The
// prefix decides only the id sequence and the two task rules: born approved and
// committed-parent required. Everything else — guards, stub, event, sync — is
// identical across the three.
func createFeatureCore(repoDir, projDir, prefix, title, module, parent string, facets, components []string, specBody string, force bool, owner, priority, fixes string) (store.Feature, error) {
	n, err := prepareFeature(prefix, title, module, parent, facets, components, specBody, force, owner, priority)
	n.fixes = fixes
	if err != nil {
		return store.Feature{}, err
	}
	// The whole creation — duplicate guard, id mint, entity+spec+event writes —
	// runs inside ONE critical section of the backend lock (F-0121): a
	// concurrent local process can neither sweep the half-written state into
	// its own commit nor mint the same id.
	var f store.Feature
	err = MutateAndSnapshot(repoDir, projDir, func() (msg string, err error) {
		f, msg, err = n.mint(repoDir)
		return msg, err
	})
	if err != nil {
		return store.Feature{}, err
	}
	return f, nil
}

// newFeature is a creation request already normalized outside the lock;
// mint writes it inside the caller's critical section, so a caller with its
// own mutation (an idea's promotion, F-0312) creates the feature atomically
// with its other writes.
type newFeature struct {
	prefix, title, module, parent, specBody, owner, priority string
	facets, components                                       []string
	force                                                    bool
	fromIdea                                                 string // F-0312: the idea a promote mints it from
	fixes                                                    string // F-0321: the feature this one corrects
}

func prepareFeature(prefix, title, module, parent string, facets, components []string, specBody string, force bool, owner, priority string) (newFeature, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return newFeature{}, fmt.Errorf("the feature title cannot be empty")
	}
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return newFeature{}, err
	}
	normPriority, err := domain.NormalizePriority(priority)
	if err != nil {
		return newFeature{}, err
	}
	if c := domain.CanonicalID(parent); c != "" {
		parent = c // a blank parent stays raw so validateParent refuses it
	}
	return newFeature{prefix: prefix, title: title, module: module, parent: parent, specBody: specBody,
		owner: strings.TrimSpace(owner), priority: normPriority, facets: normFacets,
		components: domain.NormalizeComponents(components), force: force}, nil
}

// mint runs the guards, mints the id and writes entity, spec and event; it
// returns the backend commit message. The caller holds the backend lock.
func (n newFeature) mint(repoDir string) (store.Feature, string, error) {
	prefix, title, module, parent, specBody, force, fixes := n.prefix, n.title, n.module, n.parent, n.specBody, n.force, n.fixes
	var f store.Feature
	err := func() error {
		proj, err := store.LoadProject(repoDir)
		if err != nil {
			return err
		}
		if err := ValidateModule(module, proj.Modules); err != nil {
			return err
		}
		if err := checkDuplicateTitle(repoDir, title); err != nil {
			return err
		}
		forced, err := guardRegistration(repoDir, title, force)
		if err != nil {
			return err
		}
		id, err := store.NextID(repoDir, prefix)
		if err != nil {
			return err
		}
		if parent != "" {
			if err := validateParent(repoDir, id, parent); err != nil {
				return err
			}
		}
		if fixes != "" {
			if err := validateFixes(repoDir, id, fixes); err != nil {
				return err
			}
		}
		state := domain.StateCandidate
		if prefix == "T" {
			// A task is born approved: its human gate is the parent PRD's, which
			// therefore must be committed (past candidacy) itself.
			p, err := store.LoadFeature(repoDir, parent)
			if err != nil {
				return err
			}
			if p.State == domain.StateCandidate {
				return fmt.Errorf("%w: %s — approve the PRD before decomposing it", ErrParentNotCommitted, parent)
			}
			state = domain.StateApproved
		}
		now := time.Now().UTC()
		f = store.Feature{
			ID:         id,
			Title:      title,
			Module:     module,
			Parent:     parent,
			Fixes:      fixes,
			State:      state,
			Facets:     n.facets,
			Components: n.components,
			Owner:      n.owner,
			Priority:   n.priority,
			FromIdea:   n.fromIdea,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := store.SaveFeature(repoDir, f); err != nil {
			return err
		}
		if strings.TrimSpace(specBody) != "" {
			if err := writeSpecBody(repoDir, id, specBody); err != nil {
				return err
			}
		} else if err := writeSpecStub(repoDir, id, title); err != nil {
			return err
		}
		// F-0235: a parent gaining its first (or another) child is decomposition —
		// scaffold its plan-with-execution-map now, not only at approve (F-0213),
		// so the epic is never registered without a plan slot. No-op if a real
		// plan already exists; fires in any parent state, candidate included.
		if parent != "" {
			p, err := store.LoadFeature(repoDir, parent)
			if err != nil {
				return err
			}
			if err := writePlanStub(repoDir, parent, p.Title); err != nil {
				return err
			}
		}
		payload := map[string]string{"title": title, "module": module}
		if parent != "" {
			payload["parent"] = parent
		}
		if fixes != "" {
			payload["fixes"] = fixes
		}
		if forced {
			// The guard saw live similars and --force overrode it (F-0147) —
			// leave the audit trail on the creation event.
			payload["guard"] = "forced"
		}
		ev := domain.NewEvent(id, "feature.created", payload, EventActor())
		return store.AppendEvent(repoDir, ev)
	}()
	if err != nil {
		return store.Feature{}, "", err
	}
	return f, fmt.Sprintf("cerne: create %s (%s)", f.ID, title), nil
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

// validateFixes checks that the feature id corrects, fixes, exists
// (ErrFixesNotFound, wrapped) and closes no loop: neither id itself nor a
// feature that already fixes id (ErrFixesCycle, wrapped). A terminal target is
// accepted: the fixed feature is normally implemented — that is how its defect
// escaped.
func validateFixes(repoDir, id, fixes string) error {
	if fixes == id {
		return fmt.Errorf("%w: %s", ErrFixesCycle, id)
	}
	target, err := store.LoadFeature(repoDir, fixes)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrFixesNotFound, fixes)
	}
	if err != nil {
		return err
	}
	if target.Fixes == id {
		return fmt.Errorf("%w: %s already fixes %s", ErrFixesCycle, fixes, id)
	}
	return nil
}

// validateParent checks that a non-empty parent is a usable epic for childID:
// it exists (ErrParentNotFound), is not terminal (ErrParentTerminal), and the
// link introduces no cycle (domain.ErrParentSelf/ErrParentCycle). childID is the
// feature being (re)parented — pass the freshly minted ID on create, where a
// cycle is impossible but the check stays uniform.
func validateParent(repoDir, childID, parent string) error {
	// The parent is an id, not a path: refuse a non-feature shape before the
	// path-based load, and a value that only resolved as a path ("./F-0001",
	// or "f-0001" on a case-insensitive filesystem) after it.
	if !domain.IsFeatureID(parent) {
		return fmt.Errorf("%w: %s", ErrParentNotFound, parent)
	}
	p, err := store.LoadFeature(repoDir, parent)
	if errors.Is(err, store.ErrNotFound) || (err == nil && p.ID != parent) {
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
// The parent is stored canonical (domain.CanonicalID). store.ErrNotFound (wrapped) for an unknown feature; the validateParent
// sentinels for an invalid link.
func SetFeatureParent(repoDir, projDir, id, parent string) error {
	id = domain.CanonicalID(id) // the child's event and commit carry the stored id
	if c := domain.CanonicalID(parent); c != "" {
		parent = c // a blank parent stays raw so validateParent refuses it, never a silent clear
	}
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
		// F-0235: reparenting under a new epic is decomposition too — scaffold its
		// plan-with-execution-map the same as creation does (no-op if a real plan
		// already exists).
		if parent != "" {
			p, err := store.LoadFeature(repoDir, parent)
			if err != nil {
				return "", err
			}
			if err := writePlanStub(repoDir, parent, p.Title); err != nil {
				return "", err
			}
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

// SetFeature changes a live feature's owner, priority and/or fixes link in place, appends
// a feature.set event, then commits, pushes and refreshes the docs.lock
// snapshot (F-0204). Each field is replaced only when its set flag is true, so
// a caller can update priority without touching owner (mirrors RetagFeature).
// Priority is validated against the closed set before any write
// (ErrInvalidPriority, wrapped); a terminal feature (domain.TitleEditable's
// definition — implemented/done/rejected/superseded) is frozen (ErrFeatureFrozen,
// wrapped); store.ErrNotFound (wrapped) for an unknown feature. A non-empty
// fixes must name an existing feature (ErrFixesNotFound, wrapped, F-0321)
// other than id and not one that fixes id (ErrFixesCycle, wrapped); an empty
// one clears the link.
func SetFeature(repoDir, projDir, id, owner, priority, fixes string, setOwner, setPriority, setFixes bool) (store.Feature, error) {
	var f store.Feature
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		f, err = store.LoadFeature(repoDir, id)
		if err != nil {
			return "", err
		}
		if !domain.TitleEditable(f.State) {
			return "", fmt.Errorf("%w: %s (%s)", ErrFeatureFrozen, id, f.State)
		}
		payload := map[string]string{}
		if setOwner {
			f.Owner = strings.TrimSpace(owner)
			payload["owner"] = f.Owner
		}
		if setPriority {
			np, err := domain.NormalizePriority(priority)
			if err != nil {
				return "", err // invalid priority: feature untouched
			}
			f.Priority = np
			payload["priority"] = f.Priority
		}
		if setFixes {
			fixes = strings.TrimSpace(fixes)
			if fixes != "" {
				if err := validateFixes(repoDir, id, fixes); err != nil {
					return "", err
				}
			}
			f.Fixes = fixes
			payload["fixes"] = fixes
		}
		f.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "feature.set", payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: set %s", id), nil
	})
	if err != nil {
		return store.Feature{}, err
	}
	return f, nil
}

// SaveFeatureDoc writes the markdown content of a feature lifecycle document
// (doc ∈ spec|plan|result) to content/features/<id>/<doc>.md, then commits,
// pushes and refreshes the docs.lock snapshot. A byte-identical write is a
// successful no-op (like SaveArtifact). ErrUnknownDoc for an invalid doc name;
// store.ErrNotFound (wrapped) when the feature does not exist; ErrFeatureFrozen
// (wrapped, naming the state's path) past the live states — F-0072's boundary,
// moved by F-0305: the result of an implemented feature changes only through
// AmendFeatureResult, which carries a reason; later, only errata.
func SaveFeatureDoc(repoDir, projDir, id, doc, content string) error {
	filename, ok := featureDocFilenames[doc]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownDoc, doc)
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := CheckDocWritable(repoDir, id, doc)
		if err != nil {
			return "", err
		}
		path := FeatureDocPath(repoDir, id, filename)
		// The event below is itself a change, so the byte-identical no-op has to be
		// detected here — captureChanges would always see the appended event.
		if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
			return "", fmt.Errorf("%w: %s", gitback.ErrNothingToCommit, path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
		// The audit trail of the write (F-0262): the backend commit is not
		// queryable by lint or stats. state is the state at write time. Whether an
		// approval went stale is decided by content since F-0318, not by this
		// event (lint.SpecChangedAfterApproval).
		ev := domain.NewEvent(id, "feature.doc_edited", map[string]string{
			"doc":   doc,
			"state": string(f.State),
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: update %s/%s", id, filename), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // backend already had exactly this content
	}
	return err
}

// UpdateCandidateSpec rewrites a candidate's — or an approved feature's (F-0318)
// — spec.md from the body the user confirmed in the Backlog chat (F-0256),
// through the same gated write path as the registration: cerne_id restamped by
// writeSpecBody, commit+push+snapshot by MutateAndSnapshot. It is the ONLY
// chat-driven write to an existing entity, and it deliberately touches nothing
// but the document — no state transition (approve stays a human gate, F-0131),
// no retitle. On an approved feature a different spec stales the approval
// (lint.SpecChangedAfterApproval). Past approval it refuses with
// ErrNotCandidate; the state is re-read inside the lock, so a concurrent start
// cannot be raced. Returns the feature as stored.
func UpdateCandidateSpec(repoDir, projDir, id, specBody string) (store.Feature, error) {
	f, err := loadCandidate(repoDir, id)
	if err != nil {
		return store.Feature{}, err
	}
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		cur, err := loadCandidate(repoDir, id)
		if err != nil {
			return "", err
		}
		f = cur
		if err := writeSpecBody(repoDir, id, specBody); err != nil {
			return "", err
		}
		// Same trail as the feature-page editor (F-0262): the backend commit is not
		// queryable by lint or by the entity history, so the write announces itself.
		// It matters more here since F-0265 — the writer can be the agent, and the
		// actor is what tells a spec the agent rewrote from one the human did.
		ev := domain.NewEvent(id, "feature.doc_edited", map[string]string{
			"doc":   "spec",
			"state": string(f.State),
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: update %s/spec.md", id), nil
	})
	if err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		return store.Feature{}, err
	}
	return f, nil
}

// ErrAmendNotImplemented is returned by AmendFeatureResult for a feature that is
// not implemented; the message names the path of its state (DocFixPhrase).
var ErrAmendNotImplemented = errors.New("only the result of an implemented feature can be amended")

// AmendFeatureResult replaces the result.md of an IMPLEMENTED feature, with the
// reason on the event (F-0305): the correction window between `complete` and
// the human `accept`, which then reads the amended result. ErrAmendNotImplemented
// for any other state, re-read inside the lock; ErrResultIncomplete when the new
// result fails the result checks — the mutation's error resets the clone, so
// the previous result stays. A blank reason or body is refused before the
// backend is touched. A body equal to the stored result is a successful no-op
// (changed false: no write, no event, no commit), like SaveFeatureDoc's
// byte-identical write — after the state check. Returns the feature as stored
// (state unchanged).
func AmendFeatureResult(repoDir, projDir, id, body, reason string) (f store.Feature, changed bool, err error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return store.Feature{}, false, errors.New("an amend needs a reason: pass --reason \"<why>\"")
	}
	if strings.TrimSpace(body) == "" {
		return store.Feature{}, false, errors.New("empty result body: pass the full result via --file <result.md> or on stdin")
	}
	content, err := amendedResult(id, body)
	if err != nil {
		return store.Feature{}, false, err
	}
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if f, err = store.LoadFeature(repoDir, id); err != nil {
			return "", err
		}
		if f.State != domain.StateImplemented {
			return "", fmt.Errorf("%w: %s is %s — %s", ErrAmendNotImplemented, id, f.State, DocFixPhrase(id, "result", f.State))
		}
		path := FeatureDocPath(repoDir, id, "result.md")
		// Detected here, not by captureChanges: the event below is itself a change.
		if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
			return "", fmt.Errorf("%w: %s", gitback.ErrNothingToCommit, path)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
		if err := CheckResultRecord(repoDir, f); err != nil {
			return "", err // an amend does not trade one debt for another
		}
		ev := domain.NewEvent(id, "feature.doc_edited", map[string]string{
			"doc":    "result",
			"state":  string(f.State),
			"reason": reason,
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: amend %s/result.md", id), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return f, false, nil // the stored result is already this body
	}
	if err != nil {
		return store.Feature{}, false, err
	}
	return f, true, nil
}

// amendedResult is the stored form of an amend body, under ONE cerne_id
// frontmatter: the body's own block when it already names id (what
// `cerne show --doc result` prints, extra keys kept; a quoted id is stored
// unquoted, as every cerne_id reader expects), cerne's stamp when the body has
// no block. A block naming another id is refused — it is another document —
// and so is a '---' opening with no cerne_id (a frontmatter without it, or a
// horizontal rule), which cerne cannot tell apart.
func amendedResult(id, body string) (string, error) {
	body = strings.TrimSpace(strings.TrimPrefix(body, "\ufeff"))
	lines := strings.Split(body, "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return "---\ncerne_id: " + id + "\n---\n\n" + body + "\n", nil
	}
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "---" {
		end++
	}
	for i := 1; i < end; i++ {
		v, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "cerne_id:")
		got := strings.Trim(strings.TrimSpace(v), `"'`)
		if !ok || got == "" {
			continue
		}
		if got != id {
			return "", fmt.Errorf("the body's frontmatter names cerne_id %q, not %s — is it another feature's result?", got, id)
		}
		if end == len(lines) {
			return "", fmt.Errorf("the body's '---' block never closes: close it with a '---' line, or start from 'cerne show %s --doc result'", id)
		}
		lines[i] = "cerne_id: " + id
		return strings.Join(lines, "\n") + "\n", nil
	}
	return "", fmt.Errorf("the body opens a '---' block without cerne_id: start from 'cerne show %s --doc result' or drop the block", id)
}

// loadCandidate loads a feature and asserts its spec is still open to the chat
// — candidate or approved (F-0318) — the precondition shared by the chat's
// refine turn (BuildRefineBlock) and its write (UpdateCandidateSpec).
func loadCandidate(repoDir, id string) (store.Feature, error) {
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return store.Feature{}, err
	}
	if f.State != domain.StateCandidate && f.State != domain.StateApproved {
		return store.Feature{}, fmt.Errorf("%w: %s is %s — %s", ErrNotCandidate, id, f.State, DocFixPhrase(id, "spec", f.State))
	}
	return f, nil
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
	children, err := epicChildren(repoDir, id)
	if err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + plantmpl.StubForEpic(title, children)
	return os.WriteFile(path, []byte(content), 0o644)
}

// EpicPlanStub reports whether id's plan.md is still the unfilled scaffold —
// used by the cmd layer to decide whether to print the "fill the plan before
// approve" advisory (F-0235) after registering or reparenting a child. A
// missing plan.md counts as false: nothing was just scaffolded to advise about.
func EpicPlanStub(repoDir, id string) bool {
	data, err := os.ReadFile(FeatureDocPath(repoDir, id, "plan.md"))
	if err != nil {
		return false
	}
	return !doccheck.HasRealContent(data)
}

// epicChildren returns id's children (if any) as plantmpl.EpicChild, in the
// same order store.ChildrenByParent sorts them (by ID) — the roster the plan
// scaffold's execution map is built from.
func epicChildren(repoDir, id string) ([]plantmpl.EpicChild, error) {
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	kids := store.ChildrenByParent(feats)[id]
	out := make([]plantmpl.EpicChild, len(kids))
	for i, k := range kids {
		out[i] = plantmpl.EpicChild{ID: k.ID, Title: k.Title}
	}
	return out, nil
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

// writeEpicResultStub is writeResultStub's epic-closure counterpart (F-0214):
// it scaffolds the same result template, prefixed with the children roster as
// raw material for the author. The roster lives inside an HTML comment, so
// doccheck still reads the file as scaffold (a roster is not a filled-in
// result) — the complete gate stays closed until a real section is written.
// No-op when a real result already exists, same protection as writeResultStub.
func writeEpicResultStub(repoDir, id, title string, children []store.Feature) error {
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
	var roster strings.Builder
	roster.WriteString("<!-- ROSTER DAS FILHAS (matéria-prima para a seção Resultado; apague ao preencher):\n")
	for _, c := range children {
		roster.WriteString(fmt.Sprintf("- %s: %s — result: content/features/%s/result.md\n", c.ID, c.Title, c.ID))
	}
	roster.WriteString("-->\n\n")
	content := "---\ncerne_id: " + id + "\n---\n\n" + roster.String() + restmpl.Stub(title)
	return os.WriteFile(path, []byte(content), 0o644)
}

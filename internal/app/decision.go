package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/dectmpl"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// DecisionOutcome reports a decision transition that was applied and synced.
type DecisionOutcome struct {
	ID   string
	From domain.DecisionState
	To   domain.DecisionState
}

// CreateDecision mints a new active decision in the backend clone at repoDir:
// allocates the next D- id, persists the entity, writes the ADR document (from
// body when given, else a stub), appends the decision.created event,
// commits+pushes the backend and refreshes the docs.lock snapshot in projDir. A
// non-empty body makes 'decision new' one-shot: the ADR is born with real
// content, no draft+push needed. facets/components tag the decision on the
// shared D-0002 axes (facet closed-set, component curated), like a note. A live
// decision with a similar title refuses the creation (F-0324); force overrides
// it and records guard=forced on decision.created. informs links it at birth
// to the entities it informs (E-0005 criterion 22), validated like a note's.
func CreateDecision(repoDir, projDir, title, body string, informs, facets, components []string, force bool) (store.Decision, error) {
	// Validate/normalize tags before any side effect (NextID, save): an unknown
	// facet must abort without minting an id or writing a file.
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Decision{}, err
	}
	normComponents := domain.NormalizeComponents(components)
	// Id mint and writes run inside the backend lock's critical section
	// (F-0122): concurrent creations serialize instead of minting the same id.
	var d store.Decision
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		cleaned, err := validateInforms(repoDir, informs)
		if err != nil {
			return "", err
		}
		// Duplicate guard (F-0324): before the id mint and any write.
		forced, err := guardDecision(repoDir, title, tagSet{facets: normFacets, components: normComponents}, force)
		if err != nil {
			return "", err
		}
		// After the duplicate guard (E-0005): a duplicate is refused first, so
		// the --force a near component asks for is never given blind to it.
		if err := checkComponents(repoDir, normComponents, force); err != nil {
			return "", err
		}
		id, err := store.NextID(repoDir, "D")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		d = store.Decision{
			ID:         id,
			Title:      title,
			State:      domain.DecisionActive,
			Informs:    cleaned,
			Facets:     normFacets,
			Components: normComponents,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := store.SaveDecision(repoDir, d); err != nil {
			return "", err
		}
		if err := writeDecisionBody(repoDir, id, title, body); err != nil {
			return "", err
		}
		payload := map[string]string{"title": title}
		if forced {
			payload["guard"] = "forced"
		}
		if len(cleaned) > 0 {
			payload["informs"] = strings.Join(cleaned, ",")
		}
		ev := domain.NewEvent(id, "decision.created", payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: create %s (%s)", id, title), nil
	})
	if err != nil {
		return store.Decision{}, err
	}
	return d, nil
}

// TransitionDecision supersedes decision id by decision `by` in the backend
// clone at repoDir: validates the flags, loads both decisions (errors when one
// is missing, when by is the decision itself, or when id is already terminal),
// applies the domain transition, persists state+superseded_by+reason, appends
// the decision.superseded event and syncs the backend + docs.lock in projDir.
func TransitionDecision(repoDir, projDir, id, by, reason string) (DecisionOutcome, error) {
	var out DecisionOutcome
	var title string
	if err := ValidateTransitionFlags(domain.ActionSupersede, by, reason); err != nil {
		return out, err
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		d, err := store.LoadDecision(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("decision %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if by == id {
			return "", fmt.Errorf("--by cannot be the decision itself (%s)", id)
		}
		if _, err := store.LoadDecision(repoDir, by); errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("decision %s (--by) not found in backend", by)
		} else if err != nil {
			return "", err
		}

		newState, err := domain.TransitionDecision(d.State, domain.ActionSupersede)
		if err != nil {
			return "", err
		}
		oldState := d.State
		d.State = newState
		d.SupersededBy = by
		d.Reason = reason
		d.UpdatedAt = time.Now().UTC()
		if err := store.SaveDecision(repoDir, d); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "decision.superseded", map[string]string{"by": by, "reason": reason}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		title = d.Title
		out = DecisionOutcome{ID: id, From: oldState, To: newState}
		return fmt.Sprintf("cerne: supersede %s", id), nil
	})
	if err != nil {
		return DecisionOutcome{}, err
	}
	dispatchWebhook(projDir, id, "decision", string(domain.ActionSupersede), string(out.From), string(out.To), title)
	return out, nil
}

// KeepDecision records that a human reviewed active decision id and it still
// holds (F-0385): ReviewedAt is stamped now and decision.kept carries the
// actor. Unlike a note's keep it is not a permanent flag — lint's
// stale-decision counts from ReviewedAt, so a new signal flags it again.
func KeepDecision(repoDir, projDir, id string) (store.Decision, error) {
	var d store.Decision
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		d, err = store.LoadDecision(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("decision %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if d.State != domain.DecisionActive {
			return "", fmt.Errorf("decision %s is not active (it is %s) — keep only applies to an active decision", id, d.State)
		}
		now := time.Now().UTC()
		d.ReviewedAt, d.UpdatedAt = &now, now
		if err := store.SaveDecision(repoDir, d); err != nil {
			return "", err
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "decision.kept", map[string]string{}, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: keep %s (reviewed)", id), nil
	})
	if err != nil {
		return store.Decision{}, err
	}
	return d, nil
}

// RetireDecision moves decision id to retired with no successor (F-0385): a
// decision that stopped applying has no --by to point at. A blank reason is
// domain.ErrRetireReasonRequired and nothing is written; decision.retired
// carries the reason and the state it left.
func RetireDecision(repoDir, projDir, id, reason string) (DecisionOutcome, error) {
	var out DecisionOutcome
	var title string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		d, err := store.LoadDecision(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("decision %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		newState, err := domain.RetireDecision(d.State, reason)
		if err != nil {
			return "", err
		}
		oldState := d.State
		d.State, d.SupersededBy, d.Reason = newState, "", reason
		d.UpdatedAt = time.Now().UTC()
		if err := store.SaveDecision(repoDir, d); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "decision.retired", map[string]string{"reason": reason, "from": string(oldState)}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		title = d.Title
		out = DecisionOutcome{ID: id, From: oldState, To: newState}
		return fmt.Sprintf("cerne: retire %s", id), nil
	})
	if err != nil {
		return DecisionOutcome{}, err
	}
	dispatchWebhook(projDir, id, "decision", string(domain.ActionRetire), string(out.From), string(out.To), title)
	return out, nil
}

// RetagDecision sets the tag axes of a decision, in any state, in the backend clone at
// repoDir. Each axis is replaced only when its set flag is true, so a caller can
// update facets without touching components (or clear an axis by passing an empty
// slice with set=true); state, superseded_by, reason and the ADR body are
// preserved. Facets are validated against the closed set before any write, so an
// unknown facet aborts leaving the decision untouched. Appends decision.retagged
// and syncs the backend + docs.lock in projDir.
func RetagDecision(repoDir, projDir, id string, facets, components []string, setFacets, setComponents, force bool) (store.Decision, error) {
	var d store.Decision
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		d, err = store.LoadDecision(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("decision %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if setFacets {
			nf, err := domain.NormalizeFacets(facets)
			if err != nil {
				return "", err // unknown facet: decision untouched
			}
			d.Facets = nf
		}
		if setComponents {
			d.Components = domain.NormalizeComponents(components)
			if err := checkComponents(repoDir, d.Components, force); err != nil {
				return "", err
			}
		}
		d.UpdatedAt = time.Now().UTC()
		if err := store.SaveDecision(repoDir, d); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "decision.retagged", map[string]string{
			"facets":     strings.Join(d.Facets, ","),
			"components": strings.Join(d.Components, ","),
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: retag %s", id), nil
	})
	if err != nil {
		return store.Decision{}, err
	}
	return d, nil
}

// LinkDecision adds (add) and removes (remove) the entities an active
// decision informs, the mirror of LinkNote (E-0005 criterion 22): a decision
// linked to a task reaches its package in ring (a), with its body. It returns
// the decision and the removed ids that were not there; a change of nothing
// commits nothing.
func LinkDecision(repoDir, projDir, id string, add, remove []string) (store.Decision, []string, error) {
	id = domain.CanonicalID(id) // N-0061: file names and events by the canonical id
	var d store.Decision
	var absent []string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		absent = nil // the mutation may run again
		var err error
		d, err = store.LoadDecision(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("decision %s not found in backend: %w", id, store.ErrNotFound)
		}
		if err != nil {
			return "", err
		}
		if d.State != domain.DecisionActive {
			return "", fmt.Errorf("decision %s is not active (it is %s) — link only applies to an active decision: %w", id, d.State, ErrEntityNotLive)
		}
		cleaned, err := validateInforms(repoDir, add)
		if err != nil {
			return "", err
		}
		var added, removed []string
		d.Informs, added, removed, absent, err = relink(d.Informs, cleaned, remove)
		if err != nil || len(added)+len(removed) == 0 {
			return "", err
		}
		d.UpdatedAt = time.Now().UTC()
		if err := store.SaveDecision(repoDir, d); err != nil {
			return "", err
		}
		if err := appendLinkEvents(repoDir, "decision", id, added, removed, ""); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: link %s", id), nil
	})
	if err != nil && !errors.Is(err, gitback.ErrNothingToCommit) { // unchanged: nothing to link
		return store.Decision{}, nil, err
	}
	return d, absent, nil
}

// DecisionDocPath returns the backend path of a decision's content document.
func DecisionDocPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "decisions", id+".md")
}

// writeDecisionBody writes content/decisions/<id>.md as cerne's stamped cerne_id
// frontmatter followed by body. An empty body falls back to writeDecisionStub (the
// canonical ADR template scaffold). cerne stamps the frontmatter itself — the body
// supplied for a one-shot decision never carries cerne_id.
func writeDecisionBody(repoDir, id, title, body string) error {
	if strings.TrimSpace(body) == "" {
		return writeDecisionStub(repoDir, id, title)
	}
	path := DecisionDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// writeDecisionStub creates content/decisions/<id>.md scaffolded from the
// canonical ADR template (dectmpl): the section skeleton with inline guidance.
// doccheck treats that scaffold as a stub until the author fills a section.
func writeDecisionStub(repoDir, id, title string) error {
	path := DecisionDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + dectmpl.StubIn(store.DocsLanguage(repoDir), title)
	return os.WriteFile(path, []byte(content), 0o644)
}

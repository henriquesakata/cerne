// Package app hosts use-case orchestration shared by the CLI and the
// local UI server: operations that load entities, enforce domain rules
// and sync the backend clone.
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// TransitionOutcome reports a feature transition that was applied and synced.
type TransitionOutcome struct {
	ID   string
	From domain.FeatureState
	To   domain.FeatureState
	// Propagated holds the ancestor epics that a start pulled into development
	// alongside this feature, nearest-first. Empty for the common case and for
	// every action other than start. See propagateStartToAncestors.
	Propagated []string
}

// eventTypes maps each lifecycle action to the event type it appends.
var eventTypes = map[domain.Action]string{
	domain.ActionApprove:   "feature.approved",
	domain.ActionReject:    "feature.rejected",
	domain.ActionStart:     "feature.started",
	domain.ActionComplete:  "feature.completed",
	domain.ActionAccept:    "feature.accepted",
	domain.ActionSupersede: "feature.superseded",
}

// TransitionFeature applies action to feature id in the backend clone at
// repoDir: validates by/reason rules and document gates, persists the new
// state, appends the lifecycle event, commits+pushes the backend and
// refreshes the docs.lock snapshot in projDir.
func TransitionFeature(repoDir, projDir, id string, action domain.Action, by, reason string) (TransitionOutcome, error) {
	var out TransitionOutcome
	if err := ValidateTransitionFlags(action, by, reason); err != nil {
		return out, err
	}
	eventType, ok := eventTypes[action]
	if !ok {
		return out, fmt.Errorf("ação desconhecida: %s", action)
	}
	// State reads, gate checks and writes run inside the backend lock's critical
	// section (F-0122): the FSM check is a compare-and-set against the state
	// serialized by the lock, and no concurrent process can sweep the writes.
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("feature %s não encontrada no backend", id)
		}
		if err != nil {
			return "", err
		}
		if action == domain.ActionSupersede {
			if by == id {
				return "", fmt.Errorf("--by não pode ser a própria feature (%s)", id)
			}
			if _, err := store.LoadFeature(repoDir, by); errors.Is(err, store.ErrNotFound) {
				return "", fmt.Errorf("feature %s (--by) não encontrada no backend", by)
			} else if err != nil {
				return "", err
			}
		}

		newState, err := domain.Transition(f.State, action)
		if err != nil {
			return "", err
		}
		docs, err := FeatureDocSet(repoDir, id)
		if err != nil {
			return "", err
		}
		if err := domain.CheckGate(action, docs); err != nil {
			return "", fmt.Errorf("%w — escreva content/features/%s/%s no backend e rode 'cerne push'", err, id, gateDocFilename(err))
		}

		oldState := f.State
		f.State = newState
		f.UpdatedAt = time.Now().UTC()
		switch action {
		case domain.ActionSupersede:
			f.SupersededBy = by
			f.Reason = reason
		case domain.ActionReject:
			f.Reason = reason
		}
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}

		var payload map[string]string
		if by != "" || reason != "" {
			payload = map[string]string{}
			if by != "" {
				payload["by"] = by
			}
			if reason != "" {
				payload["reason"] = reason
			}
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, eventType, payload, EventActor())); err != nil {
			return "", err
		}
		// End-of-flow bypass (F-0131): with [gates] accept = "auto" the complete
		// carries the feature straight to done in the same critical section —
		// both events land in one commit. Default (human) stops at implemented
		// ("Ready") and waits for the explicit human accept.
		if action == domain.ActionComplete && projectGates(projDir).AcceptGate() == project.GateAuto {
			promoted, err := domain.Transition(f.State, domain.ActionAccept)
			if err != nil {
				return "", err
			}
			f.State = promoted
			f.UpdatedAt = time.Now().UTC()
			if err := store.SaveFeature(repoDir, f); err != nil {
				return "", err
			}
			ev := domain.NewEvent(id, eventTypes[domain.ActionAccept], map[string]string{"via": "auto"}, EventActor())
			if err := store.AppendEvent(repoDir, ev); err != nil {
				return "", err
			}
			newState = promoted
		}
		// Approving a candidate opens the plan phase: scaffold the plan stub from the
		// canonical template so it is committed alongside the approval (mirrors the
		// spec stub written at create time). No-op if a real plan already exists.
		if action == domain.ActionApprove {
			if err := writePlanStub(repoDir, id, f.Title); err != nil {
				return "", err
			}
		}
		// Starting development opens the result phase: scaffold the result stub so it
		// is committed alongside the transition (mirrors the plan stub at approve).
		var propagated []string
		if action == domain.ActionStart {
			if err := writeResultStub(repoDir, id, f.Title); err != nil {
				return "", err
			}
			// A child entering development pulls its ancestor epics along: each one
			// that is ready (approved, with a real plan) starts in the same commit,
			// so the epic never lags its own decomposition. Bundled inside the same
			// critical section — all or nothing, no mid-state commit.
			if propagated, err = propagateStartToAncestors(repoDir, id); err != nil {
				return "", err
			}
		}
		msg := fmt.Sprintf("cerne: %s %s", action, id)
		if len(propagated) > 0 {
			msg += fmt.Sprintf(" (+ épico %s)", strings.Join(propagated, ", "))
		}
		out = TransitionOutcome{ID: id, From: oldState, To: newState, Propagated: propagated}
		return msg, nil
	})
	if err != nil {
		return TransitionOutcome{}, err
	}
	return out, nil
}

// propagateStartToAncestors walks up the epic chain from a feature that just
// entered development and starts each ancestor that is ready — in `approved`
// state with its start gate satisfied (a real plan, the same gate a manual
// start enforces). It never bypasses the gate: an approved epic without a plan
// is left untouched (lint's epic-behind-children still flags it), and a parent
// that is not approved (candidate/in_development/terminal) stops the walk with
// no change. Mutations persist to the store and are committed by the caller's
// critical section (MutateAndSnapshot), so the child start and every propagated
// epic land in one commit. Returns the ancestors it started, nearest-first
// (empty when none).
// A visited set guards against a cyclic parent link so a corrupt graph cannot
// loop forever.
func propagateStartToAncestors(repoDir, childID string) ([]string, error) {
	cur, err := store.LoadFeature(repoDir, childID)
	if err != nil {
		return nil, err
	}
	var started []string
	visited := map[string]bool{childID: true}
	for cur.Parent != "" {
		if visited[cur.Parent] {
			break // cyclic parent link: stop rather than loop
		}
		visited[cur.Parent] = true
		parent, err := store.LoadFeature(repoDir, cur.Parent)
		if errors.Is(err, store.ErrNotFound) {
			break // dangling parent: lint's parent-not-found reports it
		}
		if err != nil {
			return started, err
		}
		if parent.State != domain.StateApproved {
			break // candidate needs approval; in_development/terminal: nothing to do
		}
		docs, err := FeatureDocSet(repoDir, parent.ID)
		if err != nil {
			return started, err
		}
		if err := domain.CheckGate(domain.ActionStart, docs); err != nil {
			break // epic not ready (no real plan): don't force it, leave it to lint
		}
		newState, err := domain.Transition(parent.State, domain.ActionStart)
		if err != nil {
			break
		}
		parent.State = newState
		parent.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, parent); err != nil {
			return started, err
		}
		ev := domain.NewEvent(parent.ID, "feature.started", map[string]string{"via": childID}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return started, err
		}
		if err := writeResultStub(repoDir, parent.ID, parent.Title); err != nil {
			return started, err
		}
		started = append(started, parent.ID)
		cur = parent
	}
	return started, nil
}

// ValidateTransitionFlags enforces which flags each action accepts:
// supersede requires both, reject requires --reason only, every other
// action forbids both.
func ValidateTransitionFlags(action domain.Action, by, reason string) error {
	switch action {
	case domain.ActionSupersede:
		if by == "" {
			return fmt.Errorf("--by é obrigatório para a substituição")
		}
		if reason == "" {
			return fmt.Errorf("--reason é obrigatório para a substituição")
		}
	case domain.ActionReject:
		if by != "" {
			return fmt.Errorf("--by não é permitido para reject")
		}
		if reason == "" {
			return fmt.Errorf("--reason é obrigatório para reject")
		}
	case domain.ActionAccept:
		// The acceptance note is optional: the accept is an act, not a document.
		if by != "" {
			return fmt.Errorf("--by não é permitido para accept")
		}
	default:
		if by != "" || reason != "" {
			return fmt.Errorf("--by/--reason não são permitidos para %s", action)
		}
	}
	return nil
}

// FeatureDocPath returns the backend path of a feature lifecycle document.
func FeatureDocPath(repoDir, id, filename string) string {
	return filepath.Join(repoDir, "content", "features", id, filename)
}

// FeatureDocSet reports which lifecycle documents of the feature exist in
// the backend with real (non-stub) content.
func FeatureDocSet(repoDir, id string) (domain.DocSet, error) {
	var ds domain.DocSet
	var err error
	if ds.HasSpec, err = isRealDoc(FeatureDocPath(repoDir, id, "spec.md")); err != nil {
		return ds, err
	}
	if ds.HasPlan, err = isRealDoc(FeatureDocPath(repoDir, id, "plan.md")); err != nil {
		return ds, err
	}
	if ds.HasResult, err = isRealDoc(FeatureDocPath(repoDir, id, "result.md")); err != nil {
		return ds, err
	}
	return ds, nil
}

// isRealDoc reports whether the file exists and has real content.
func isRealDoc(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return doccheck.HasRealContent(data), nil
}

// EventActor returns CERNE_ACTOR when set, "local" otherwise.
func EventActor() string {
	if a := os.Getenv("CERNE_ACTOR"); a != "" {
		return a
	}
	return "local"
}

// gateDocFilename maps a gate error to the document the hint should name.
func gateDocFilename(gateErr error) string {
	switch {
	case errors.Is(gateErr, domain.ErrGatePlanRequired):
		return "plan.md"
	case errors.Is(gateErr, domain.ErrGateResultRequired):
		return "result.md"
	default:
		return "spec.md"
	}
}

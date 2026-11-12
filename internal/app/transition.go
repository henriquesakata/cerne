// Package app hosts use-case orchestration shared by the CLI and the
// local UI server: operations that load entities, enforce domain rules
// and sync the backend clone.
package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/telemetry"
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
	// EpicReadyToClose holds the parent epic ID when this complete left no
	// other alive (non-rejected/superseded), non-implemented child — the epic's
	// result was scaffolded with the children roster. Empty for the common case
	// and for every action other than complete. See scaffoldEpicCloseIfReady.
	EpicReadyToClose string
	// Warnings carries non-blocking advisories raised by the transition — today
	// the parallel-implementation guard's ambiguous matches (F-0257): an active
	// work of the same actor on this feature that no session binding can be
	// attributed to. Printed by the caller; empty for the common case.
	Warnings []string
	// Bar is the bar line an approve accepted (F-0308), also recorded on the
	// approve event; "" for every other action.
	Bar string
	// Baseline is true when an approve on an approved feature recorded its
	// first approved copy (F-0318 crit. 14: approved before the copy existed)
	// rather than renewing a stale one.
	Baseline bool
	// Pendings holds one line per pending the feature paid that this
	// transition moved (F-0885: resolved on accept, reopened on reject,
	// transferred on supersede), for the caller to print. Empty otherwise.
	Pendings []string
}

// ErrApprovalStale is the start refusal for an approved feature whose spec
// differs from the version its last approve accepted (F-0318).
var ErrApprovalStale = errors.New("spec changed since approval")

// ErrNothingToReapprove refuses approving an approved feature whose approved
// copy exists and still holds the spec's content (F-0318). It wraps
// domain.ErrInvalidTransition: to every caller that asks "can this be
// approved?", an approved feature with nothing to renew answers as before.
var ErrNothingToReapprove = fmt.Errorf("%w: nothing to re-approve", domain.ErrInvalidTransition)

// ApprovalBaselineMissing reports an approved feature with no approved copy —
// approved before F-0318. Never stale (crit. 14); its next approve records the
// copy.
func ApprovalBaselineMissing(repoDir string, f store.Feature) bool {
	if f.State != domain.StateApproved {
		return false
	}
	_, err := os.Stat(store.ApprovedSpecPath(repoDir, f.ID))
	return errors.Is(err, fs.ErrNotExist)
}

// checkReapprove returns ErrNothingToReapprove for an approved feature whose
// approval is not stale; nil for any other feature, and for an approved one
// with no copy yet, whose approve records the baseline.
func checkReapprove(repoDir string, f store.Feature) error {
	if f.State != domain.StateApproved || ApprovalBaselineMissing(repoDir, f) {
		return nil
	}
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); !stale {
		return fmt.Errorf("%w: %s is approved and its spec is the approved version", ErrNothingToReapprove, f.ID)
	}
	return nil
}

// eventTypes maps each lifecycle action to the event type it appends.
var eventTypes = map[domain.Action]string{
	domain.ActionApprove:   "feature.approved",
	domain.ActionReject:    "feature.rejected",
	domain.ActionStart:     "feature.started",
	domain.ActionComplete:  "feature.completed",
	domain.ActionAccept:    "feature.accepted",
	domain.ActionSupersede: "feature.superseded",
	domain.ActionRevert:    "feature.reverted",
}

// TransitionFeature applies action to feature id in the backend clone at
// repoDir: validates by/reason rules and document gates, persists the new
// state, appends the lifecycle event, commits+pushes the backend and
// refreshes the docs.lock snapshot in projDir. d (F-0179, complete only)
// carries the delivery links to persist alongside the transition; nil for
// every other action and for a complete with no delivery.
func TransitionFeature(repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery) (TransitionOutcome, error) {
	return TransitionFeatureForce(repoDir, projDir, id, action, by, reason, d, false)
}

// TransitionFeatureForce is TransitionFeature with the conscious override of
// the parallel-implementation guard (F-0257): force starts the feature even
// when another session holds active work on it, stamping the event.
func TransitionFeatureForce(repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery, force bool) (TransitionOutcome, error) {
	op := telemetry.StartOp("backend")
	out, err := TransitionFeatureOpForce(op, repoDir, projDir, id, action, by, reason, d, force)
	status := "ok"
	if err != nil {
		status = "error"
	}
	op.End(status)
	return out, err
}

// TransitionFeatureOp is TransitionFeature recording its phases on the
// caller's Op (F-0149): the server passes the request's Op so the transition's
// lock_wait/fetch/push story correlates with the HTTP log line.
func TransitionFeatureOp(op *telemetry.Op, repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery) (TransitionOutcome, error) {
	return TransitionFeatureOpForce(op, repoDir, projDir, id, action, by, reason, d, false)
}

// TransitionFeatureOpForce is TransitionFeatureOp carrying the F-0257 guard
// override.
func TransitionFeatureOpForce(op *telemetry.Op, repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery, force bool) (TransitionOutcome, error) {
	var out TransitionOutcome
	var title string
	var baseline bool
	if err := ValidateTransitionFlags(action, by, reason); err != nil {
		return out, err
	}
	if d != nil && action != domain.ActionComplete {
		return out, fmt.Errorf("delivery links are only captured on complete, not %s", action)
	}
	eventType, ok := eventTypes[action]
	if !ok {
		return out, fmt.Errorf("unknown action: %s", action)
	}
	// State reads, gate checks and writes run inside the backend lock's critical
	// section (F-0122): the FSM check is a compare-and-set against the state
	// serialized by the lock, and no concurrent process can sweep the writes.
	err := MutateAndSnapshotOp(op, repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("feature %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if action == domain.ActionSupersede {
			if by == id {
				return "", fmt.Errorf("--by cannot be the feature itself (%s)", id)
			}
			if _, err := store.LoadFeature(repoDir, by); errors.Is(err, store.ErrNotFound) {
				return "", fmt.Errorf("feature %s (--by) not found in backend", by)
			} else if err != nil {
				return "", err
			}
		}

		// Stale-approval gate (F-0318): the spec to implement is the one a human
		// approved, and a re-approval needs something to renew. Refused before the
		// parallel guard and any write, so a refused start opens no work; --force
		// does not cross it.
		if action == domain.ActionApprove {
			if err := checkReapprove(repoDir, f); err != nil {
				return "", err
			}
			baseline = ApprovalBaselineMissing(repoDir, f)
		}
		if action == domain.ActionStart {
			if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale {
				return "", fmt.Errorf("%w: %s — a human re-approves it with 'cerne feature approve %s' (or 'cerne feature revert %s' sends it back to the Backlog)", ErrApprovalStale, id, id, id)
			}
		}

		// Parallel-implementation guard (F-0257): before the FSM and before any
		// write, refuse a start that another session/actor is already on. Runs
		// ahead of the auto-work open below, so a refused start never registers
		// a second work (crit. 5).
		var guardForced bool
		var warnings []string
		if action == domain.ActionStart {
			guardForced, warnings, err = guardParallelStart(repoDir, projDir, id, force)
			if err != nil {
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
		if err := domain.CheckGate(id, action, docs); err != nil {
			return "", fmt.Errorf("%w — write content/features/%s/%s in the backend and run 'cerne push'", err, id, gateDocFilename(err))
		}
		// Gauntlet bar gate (F-0295): a property of the spec, not of who
		// approves, so it lives here where CLI and UI share it — as a CLI-only
		// check the UI approved specs with no bar.
		// Light mode (F-0364) skips it: the project chose to run without the
		// Gauntlet, and status says so.
		gauntletOn := projectGates(projDir).GauntletEnabled()
		if action == domain.ActionApprove && gauntletOn {
			if err := CheckSpecBar(repoDir, id); err != nil {
				return "", err
			}
		}

		// Result record gate (F-0303): the lint's result rules, charged while the
		// result is still editable — after complete it is frozen out of .docs/.
		if action == domain.ActionComplete {
			if err := CheckResultRecord(repoDir, f, gauntletOn); err != nil {
				return "", fmt.Errorf("%w\n— fix content/features/%s/result.md in the backend and run 'cerne push'", err, id)
			}
			// The result names each debt the feature pays (F-0885), while it can.
			if err := CheckPaidCited(repoDir, f); err != nil {
				return "", err
			}
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
		// Delivery links (F-0179): captured in the same write as the complete,
		// so entity, event and state land in one backend commit.
		if d != nil {
			if d.CommitURL != "" {
				f.DeliveryCommitURL = d.CommitURL
			}
			if d.MRURL != "" {
				f.DeliveryMRURL = d.MRURL
			}
		}
		// The delivery shows in the result too (F-0355), in the same commit.
		if action == domain.ActionComplete {
			if err := stampDeliveryInResult(repoDir, f); err != nil {
				return "", err
			}
		}
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}

		var payload map[string]string
		if by != "" || reason != "" || (d != nil && *d != (Delivery{})) {
			payload = map[string]string{}
			if by != "" {
				payload["by"] = by
			}
			if reason != "" {
				payload["reason"] = reason
			}
			if d != nil && d.CommitURL != "" {
				payload["commit_url"] = d.CommitURL
			}
			if d != nil && d.MRURL != "" {
				payload["mr_url"] = d.MRURL
			}
		}
		// The conscious override of the parallel guard is part of the record
		// (F-0257 crit. 2), same shape as the duplicate guard's `guard: forced`.
		if guardForced {
			if payload == nil {
				payload = map[string]string{}
			}
			payload["guard"] = "forced"
		}
		// Approving is accepting the proposed bar (F-0308 crit. 8): the line the
		// gate just validated goes on the event, with the actor.
		var bar string
		if action == domain.ActionApprove {
			if bar, err = AcceptedBar(repoDir, id); err != nil {
				return "", err
			}
			if bar != "" {
				if payload == nil {
					payload = map[string]string{}
				}
				payload["bar"] = bar
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
		// The pendings this feature pays follow it (F-0885), in this commit.
		movedPendings, err := movePaidPendings(repoDir, f, reason)
		if err != nil {
			return "", err
		}
		// Approving a candidate opens the plan phase: scaffold the plan stub from the
		// canonical template so it is committed alongside the approval (mirrors the
		// spec stub written at create time). No-op if a real plan already exists.
		// Every approve — a re-approval too — keeps the spec it accepted.
		if action == domain.ActionApprove {
			if err := writePlanStub(repoDir, id, f.Title); err != nil {
				return "", err
			}
			if err := keepApprovedSpec(repoDir, id); err != nil {
				return "", err
			}
		}
		// Leaving development ends the auto-registered activity (F-0146): the
		// same commit that records the transition closes the auto work.
		if oldState == domain.StateInDevelopment {
			if err := closeAutoWorks(repoDir, id, action); err != nil {
				return "", err
			}
		}
		// Leaving the live cycle ends its planning, of any actor (F-0341
		// crit. 2). After closeAutoWorks, so a work the F-0208 adoption already
		// closed is not touched twice.
		if featureLive(oldState) && !featureLive(newState) && activityAutoEnabled(projDir) {
			workAction := domain.ActionAbandon
			if action == domain.ActionComplete {
				workAction = domain.ActionComplete
			}
			if err := closePlanningWorks(repoDir, id, oldState, workAction, "feature "+string(action)); err != nil {
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
			// Auto-registered activity (F-0146): entering development records who
			// is on it. Only the leaf feature the agent actually picked up — the
			// epic propagation below never opens works of its own (crit. 4).
			// Planning ends where development begins (F-0341 crit. 1): every
			// planning work, of any actor, closes before the auto work opens.
			if activityAutoEnabled(projDir) {
				if err := closePlanningWorks(repoDir, id, oldState, domain.ActionComplete, "planning closed: feature start"); err != nil {
					return "", err
				}
				if err := openAutoWork(repoDir, f); err != nil {
					return "", err
				}
			}
			// A child entering development pulls its ancestor epics along: each one
			// that is ready (approved, with a real plan) starts in the same commit,
			// so the epic never lags its own decomposition. Bundled inside the same
			// critical section — all or nothing, no mid-state commit.
			var staleNote string
			if propagated, staleNote, err = propagateStartToAncestors(repoDir, id); err != nil {
				return "", err
			}
			if staleNote != "" {
				warnings = append(warnings, staleNote)
			}
		}
		// Completing a child conducts the epic closure (F-0214, close-side
		// counterpart of the start-side propagation above): once no other alive
		// child is left pending, scaffold the epic's result with the children
		// roster so the author lands on a structured closeout instead of a blank
		// file. It never completes the epic — that stays a document + human gate.
		var epicReady string
		if action == domain.ActionComplete && f.Parent != "" {
			if epicReady, err = scaffoldEpicCloseIfReady(repoDir, f.Parent); err != nil {
				return "", err
			}
		}
		msg := fmt.Sprintf("cerne: %s %s", action, id)
		if len(propagated) > 0 {
			msg += fmt.Sprintf(" (+ epic %s)", strings.Join(propagated, ", "))
		}
		if epicReady != "" {
			msg += fmt.Sprintf(" (epic %s ready to close)", epicReady)
		}
		title = f.Title
		out = TransitionOutcome{ID: id, From: oldState, To: newState, Propagated: propagated, EpicReadyToClose: epicReady, Warnings: warnings, Bar: bar, Baseline: baseline, Pendings: movedPendings}
		return msg, nil
	})
	if err != nil {
		return TransitionOutcome{}, err
	}
	dispatchWebhook(projDir, id, "feature", string(action), string(out.From), string(out.To), title)
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
// (empty when none), and — when the walk stopped at an epic whose approval is
// stale (F-0318) — the line that says so, for the caller to print.
// A visited set guards against a cyclic parent link so a corrupt graph cannot
// loop forever.
func propagateStartToAncestors(repoDir, childID string) (started []string, staleNote string, err error) {
	cur, err := store.LoadFeature(repoDir, childID)
	if err != nil {
		return nil, "", err
	}
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
			return started, "", err
		}
		if parent.State != domain.StateApproved {
			break // candidate needs approval; in_development/terminal: nothing to do
		}
		docs, err := FeatureDocSet(repoDir, parent.ID)
		if err != nil {
			return started, "", err
		}
		if err := domain.CheckGate(parent.ID, domain.ActionStart, docs); err != nil {
			break // epic not ready (no real plan): don't force it, leave it to lint
		}
		if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, parent); stale {
			// The start gate refuses a stale approval (F-0318): so does this, out loud.
			return started, fmt.Sprintf("epic %s was not started along with %s: spec changed since approval — a human re-approves it with 'cerne feature approve %s'", parent.ID, childID, parent.ID), nil
		}
		newState, err := domain.Transition(parent.State, domain.ActionStart)
		if err != nil {
			break
		}
		parent.State = newState
		parent.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, parent); err != nil {
			return started, "", err
		}
		ev := domain.NewEvent(parent.ID, "feature.started", map[string]string{"via": childID}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return started, "", err
		}
		if err := writeResultStub(repoDir, parent.ID, parent.Title); err != nil {
			return started, "", err
		}
		started = append(started, parent.ID)
		cur = parent
	}
	return started, "", nil
}

// scaffoldEpicCloseIfReady checks whether parentID's children are all
// concluded (implemented/done) or dead (rejected/superseded) after a child
// just completed, and if so scaffolds the epic's result with the children
// roster. Returns the epic ID when it scaffolded, "" when the epic is not
// mid-flight or a live child is still pending — the mirror check of the start
// gate in propagateStartToAncestors, but it never advances the epic's state.
func scaffoldEpicCloseIfReady(repoDir, parentID string) (string, error) {
	parent, err := store.LoadFeature(repoDir, parentID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil // dangling parent: lint's parent-not-found reports it
	}
	if err != nil {
		return "", err
	}
	if parent.State != domain.StateInDevelopment {
		return "", nil // epic not mid-flight: nothing to close
	}
	all, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return "", err
	}
	children := store.ChildrenByParent(all)[parentID]
	for _, c := range children {
		if domain.TitleEditable(c.State) {
			return "", nil // still a live, unfinished child: nothing to do
		}
	}
	if err := writeEpicResultStub(repoDir, parent.ID, parent.Title, children); err != nil {
		return "", err
	}
	return parent.ID, nil
}

// keepApprovedSpec copies feature id's spec, byte for byte, to
// store.ApprovedSpecPath (F-0318): the version this approve accepted, committed
// with it, against which every later write is compared.
func keepApprovedSpec(repoDir, id string) error {
	data, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		return err
	}
	path := store.ApprovedSpecPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ValidateTransitionFlags enforces which flags each action accepts:
// supersede requires both, reject requires --reason only, every other
// action forbids both.
func ValidateTransitionFlags(action domain.Action, by, reason string) error {
	switch action {
	case domain.ActionSupersede:
		if by == "" {
			return fmt.Errorf("--by is required for substitution")
		}
		if reason == "" {
			return fmt.Errorf("--reason is required for substitution")
		}
	case domain.ActionReject:
		if by != "" {
			return fmt.Errorf("--by is not allowed for reject")
		}
		if reason == "" {
			return fmt.Errorf("--reason is required for reject")
		}
	case domain.ActionRevert:
		// Undoing an approval carries its why into the event log (F-0169).
		if by != "" {
			return fmt.Errorf("--by is not allowed for revert")
		}
		if reason == "" {
			return fmt.Errorf("--reason is required for revert")
		}
	case domain.ActionAccept:
		// The acceptance note is optional: the accept is an act, not a document.
		if by != "" {
			return fmt.Errorf("--by is not allowed for accept")
		}
	default:
		if by != "" || reason != "" {
			return fmt.Errorf("--by/--reason are not allowed for %s", action)
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

// EventActor returns the actor for events: CERNE_ACTOR resolved by
// resolveActor, "local" when unset.
func EventActor() string { return resolveActor("local") }

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

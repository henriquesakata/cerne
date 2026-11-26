// Package app hosts use-case orchestration shared by the CLI and the
// local UI server: operations that load entities, enforce domain rules
// and sync the backend clone.
package app

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// TransitionOutcome reports a feature transition that was applied and synced.
type TransitionOutcome struct {
	// AlreadyDone: an accept on a card already done (F-0506); nothing was written.
	AlreadyDone bool
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
	// JudgeQuestion is the judge question an approve accepted beside the bar
	// (F-0381), also on the approve event; "" when none is charged.
	JudgeQuestion string
	// Baseline is true when an approve on an approved feature recorded its
	// first approved copy (F-0318 crit. 14: approved before the copy existed)
	// rather than renewing a stale one.
	Baseline bool
	// RenewedApprovalAt is when the approval this approve renewed was given —
	// the feature's last feature.approved event (F-0417); zero for every other
	// action, a first approve and a baseline record.
	RenewedApprovalAt time.Time
	// Pendings holds one line per pending the feature paid that this
	// transition moved (F-0885: resolved on accept, reopened on reject,
	// transferred on supersede), for the caller to print. Empty otherwise.
	Pendings []string
	// Ideas is Pendings for the ideas promoted or linked to the feature
	// (F-0419): one line per idea moved.
	Ideas []string
	// Cascade holds one line per child an approve or accept carried along
	// (T-0007), and per candidate child an approve left behind, with why.
	Cascade []string
	// Risk is what the risk gate measured on a complete under [gates] accept
	// = "risk" (F-0516); nil for every other action and gate.
	Risk *RiskAssessment
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

// lastEventAt is the time of entity id's last event of type typ; zero when
// the log holds none.
func lastEventAt(repoDir, id, typ string) (time.Time, error) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return time.Time{}, err
	}
	var at time.Time
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == typ {
			at = ev.At
		}
	}
	return at, nil
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
	return transitionOwnOp(repoDir, projDir, id, action, by, reason, d, force, srcCLI)
}

// TransitionFeatureSeed is TransitionFeatureForce for a scripted seed (cerne
// demo, F-0515): nobody crosses a gate, so no card it moves is marked auto,
// whatever the environment declares.
func TransitionFeatureSeed(repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery, force bool) (TransitionOutcome, error) {
	return transitionOwnOp(repoDir, projDir, id, action, by, reason, d, force, srcSeed)
}

// transitionOwnOp runs a transition on an Op of its own.
func transitionOwnOp(repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery, force bool, src actSource) (TransitionOutcome, error) {
	op := telemetry.StartOp("backend")
	out, err := transitionFeature(op, repoDir, projDir, id, action, by, reason, d, force, nil, src)
	status := "ok"
	if err != nil {
		status = "error"
	}
	op.End(status)
	return out, err
}

// TransitionFeatureFromUI is the cerne ui form's transition. It records its
// phases on the caller's Op (F-0149): the server passes the request's Op so
// the transition's lock_wait/fetch/push story correlates with the HTTP log
// line. The person at the form is the human whatever env the server runs in,
// so no approve or accept through it is marked auto (F-0515).
func TransitionFeatureFromUI(op *telemetry.Op, repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery) (TransitionOutcome, error) {
	return transitionFeature(op, repoDir, projDir, id, action, by, reason, d, false, nil, srcUI)
}

// TransitionFeatureOpForce is TransitionFeature recording its phases on op,
// carrying the F-0257 guard override.
func TransitionFeatureOpForce(op *telemetry.Op, repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery, force bool) (TransitionOutcome, error) {
	return transitionFeature(op, repoDir, projDir, id, action, by, reason, d, force, nil, srcCLI)
}

// mergeAccept is an accept that transcribes the human's merge of the delivery
// PR (F-0488): the actor is the human, the shell that recorded it goes apart.
type mergeAccept struct {
	PRURL, MergedAt, RecordedBy string
}

// payload is the record of the merge on the accept event.
func (m *mergeAccept) payload() map[string]string {
	return map[string]string{"via": "merge", "pr": m.PRURL, "merged_at": m.MergedAt, "recorded_by": m.RecordedBy}
}

// mergeActor is the actor of an accept on merge: the merge was the human's.
const mergeActor = "human"

// transitionFeature is the one transition path; m is non-nil only for an
// accept on merge (F-0488), src says who stands behind the act (F-0515).
func transitionFeature(op *telemetry.Op, repoDir, projDir, id string, action domain.Action, by, reason string, d *Delivery, force bool, m *mergeAccept, src actSource) (TransitionOutcome, error) {
	if m != nil && action != domain.ActionAccept {
		return TransitionOutcome{}, fmt.Errorf("an acceptance on merge is an accept, not %s", action)
	}
	var out TransitionOutcome
	var title string
	var casc cascaded
	var autoAccepted bool // a complete [gates] accept = "auto" carried to done
	var baseline bool
	var renewedAt time.Time
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
			if f.State == domain.StateApproved && !baseline {
				if renewedAt, err = lastEventAt(repoDir, id, eventType); err != nil {
					return "", err
				}
			}
		}
		if action == domain.ActionStart {
			if err := CheckStartNotStale(repoDir, f); err != nil {
				return "", err
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
			if w := planBehindWarning(repoDir, f); w != "" {
				warnings = append(warnings, w)
			}
		}

		if action == domain.ActionAccept && f.State == domain.StateDone {
			out = TransitionOutcome{ID: id, From: f.State, To: f.State, AlreadyDone: true}
			return "", nil // idempotent (F-0506): writes nothing, surfaces as ErrNothingToCommit
		}
		newState, err := domain.Transition(f.State, action)
		if err != nil {
			return "", err
		}
		docs, err := FeatureDocSet(repoDir, id)
		if err != nil {
			return "", err
		}
		if err := checkActionDocs(f, action, docs); err != nil {
			return "", err
		}
		// Gauntlet bar gate (F-0295): a property of the spec, not of who
		// approves, so it lives here where CLI and UI share it — as a CLI-only
		// check the UI approved specs with no bar.
		// Light mode (F-0364) skips it: the project chose to run without the
		// Gauntlet, and status says so.
		gates := projectGates(projDir)
		gauntletOn := gates.GauntletEnabled()
		if action == domain.ActionApprove {
			if err := CheckSpecSections(repoDir, id); err != nil {
				return "", err
			}
		}
		if action == domain.ActionApprove && gauntletOn {
			if err := CheckSpecBar(repoDir, id); err != nil {
				return "", err
			}
			if err := CheckSpecJudgeQuestion(repoDir, id); err != nil {
				return "", err
			}
		}
		// A task has no approve: its start is the first gate before the work,
		// so its own bar is charged there (T-0007), before any write.
		if action == domain.ActionStart && gauntletOn {
			if err := checkTaskBar(repoDir, f); err != nil {
				return "", err
			}
		}

		// Result record gate (F-0303): the lint's result rules, charged while the
		// result is still editable — after complete it is frozen out of .docs/.
		if action == domain.ActionComplete {
			if err := CheckCompleteResult(repoDir, f, gates); err != nil {
				return "", err
			}
			if err := CheckCriterionTests(repoDir, projDir, f); err != nil {
				return "", err
			}
		}
		// The risk gate (F-0516) measures the delivery once the gates passed:
		// low risk is accepted below, anything else waits for the human merge.
		var risk *RiskAssessment
		if action == domain.ActionComplete && gates.AcceptGate() == project.GateRisk {
			ra := assessRisk(repoDir, projDir, f)
			risk = &ra
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
		case domain.ActionApprove:
			// F-0515: a person's re-approval clears an agent's earlier mark.
			f.ApprovedVia = autoVia(gates.ApproveGate(), agentAct(src))
		case domain.ActionAccept:
			// F-0515: an agent's own accept under accept = "auto"; the merge
			// (m, F-0488) is the human's and marks itself below.
			f.AcceptedVia = autoVia(gates.AcceptGate(), agentAct(src))
		case domain.ActionRevert:
			// An acceptance undone (F-0499) is no longer an acceptance on merge,
			// nor is an approval undone one without a human act (F-0515).
			if oldState == domain.StateDone {
				f.AcceptedVia = ""
			}
			if oldState == domain.StateApproved {
				f.ApprovedVia = ""
			}
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
		if m != nil {
			f.AcceptedVia = "merge"
		}
		if f.AcceptedVia != project.GateRisk {
			f.AcceptedRisk = "" // the summary belongs to a risk accept only (F-0516)
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
		// Light mode is part of the record (F-0364): an approve or complete
		// that skipped the Gauntlet says so on its event, auditable even after
		// .cernerc flips back.
		if !gauntletOn && (action == domain.ActionApprove || action == domain.ActionComplete) {
			if payload == nil {
				payload = map[string]string{}
			}
			payload["gauntlet"] = "off"
		}
		// So is what the risk gate measured (F-0516), accepted or held.
		if risk != nil {
			if payload == nil {
				payload = map[string]string{}
			}
			maps.Copy(payload, risk.payload())
		}
		// Approving is accepting the proposed bar (F-0308 crit. 8): the line the
		// gate just validated goes on the event, with the actor.
		var bar, judgeQuestion string
		if action == domain.ActionApprove {
			if bar, err = AcceptedBar(repoDir, id); err != nil {
				return "", err
			}
			if bar != "" {
				if payload == nil {
					payload = map[string]string{}
				}
				payload["bar"] = bar
				if judgeQuestion, _, err = AcceptedJudgeQuestion(repoDir, id); err != nil {
					return "", err
				}
				if judgeQuestion != "" {
					payload["judge_question"] = judgeQuestion
				}
			}
		}
		// The mark goes on the event too (F-0515); a merge carries its own.
		var via string
		switch action {
		case domain.ActionApprove:
			via = f.ApprovedVia
		case domain.ActionAccept:
			via = f.AcceptedVia
		}
		if via != "" && m == nil {
			if payload == nil {
				payload = map[string]string{}
			}
			payload["via"] = via
		}
		actor := EventActor()
		if m != nil {
			payload, actor = m.payload(), mergeActor
		}
		// Every accept records the Gauntlet round it saw, whatever its via, so
		// the risk gate can tell an old win from a new one after a revert (F-0516).
		if action == domain.ActionAccept {
			if payload == nil {
				payload = map[string]string{}
			}
			payload["gauntlet_round"] = gauntletRoundOf(repoDir, f)
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, eventType, payload, actor)); err != nil {
			return "", err
		}
		// A task that held its parent records its own measurements (F-0516).
		if risk != nil {
			for _, ht := range risk.HeldTasks {
				p := ht.Risk.payload()
				p["cascade_from"] = id
				if err := store.AppendEvent(repoDir, domain.NewEvent(ht.ID, "feature.risk_held", p, EventActor())); err != nil {
					return "", err
				}
			}
		}
		// End-of-flow bypass (F-0131): with [gates] accept = "auto" the complete
		// carries the feature straight to done in the same critical section —
		// both events land in one commit. Default (human) stops at implemented
		// ("Ready") and waits for the explicit human accept. Under the risk
		// gate (F-0516) only a delivery of low risk is carried, marked risk.
		autoAccepted = action == domain.ActionComplete && (projectGates(projDir).AcceptGate() == project.GateAuto || risk != nil && risk.Low())
		if autoAccepted {
			promoted, err := domain.Transition(f.State, domain.ActionAccept)
			if err != nil {
				return "", err
			}
			f.State = promoted
			acceptVia, acceptPayload := project.GateAuto, map[string]string{"via": project.GateAuto}
			if risk != nil {
				acceptVia, acceptPayload = project.GateRisk, risk.payload()
				acceptPayload["via"] = project.GateRisk
			}
			acceptPayload["gauntlet_round"] = gauntletRoundOf(repoDir, f)
			if src != srcSeed { // a seed crosses no gate, so it marks nothing
				f.AcceptedVia = acceptVia // F-0515: done with no human act
				if risk != nil {
					f.AcceptedRisk = risk.Summary()
				}
			}
			f.UpdatedAt = time.Now().UTC()
			if err := store.SaveFeature(repoDir, f); err != nil {
				return "", err
			}
			ev := domain.NewEvent(id, eventTypes[domain.ActionAccept], acceptPayload, EventActor())
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
		// So do the ideas promoted or linked to it (F-0419).
		movedIdeas, err := moveLinkedIdeas(repoDir, f, reason)
		if err != nil {
			return "", err
		}
		// So do its implemented tasks (T-0007), on an auto accept too (F-0515)
		// and on a risk one (F-0516).
		if action == domain.ActionAccept || autoAccepted {
			if casc, err = cascadeAccept(repoDir, projDir, f, m); err != nil {
				return "", err
			}
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
			// The children ride the parent's approve (T-0007), in this commit.
			if casc, err = cascadeApprove(repoDir, projDir, id, gauntletOn, agentAct(src)); err != nil {
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
			// A batch work ends with the last of its features (F-0422).
			if err := closeBatchWorks(repoDir, id); err != nil {
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
		if len(casc.moved) > 0 {
			ids := make([]string, len(casc.moved))
			for i, c := range casc.moved {
				ids[i] = c.ID
			}
			msg += fmt.Sprintf(" (+ %s)", strings.Join(ids, ", "))
		}
		title = f.Title
		out = TransitionOutcome{ID: id, From: oldState, To: newState, Propagated: propagated, EpicReadyToClose: epicReady, Warnings: warnings, Bar: bar, JudgeQuestion: judgeQuestion, Baseline: baseline, RenewedApprovalAt: renewedAt, Pendings: movedPendings, Ideas: movedIdeas, Cascade: casc.lines, Risk: risk}
		return msg, nil
	})
	if out.AlreadyDone && errors.Is(err, gitback.ErrNothingToCommit) {
		return out, nil
	}
	if err != nil {
		return TransitionOutcome{}, err
	}
	dispatchWebhook(projDir, id, "feature", string(action), string(out.From), string(out.To), title)
	if noticeActions[action] {
		if err := notifyCardSessions(repoDir, projDir, id, out.From, out.To, time.Now().UTC()); err != nil {
			out.Warnings = append(out.Warnings, "warning: card notice: "+err.Error())
		}
	}
	// A task an auto complete carried was accepted, not completed (F-0515).
	cascAction := action
	if autoAccepted {
		cascAction = domain.ActionAccept
	}
	for _, c := range casc.moved {
		dispatchWebhook(projDir, c.ID, "feature", string(cascAction), string(cascadeFrom[cascAction]), string(c.State), c.Title)
	}
	out.Warnings = append(out.Warnings, mirrorAfterTransition(repoDir, projDir, transitionIDs(id, out.Propagated, casc.moved))...)
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

// CheckStartNotStale is the stale-approval gate of the start (F-0318): the spec
// to implement is the one a human approved. The transition and NextStep (F-0371)
// both call it.
func CheckStartNotStale(repoDir string, f store.Feature) error {
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale {
		return fmt.Errorf("%w: %s — a human re-approves it with 'cerne feature approve %s' (or 'cerne feature revert %s' sends it back to the Backlog)", ErrApprovalStale, f.ID, f.ID, f.ID)
	}
	return nil
}

// planBehindWarning is the start's advisory when feature f's plan was written
// against an older spec (F-0377, lint.PlanBehindSpec): the start proceeds and
// the lint keeps the debt. "" when the plan is not behind. The transition and
// NextStep both print it.
func planBehindWarning(repoDir string, f store.Feature) string {
	behind, planAt, specAt := lint.PlanBehindSpec(repoDir, f)
	if !behind {
		return ""
	}
	return fmt.Sprintf("warning: %s plan predates the current spec (plan written %s, spec changed %s) — re-read the spec, update the plan (a line recording the re-check is enough) and push it", f.ID, lint.WriteTime(planAt), lint.WriteTime(specAt))
}

// checkActionDocs is the document gate of an action (a real plan to start, a
// real result to complete): domain.CheckGate over the feature's doc set, with
// the file to write named.
func checkActionDocs(f store.Feature, action domain.Action, docs domain.DocSet) error {
	if err := domain.CheckGate(f.ID, action, docs); err != nil {
		return fmt.Errorf("%w — %s", err, EditDoc(f, gateDocFilename(err)))
	}
	return nil
}

// CheckCompleteResult is the result-record part of the complete gate: the
// lint's result rules (F-0303) and the debts the result must cite (F-0885).
// The transition and NextStep (F-0371) both call it.
func CheckCompleteResult(repoDir string, f store.Feature, gates project.GatesConfig) error {
	if err := CheckResultRecord(repoDir, f, gates); err != nil {
		return fmt.Errorf("%w\n— %s", err, EditDoc(f, "result.md"))
	}
	return CheckPaidCited(repoDir, f)
}

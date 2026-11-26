package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// Acceptance on merge (F-0488): under [gates] accept = "merge" the human's
// merge of the delivery PR into the default branch is the acceptance, and
// cerne only transcribes it. Anyone may run the transcription, an agent
// included: the act it records already happened on the forge.

// ErrMergeGateOff is the sentinel for --merged outside the merge gate.
var ErrMergeGateOff = errors.New("the accept gate is not merge")

// ErrMergeGateNoForge is the sentinel for the merge gate with no forge to ask.
var ErrMergeGateNoForge = errors.New("no [delivery] forge to ask")

// MergeCheck is what AcceptMerged found for one card: accepted or not, and
// the line that says so (with the reason and next step when it was not).
type MergeCheck struct {
	ID       string
	Accepted bool
	Waiting  bool // the PR is still open: the normal wait for the human's merge
	Line     string
	Outcome  TransitionOutcome // the accept's outcome, when Accepted
}

// AcceptMerged accepts on merge the cards ids, or every implemented card when
// ids is empty: each one whose delivery PR the forge reports merged into the
// default branch goes to done; every other gets a line and stays. It fails
// only outside the merge gate, with no forge, or on an unknown id.
func AcceptMerged(repoDir, projDir string, cfg project.Config, ids []string) ([]MergeCheck, error) {
	if g := cfg.Gates.AcceptGate(); !cfg.Gates.AcceptsOnMerge() {
		return nil, fmt.Errorf("%w: --merged needs [gates] accept = %q or %q in .cernerc (this project's accept gate is %s)", ErrMergeGateOff, project.GateMerge, project.GateRisk, g)
	}
	if cfg.Delivery.Forge == "" {
		return nil, fmt.Errorf("%w: set [delivery] forge in .cernerc (github, gitlab or gitea) so cerne can ask whether the PR merged", ErrMergeGateNoForge)
	}
	var cards []store.Feature
	if len(ids) == 0 {
		all, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			return nil, err
		}
		for _, f := range all {
			if f.State == domain.StateImplemented {
				cards = append(cards, f)
			}
		}
	}
	for _, id := range ids {
		f, err := store.LoadFeature(repoDir, domain.CanonicalID(id))
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%s is not a feature in this project", id)
		}
		if err != nil {
			return nil, err
		}
		cards = append(cards, f)
	}
	checks := make([]MergeCheck, 0, len(cards))
	for _, f := range cards {
		// A parent accepted earlier in this sweep may have carried this task
		// along (its line already says so): not examined again.
		if len(ids) == 0 {
			if cur, err := store.LoadFeature(repoDir, f.ID); err == nil && cur.State != domain.StateImplemented {
				continue
			}
		}
		c := MergeCheck{ID: f.ID}
		// Asked by id, a done card was accepted already (F-0506): a success
		// line, and the forge is not asked.
		if f.State == domain.StateDone {
			c.Accepted, c.Line = true, f.ID+" was already done; nothing changed"
			c.Outcome = TransitionOutcome{ID: f.ID, From: f.State, To: f.State, AlreadyDone: true}
			checks = append(checks, c)
			continue
		}
		m, why, open := mergeOf(cfg, repoDir, projDir, f)
		if m == nil {
			c.Line, c.Waiting = f.ID+": "+why, open
			checks = append(checks, c)
			continue
		}
		op := telemetry.StartOp("backend")
		out, err := transitionFeature(op, repoDir, projDir, f.ID, domain.ActionAccept, "", "", nil, false, m, srcCLI)
		if err != nil {
			op.End("error")
			c.Line = fmt.Sprintf("%s: the PR merged but the accept failed: %v", f.ID, err)
			checks = append(checks, c)
			continue
		}
		op.End("ok")
		c.Accepted, c.Outcome = true, out
		c.Line = fmt.Sprintf("%s: %s → %s (accepted on merge of %s)", f.ID, out.From, out.To, m.PRURL)
		checks = append(checks, c)
	}
	return checks, nil
}

// mergeOf asks the forge about f's delivery PR: the merge to record, or why
// there is none (and what to do), and whether the PR is simply still open.
// In doubt, never a merge.
func mergeOf(cfg project.Config, repoDir, projDir string, f store.Feature) (m *mergeAccept, why string, open bool) {
	if f.State != domain.StateImplemented {
		return nil, fmt.Sprintf("is %s, not implemented: nothing to accept", f.State), false
	}
	url := f.DeliveryMRURL
	if url == "" {
		return nil, fmt.Sprintf("no delivery PR to ask about: link it with 'cerne feature deliver %s --mr <url>'", f.ID), false
	}
	// A merge is transcribed once (F-0499): back on implemented after that
	// accept, the card was reverted by a human, and the same merge does not
	// undo the revert.
	recorded, err := mergeRecorded(repoDir, f.ID, url)
	if err != nil {
		return nil, fmt.Sprintf("the event log could not be read (%v); nothing accepted", err), false
	}
	if recorded {
		return nil, fmt.Sprintf("the merge of %s was already recorded as the acceptance and then reverted: link a new delivery with 'cerne feature deliver %s --mr <url>', or a human accepts with 'cerne feature accept %s'", url, f.ID, f.ID), false
	}
	repo, n, ok := forge.ParsePRURL(url)
	if !ok {
		return nil, fmt.Sprintf("%s is not a PR URL: link the PR with 'cerne feature deliver %s --mr <url>'", url, f.ID), false
	}
	fg, reason := forgeOnOrigin(cfg, projDir, repo, "the PR")
	if fg == nil {
		return nil, "not asked: " + reason, false
	}
	pr, err := fg.PullRequest(context.Background(), n)
	if err != nil {
		return nil, fmt.Sprintf("the forge did not answer (%s); nothing accepted", reasonOf(err)), false
	}
	switch {
	case !pr.Closed:
		return nil, fmt.Sprintf("%s is still open: the human's merge into %s is the acceptance", url, pr.DefaultBranch), true
	case !pr.Merged:
		return nil, fmt.Sprintf("%s was closed without merge: a human accepts with 'cerne feature accept %s', or reopens and merges the PR", url, f.ID), false
	case pr.DefaultBranch == "" || pr.Base != pr.DefaultBranch:
		return nil, fmt.Sprintf("%s merged into %s, not into %s: a human accepts with 'cerne feature accept %s' once the work reaches %s", url, pr.Base, pr.DefaultBranch, f.ID, pr.DefaultBranch), false
	}
	return &mergeAccept{PRURL: url, MergedAt: pr.MergedAt, RecordedBy: EventActor()}, "", false
}

// mergeRecorded reports whether the log holds an acceptance of id on the
// merge of the PR at url.
func mergeRecorded(repoDir, id, url string) (bool, error) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return false, err
	}
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == eventTypes[domain.ActionAccept] && ev.Payload["pr"] == url {
			return true, nil
		}
	}
	return false, nil
}

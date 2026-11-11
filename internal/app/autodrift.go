package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/store"
)

// DriftAutoActor is the agent actor stamped on automatic drift erratas, so the
// event log distinguishes them from human ones (D-0005).
const DriftAutoActor = "drift-auto"

// ErrEmptyAutoErrata is returned when the provider replies with no usable text —
// nothing is applied in that case.
var ErrEmptyAutoErrata = errors.New("provider replied empty — errata not applied")

// autoErrataInstruction is appended to the delegation prompt when the errata is
// drafted by a called provider instead of the host agent: the raw completion is
// applied verbatim, so it must be the errata text alone.
const autoErrataInstruction = "\nIMPORTANT: reply with ONLY the errata text, with no preamble, no code fences, and without repeating the document.\n"

// AutoDriftErrata closes the drift loop for one finding (F-0115, D-0005
// apply-then-notify): it builds the same grounded prompt --draft emits, asks
// the provider to write the errata, applies it via AddErrata under the
// drift-auto actor, and records a drift.auto event as the pending-review
// notification. The entity's historical body is never rewritten — the errata is
// additive. Returns the applied text.
func AutoDriftErrata(ctx context.Context, repoDir, projDir, entityID, baseline string, changed []string, p llm.Provider) (string, error) {
	prompt, err := DriftDraftPrompt(repoDir, entityID, baseline, changed)
	if err != nil {
		return "", err
	}
	reply, err := p.Complete(ctx, prompt+autoErrataInstruction)
	if err != nil {
		return "", fmt.Errorf("%s: %w", entityID, err)
	}
	text := strings.TrimSpace(reply)
	if text == "" {
		return "", fmt.Errorf("%w: %s", ErrEmptyAutoErrata, entityID)
	}
	if _, err := AddErrata(repoDir, projDir, entityID, text, DriftAutoActor); err != nil {
		return "", err
	}
	short := baseline
	if len(short) > 7 {
		short = short[:7]
	}
	// The provider call above stays outside any lock (it can take minutes —
	// lockStaleAfter would let another process steal the lock mid-section);
	// only this event append runs in the critical section (F-0122).
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		ev := domain.NewEvent(entityID, "drift.auto", map[string]string{
			"baseline": short,
			"paths":    strings.Join(changed, ","),
		}, DriftAutoActor)
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return "cerne: drift auto " + entityID, nil
	})
	if err != nil {
		return "", err
	}
	return text, nil
}

// PendingReview is one automatic drift update a human has not acknowledged yet.
type PendingReview struct {
	Entity   string
	At       time.Time
	Baseline string
	Paths    string
	EventIDs []string
}

// PendingDriftReviews folds the event log into the set of drift.auto updates
// not yet covered by a later drift.ack for the same entity (F-0115). Pure read:
// pending state is derived, never stored. Sorted by entity id.
func PendingDriftReviews(repoDir string) ([]PendingReview, error) {
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return nil, err
	}
	open := make(map[string]*PendingReview)
	for _, ev := range events {
		switch ev.Type {
		case "drift.auto":
			pr, ok := open[ev.EntityID]
			if !ok {
				pr = &PendingReview{Entity: ev.EntityID}
				open[ev.EntityID] = pr
			}
			pr.At = ev.At
			pr.Baseline = ev.Payload["baseline"]
			pr.Paths = ev.Payload["paths"]
			pr.EventIDs = append(pr.EventIDs, ev.ID)
		case "drift.ack":
			delete(open, ev.EntityID)
		}
	}
	out := make([]PendingReview, 0, len(open))
	for _, pr := range open {
		out = append(out, *pr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entity < out[j].Entity })
	return out, nil
}

// ErrNoPendingDrift is the sentinel for an AckDrift on an entity with nothing
// pending.
var ErrNoPendingDrift = errors.New("no pending automatic update")

// AckDrift acknowledges every pending drift.auto of an entity: it appends a
// drift.ack event (human actor, payload citing the covered event ids) and
// commits+pushes. ErrNoPendingDrift when the entity has nothing pending.
func AckDrift(repoDir, projDir, entityID, actor string) error {
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		pending, err := PendingDriftReviews(repoDir)
		if err != nil {
			return "", err
		}
		var covers []string
		for _, pr := range pending {
			if pr.Entity == entityID {
				covers = pr.EventIDs
				break
			}
		}
		if len(covers) == 0 {
			return "", fmt.Errorf("%w: %s", ErrNoPendingDrift, entityID)
		}
		ev := domain.NewEvent(entityID, "drift.ack", map[string]string{"covers": strings.Join(covers, ",")}, actor)
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return "cerne: drift ack " + entityID, nil
	})
}

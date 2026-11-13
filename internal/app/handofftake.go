package app

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrHandoffNotPending is the sentinel for a take or cancel of a handoff that
// was already taken, superseded or cancelled (F-0368 crit. 4).
var ErrHandoffNotPending = errors.New("handoff is not pending")

// ErrMalformedHandoffID is the sentinel for an id that is not HO-<digits>,
// refused before the store is touched.
var ErrMalformedHandoffID = errors.New("malformed handoff id")

var handoffIDRe = regexp.MustCompile(`^HO-\d+$`)

// canonicalHandoffID upper-cases id and checks it is HO-<digits>.
func canonicalHandoffID(id string) (string, error) {
	c := strings.ToUpper(strings.TrimSpace(id))
	if !handoffIDRe.MatchString(c) {
		return "", fmt.Errorf("%w: %q (want HO-NNNN)", ErrMalformedHandoffID, id)
	}
	return c, nil
}

// TakeResult is what TakeHandoff did: the new work (with the taken handoff in
// Taken), the brief to read first, and whether the origin was closed — false
// when it was no longer active and was left as it is (crit. 5). Warning is a
// local session-binding failure after the commit; the take itself stands.
type TakeResult struct {
	StartedWork
	Brief         string
	OriginClosed  bool
	OriginState   domain.WorkState // the origin's state when it was left as it is
	OriginMissing bool             // the origin could not be loaded: treated as not active
	DroppedRefs   []string         // the handoff's refs that no longer resolve, left off the new work
	Warning       string
}

// TakeHandoff (F-0368 crit. 3) takes pending handoff id in one commit: opens
// a new work with the origin's title (or title) and refs, marks the HO taken
// (by, work, session sess, at, note), closes the origin as done "handed off to
// <new> via <HO>" when it is still active, and appends the events. After the
// commit, the new work is bound to session sess (when set) in cacheDir. A
// handoff that is not pending is refused, writing nothing (crit. 4).
func TakeHandoff(repoDir, projDir, cacheDir, sess, id, title, note string) (TakeResult, error) {
	id, err := canonicalHandoffID(id)
	if err != nil {
		return TakeResult{}, err
	}
	var out TakeResult
	var fromTitle string
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		h, err := loadPendingHandoff(repoDir, id)
		if err != nil {
			return "", err
		}
		// An origin that cannot be loaded is not active: the take proceeds without it.
		origin, err := store.LoadWork(repoDir, h.FromWork)
		missing := err != nil
		if missing {
			origin = store.Work{ID: h.FromWork, Title: h.FromWork + " (taken from " + id + ")"}
		}
		// Refs that no longer resolve are dropped, not a reason to refuse the baton.
		var refs, dropped []string
		for _, r := range h.Refs {
			if store.EntityExists(repoDir, r) {
				refs = append(refs, r)
			} else {
				dropped = append(dropped, r)
			}
		}
		body, err := store.LoadHandoffContent(repoDir, id)
		if err != nil {
			return "", err
		}
		p := startParams{title: strings.TrimSpace(title), refs: refs, finalRefs: true, actor: defaultWorkActor(), ho: &h, sess: sess, note: strings.TrimSpace(note)}
		if p.title == "" {
			p.title = origin.Title
		}
		// The handoff is the proof of continuity: the duplicate guard does not apply.
		p.force = true
		if !missing && origin.State == domain.WorkActive {
			p.from = origin.ID
		}
		sw, ft, msg, err := startWorkTx(repoDir, projDir, p)
		out, fromTitle = TakeResult{StartedWork: sw, Brief: strings.TrimSpace(body), OriginClosed: p.from != "", OriginState: origin.State,
			OriginMissing: missing, DroppedRefs: dropped}, ft
		return msg, err
	})
	if err != nil {
		return TakeResult{}, err
	}
	dispatchStarted(projDir, out.StartedWork, fromTitle)
	if out.OriginClosed {
		if err := ClearBindings(cacheDir, out.From); err != nil {
			out.Warning = "clearing the origin's session bindings: " + err.Error()
		}
	}
	if sess != "" {
		if err := BindSessionAgent(cacheDir, sess, "", cost.CurrentHarness(), out.ID); err != nil {
			out.Warning = "binding the session to " + out.ID + ": " + err.Error()
		}
	}
	return out, nil
}

// CancelHandoff (F-0368 crit. 8) withdraws pending handoff id, with a
// mandatory reason, in one commit.
func CancelHandoff(repoDir, projDir, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("--reason is required to cancel a handoff")
	}
	id, err := canonicalHandoffID(id)
	if err != nil {
		return err
	}
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		h, err := loadPendingHandoff(repoDir, id)
		if err != nil {
			return "", err
		}
		if h.State, err = domain.TransitionHandoff(h.State, domain.ActionCancel); err != nil {
			return "", err
		}
		h.CancelReason, h.UpdatedAt = reason, time.Now().UTC()
		if err := store.SaveHandoff(repoDir, h); err != nil {
			return "", err
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "handoff.cancelled", map[string]string{"reason": reason}, EventActor())); err != nil {
			return "", err
		}
		return "cerne: cancel " + id, nil
	})
}

// loadPendingHandoff loads id and refuses, saying why, unless it is pending.
func loadPendingHandoff(repoDir, id string) (store.Handoff, error) {
	h, err := store.LoadHandoff(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return h, fmt.Errorf("handoff %s not found in the backend: %w", id, err)
	}
	if err != nil {
		return h, err
	}
	switch h.State {
	case domain.HandoffPending:
		return h, nil
	case domain.HandoffTaken:
		at := ""
		if h.TakenAt != nil {
			at = " at " + h.TakenAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		return h, fmt.Errorf("%w: %s was taken by %s%s (%s)", ErrHandoffNotPending, id, h.TakenBy, at, h.TakenWork)
	case domain.HandoffSuperseded:
		// Follow the chain to the current successor (a cycle or a missing link ends it).
		last := h
		for seen := map[string]bool{h.ID: true}; last.State == domain.HandoffSuperseded && !seen[last.SupersededBy]; {
			seen[last.SupersededBy] = true
			next, err := store.LoadHandoff(repoDir, last.SupersededBy)
			if err != nil {
				break
			}
			last = next
		}
		if last.State == domain.HandoffPending {
			return h, fmt.Errorf("%w: %s is superseded by %s — the current one is %s; take that one: cerne handoff take %s", ErrHandoffNotPending, id, h.SupersededBy, last.ID, last.ID)
		}
		return h, fmt.Errorf("%w: %s is superseded by %s — the chain ended in %s, which is %s: nothing to take", ErrHandoffNotPending, id, h.SupersededBy, last.ID, last.State)
	case domain.HandoffCancelled:
		return h, fmt.Errorf("%w: %s was cancelled: %s", ErrHandoffNotPending, id, h.CancelReason)
	}
	return h, fmt.Errorf("%w: %s is %s", ErrHandoffNotPending, id, h.State)
}

// markTaken records h as taken by actor into work, from session sess.
func markTaken(repoDir string, h store.Handoff, actor, work, sess, note string, now time.Time) (store.Handoff, error) {
	var err error
	if h.State, err = domain.TransitionHandoff(h.State, domain.ActionTake); err != nil {
		return h, err
	}
	h.TakenBy, h.TakenWork, h.TakenSession, h.TakenNote = actor, work, sess, note
	h.TakenAt, h.UpdatedAt = &now, now
	if err := store.SaveHandoff(repoDir, h); err != nil {
		return h, err
	}
	return h, store.AppendEvent(repoDir, domain.NewEvent(h.ID, "handoff.taken", map[string]string{"work": work, "session": sess}, EventActor()))
}

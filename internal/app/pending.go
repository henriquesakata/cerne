package app

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrPendingFromRequired is returned by CreatePending without an origin: a
// pending is always a debt of a concrete delivery (F-0322), which is what
// keeps it apart from a speculative idea.
var ErrPendingFromRequired = errors.New("--from is required (the F-/T-/E- id the pending was left by)")

// ErrPendingFromInvalid is returned by CreatePending for an origin that is not
// an F-/T-/E-NNNN id — refused before the store is read, so a path-like value
// never reaches the filesystem.
var ErrPendingFromInvalid = errors.New("--from must be an F-/T-/E-NNNN id")

var pendingOriginRx = regexp.MustCompile(`^[FTE]-\d{4}$`)

// ErrPendingReasonRequired is returned by DropPending with a blank reason: a
// discarded debt keeps why it was discarded.
var ErrPendingReasonRequired = errors.New("--reason is required to drop a pending")

// ErrPendingPromoteKind is returned by PromotePending for a target other than
// a feature or a task.
var ErrPendingPromoteKind = errors.New("--as must be feature or task")

// ErrPendingOriginClosed is returned by PromotePending --as task when the
// origin is implemented or done: a task filed under a closed parent leaves an
// epic with open children.
var ErrPendingOriginClosed = errors.New("the origin is closed (implemented/done): a task cannot be filed under it — promote with --as feature")

// CreatePending mints an open pending (P-) in the backend clone at repoDir.
// from is mandatory and must be an existing feature/task/epic. Tags are
// normalized; an axis whose flag was not given inherits the origin's tags. A
// similar title on an OPEN pending of the same origin refuses (the F-0147
// guard, scoped by origin); force overrides it and records guard=forced on
// pending.created. Everything is validated before any write.
func CreatePending(repoDir, projDir, title, body, from string, facets, components []string, force bool) (store.Pending, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return store.Pending{}, fmt.Errorf("the pending title cannot be empty")
	}
	from = strings.ToUpper(strings.TrimSpace(from))
	if from == "" {
		return store.Pending{}, ErrPendingFromRequired
	}
	if !pendingOriginRx.MatchString(from) {
		return store.Pending{}, fmt.Errorf("%w: %q", ErrPendingFromInvalid, from)
	}
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Pending{}, err
	}
	normComponents := domain.NormalizeComponents(components)
	var p store.Pending
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		origin, err := store.LoadFeature(repoDir, from)
		if err != nil {
			return "", fmt.Errorf("--from: %w", err)
		}
		from = origin.ID // the stored id, never the typed spelling
		if len(facets) == 0 {
			normFacets = origin.Facets
		}
		if len(components) == 0 {
			normComponents = origin.Components
		}
		forced, err := guardPending(repoDir, title, from, force)
		if err != nil {
			return "", err
		}
		id, err := store.NextID(repoDir, "P")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		p = store.Pending{
			ID:         id,
			Title:      title,
			Body:       strings.TrimSpace(body),
			From:       from,
			State:      domain.PendingOpen,
			Facets:     normFacets,
			Components: normComponents,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := store.SavePending(repoDir, p); err != nil {
			return "", err
		}
		payload := map[string]string{"title": title, "from": from}
		if forced {
			payload["guard"] = "forced"
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "pending.created", payload, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: create %s (%s)", id, title), nil
	})
	if err != nil {
		return store.Pending{}, err
	}
	return p, nil
}

// ResolvePending closes an open pending as paid: open -> resolved, closed_at
// stamped. Not a human gate (F-0322): whoever resolved it closes it.
func ResolvePending(repoDir, projDir, id string) (store.Pending, error) {
	return closePending(repoDir, projDir, id, domain.ActionResolve, "", "")
}

// DropPending discards an open pending: open -> dropped, the reason persisted.
func DropPending(repoDir, projDir, id, reason string) (store.Pending, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return store.Pending{}, ErrPendingReasonRequired
	}
	return closePending(repoDir, projDir, id, domain.ActionDrop, reason, "")
}

// PromotePending turns an open pending into a feature (candidate) or a task
// (under the origin, which the task core requires past candidacy), inheriting
// its title, tags, the origin's module and the body as the spec's context. The
// spec carries "**Origem:** pendência [[P-NNNN]] (de <origin>)" — the link
// back, an id wiki-link — and the pending moves to promoted with promoted_to.
// force is the created entity's duplicate-guard override (F-0147): a debt
// often reads like its origin.
//
// ponytail: two mutations, not one transaction. If the second fails, the
// entity exists without promoted_to; the error names it, a retry hits the
// duplicate guard on the created title, and 'cerne pending resolve' closes it.
func PromotePending(repoDir, projDir, id, as string, force bool) (store.Pending, store.Feature, error) {
	if as != "feature" && as != "task" {
		return store.Pending{}, store.Feature{}, fmt.Errorf("%w: %q", ErrPendingPromoteKind, as)
	}
	p, err := store.LoadPending(repoDir, id)
	if err != nil {
		return store.Pending{}, store.Feature{}, err
	}
	if _, err := domain.TransitionPending(p.State, domain.ActionPromote); err != nil {
		return store.Pending{}, store.Feature{}, fmt.Errorf("%s: %w", id, err)
	}
	origin, err := store.LoadFeature(repoDir, p.From)
	if err != nil {
		return store.Pending{}, store.Feature{}, err
	}
	create, parent := CreateFeatureWithSpec, ""
	if as == "task" {
		if origin.State == domain.StateImplemented || origin.State == domain.StateDone {
			return store.Pending{}, store.Feature{}, fmt.Errorf("%w: %s is %s", ErrPendingOriginClosed, origin.ID, origin.State)
		}
		create, parent = CreateTaskWithSpec, p.From
	}
	context := fmt.Sprintf("**Origem:** pendência [[%s]] (de %s)", p.ID, p.From)
	if p.Body != "" {
		context += "\n\n" + p.Body
	}
	f, err := create(repoDir, projDir, p.Title, origin.Module, parent, p.Facets, p.Components, spectmpl.StubWithContext(p.Title, context), force, "", "")
	if err != nil {
		return store.Pending{}, store.Feature{}, err
	}
	p, err = closePending(repoDir, projDir, id, domain.ActionPromote, "", f.ID)
	if err != nil {
		return store.Pending{}, f, fmt.Errorf("%s created, but %s was not marked promoted (close it with 'cerne pending resolve %s'): %w", f.ID, id, id, err)
	}
	return p, f, nil
}

// closePending applies action to pending id, re-reading its state inside the
// backend lock, stamps closed_at (plus reason / promoted_to when given) and
// appends pending.<state>.
func closePending(repoDir, projDir, id string, action domain.Action, reason, promotedTo string) (store.Pending, error) {
	var p store.Pending
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		p, err = store.LoadPending(repoDir, id)
		if err != nil {
			return "", err
		}
		to, err := domain.TransitionPending(p.State, action)
		if err != nil {
			return "", fmt.Errorf("%s: %w", id, err)
		}
		now := time.Now().UTC()
		p.State, p.Reason, p.PromotedTo, p.UpdatedAt, p.ClosedAt = to, reason, promotedTo, now, &now
		if err := store.SavePending(repoDir, p); err != nil {
			return "", err
		}
		payload := map[string]string{}
		if reason != "" {
			payload["reason"] = reason
		}
		if promotedTo != "" {
			payload["promoted_to"] = promotedTo
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "pending."+string(to), payload, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: %s %s", action, id), nil
	})
	if err != nil {
		return store.Pending{}, err
	}
	return p, nil
}

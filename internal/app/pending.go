package app

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
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

// ErrPendingClosedConcurrently is returned by a promotion whose pending was
// closed (resolved/dropped/promoted) by someone else between creating the
// feature/task and recording the promotion.
var ErrPendingClosedConcurrently = errors.New("the pending was closed concurrently")

// pendingStateFilters are the values a state filter takes: every pending
// state, then all.
func pendingStateFilters() []string {
	states, _ := domain.ValidStates("pending")
	return append(states, "all")
}

// ErrPendingStateInvalid is a state filter that is not a pending state nor
// all; its text lists the accepted values, for the caller to prefix with the
// value (the CLI as `--state "x": …`, ListPendings as `invalid pending state`).
var ErrPendingStateInvalid = errors.New("want one of " + strings.Join(pendingStateFilters(), ", "))

// ValidPendingState reports whether state is a pending state or all. Empty is
// not: ListPendings reads it as open, the CLI refuses it.
func ValidPendingState(state string) bool {
	return slices.Contains(pendingStateFilters(), state)
}

// PendingFilter selects pendings for ListPendings: State "" means open and
// all keeps every state; From is canonized (trimmed, upper-cased) and
// Component slugged; Module matches the origin's module. Empty fields do not
// filter.
type PendingFilter struct{ State, From, Component, Module string }

// ListPendings is the one pending selection the CLI list and the ui route
// share (F-0334), in ascending id order and never nil (the JSON's []).
func ListPendings(repoDir string, f PendingFilter) ([]store.Pending, error) {
	state := f.State
	if state == "" {
		state = string(domain.PendingOpen)
	}
	if !ValidPendingState(state) {
		return nil, fmt.Errorf("invalid pending state %q: %w", state, ErrPendingStateInvalid)
	}
	pendings, err := store.LoadAllPendings(repoDir)
	if err != nil {
		return nil, err
	}
	moduleOf := map[string]string{}
	if f.Module != "" {
		features, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			return nil, err
		}
		for _, feat := range features {
			moduleOf[feat.ID] = feat.Module
		}
	}
	comp := slug.Make(f.Component)
	from := strings.ToUpper(strings.TrimSpace(f.From)) // origins are stored canonical
	shown := []store.Pending{}
	for _, p := range pendings {
		if (state != "all" && string(p.State) != state) || (from != "" && p.From != from) ||
			(comp != "" && !slices.Contains(p.Components, comp)) || (f.Module != "" && moduleOf[p.From] != f.Module) {
			continue
		}
		shown = append(shown, p)
	}
	return shown, nil
}

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
// module overrides the inherited one (needed when the project requires a
// module and the origin has none). force is the created entity's
// duplicate-guard override (F-0147): a debt often reads like its origin.
//
// ponytail: two mutations, not one transaction (the second re-checks the
// pending inside the lock). If it fails, the entity exists without
// promoted_to; markPromoted's error names it and says how to recover. One
// transaction would need the feature core to run inside this lock.
func PromotePending(repoDir, projDir, id, as, module string, force bool) (store.Pending, store.Feature, error) {
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
	if module = strings.TrimSpace(module); module == "" {
		module = origin.Module
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
	f, err := create(repoDir, projDir, p.Title, module, parent, p.Facets, p.Components, spectmpl.StubWithContext(p.Title, context), force, "", "")
	if err != nil {
		return store.Pending{}, store.Feature{}, err
	}
	p, err = markPromoted(repoDir, projDir, id, f.ID)
	if err != nil {
		return store.Pending{}, f, err
	}
	return p, f, nil
}

// markPromoted is the promotion's second step: open -> promoted with
// promoted_to = created, the state re-read inside the backend lock. Its errors
// name the created entity and the way out: closed meanwhile (nothing to fix on
// the pending), or a backend failure (the pending may still be open).
func markPromoted(repoDir, projDir, id, created string) (store.Pending, error) {
	p, err := closePending(repoDir, projDir, id, domain.ActionPromote, "", created)
	switch {
	case err == nil:
		return p, nil
	case errors.Is(err, domain.ErrInvalidTransition):
		return store.Pending{}, fmt.Errorf("%w: %s was created, but %s was closed by someone else meanwhile (%v), so it does not point to %s — decide whether %s is still wanted",
			ErrPendingClosedConcurrently, created, id, err, created, created)
	default:
		return store.Pending{}, fmt.Errorf("%s was created, but recording the promotion on %s failed: %w — %s may still be open: once the backend is reachable, check 'cerne pending show %s' and, if it is open, record the link with 'cerne pending drop %s --reason \"promoted to %s\"'",
			created, id, err, id, id, id, created)
	}
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

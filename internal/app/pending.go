package app

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/slug"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
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
	return closePending(repoDir, projDir, id, domain.ActionResolve, "")
}

// DropPending discards an open pending: open -> dropped, the reason persisted.
func DropPending(repoDir, projDir, id, reason string) (store.Pending, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return store.Pending{}, ErrPendingReasonRequired
	}
	return closePending(repoDir, projDir, id, domain.ActionDrop, reason)
}

// ErrPendingGroupTitle is returned by PromotePendings for two or more pendings
// without a title: the group has no single title to inherit.
var ErrPendingGroupTitle = errors.New("--title is required to promote two or more pendings into one feature")

// ErrPendingRepeated is returned by PromotePendings when a pending is listed
// twice.
var ErrPendingRepeated = errors.New("pending listed twice")

// ErrPendingIDInvalid is a pending id that is not P-NNNN — refused before the
// store is read, so a path-like value never reaches the filesystem.
var ErrPendingIDInvalid = errors.New("not a pending id (P-NNNN)")

// ErrPendingModuleDiverges is returned by PromotePendings when the origins
// belong to different modules and no module was given: it is not guessed.
var ErrPendingModuleDiverges = errors.New("the origins belong to different modules — pass --module")

// ErrPendingTaskOrigins is returned by PromotePendings --as task for pendings
// of more than one origin: a task has a single parent.
var ErrPendingTaskOrigins = errors.New("--as task needs pendings of a single origin (the task's parent)")

var pendingIDRx = regexp.MustCompile(`^P-\d{4,}$`)

// canonPendingID trims and upper-cases a typed pending id and refuses one that
// is not P-NNNN.
func canonPendingID(typed string) (string, error) {
	id := strings.ToUpper(strings.TrimSpace(typed))
	if !pendingIDRx.MatchString(id) {
		return "", fmt.Errorf("%w: %q", ErrPendingIDInvalid, typed)
	}
	return id, nil
}

// PromotePending is PromotePendings for a single pending, whose title the
// created entity inherits.
func PromotePending(repoDir, projDir, id, as, module string, force bool) (store.Pending, store.Feature, error) {
	ps, f, err := PromotePendings(repoDir, projDir, []string{id}, as, "", module, force)
	if len(ps) == 0 {
		return store.Pending{}, f, err
	}
	return ps[0], f, err
}

// PromotePendings turns one or more open pendings into ONE feature (candidate)
// or task (under their single origin, which the task core requires past
// candidacy) — the group that pays them (F-0885). The created entity takes the
// union of the pendings' tags and the module common to their origins (module
// overrides it, and is required when they diverge). Its spec opens with the
// link back, "**Origem:** pendência(s) [[P-NNNN]] (de <origin>)", followed by
// each pending's body. Each pending moves to promoted with promoted_to, and
// follows that feature's lifecycle from then on (see movePaidPendings). One
// pending inherits its title; two or more need title. Every pending is
// validated (exists, open, listed once) before anything is created. force is
// the created entity's duplicate-guard override (F-0147): a debt often reads
// like its origin.
//
// ponytail: two mutations, not one transaction (the second re-checks the
// pendings inside the lock). If it fails, the entity exists without
// promoted_to; markPromoted's error names it, each pending left unmarked and
// how to link it. One transaction would need the feature core to run inside
// this lock.
func PromotePendings(repoDir, projDir string, ids []string, as, title, module string, force bool) ([]store.Pending, store.Feature, error) {
	if as != "feature" && as != "task" {
		return nil, store.Feature{}, fmt.Errorf("%w: %q", ErrPendingPromoteKind, as)
	}
	if len(ids) == 0 {
		return nil, store.Feature{}, errors.New("no pending to promote")
	}
	if title = strings.TrimSpace(title); title == "" && len(ids) > 1 {
		return nil, store.Feature{}, ErrPendingGroupTitle
	}
	var ps []store.Pending
	for _, raw := range ids {
		id, err := canonPendingID(raw)
		if err != nil {
			return nil, store.Feature{}, err
		}
		if slices.ContainsFunc(ps, func(p store.Pending) bool { return p.ID == id }) {
			return nil, store.Feature{}, fmt.Errorf("%w: %s", ErrPendingRepeated, id)
		}
		p, err := store.LoadPending(repoDir, id)
		if err != nil {
			return nil, store.Feature{}, err
		}
		if _, err := domain.TransitionPending(p.State, domain.ActionPromote); err != nil {
			return nil, store.Feature{}, fmt.Errorf("%s: %w", id, err)
		}
		ps = append(ps, p)
	}
	var origins []store.Feature
	var modules, facets, components []string
	for _, p := range ps {
		if !slices.ContainsFunc(origins, func(o store.Feature) bool { return o.ID == p.From }) {
			origin, err := store.LoadFeature(repoDir, p.From)
			if err != nil {
				return nil, store.Feature{}, err
			}
			origins = append(origins, origin)
			if !slices.Contains(modules, origin.Module) {
				modules = append(modules, origin.Module)
			}
		}
		facets = appendMissing(facets, p.Facets)
		components = appendMissing(components, p.Components)
	}
	if module = strings.TrimSpace(module); module == "" {
		if len(modules) > 1 {
			return nil, store.Feature{}, fmt.Errorf("%w: %s", ErrPendingModuleDiverges, strings.Join(modules, ", "))
		}
		module = modules[0]
	}
	create, parent := CreateFeatureWithSpec, ""
	if as == "task" {
		if len(origins) > 1 {
			return nil, store.Feature{}, fmt.Errorf("%w: %s and %s", ErrPendingTaskOrigins, origins[0].ID, origins[1].ID)
		}
		if origin := origins[0]; origin.State == domain.StateImplemented || origin.State == domain.StateDone {
			return nil, store.Feature{}, fmt.Errorf("%w: %s is %s", ErrPendingOriginClosed, origin.ID, origin.State)
		}
		create, parent = CreateTaskWithSpec, origins[0].ID
	}
	if title == "" {
		title = ps[0].Title
	}
	f, err := create(repoDir, projDir, title, module, parent, facets, components, spectmpl.StubWithContext(title, promotedContext(ps)), force, "", "")
	if err != nil {
		return nil, store.Feature{}, err
	}
	pendingIDs := make([]string, len(ps))
	for i, p := range ps {
		pendingIDs[i] = p.ID
	}
	marked, err := markPromoted(repoDir, projDir, pendingIDs, f.ID)
	return marked, f, err
}

// appendMissing appends each value of add not yet in dst, keeping order.
func appendMissing(dst, add []string) []string {
	for _, v := range add {
		if !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}

// promotedContext is the spec context of a promotion: the Origem line linking
// back to each pending and its origin, then the body of each — under a heading
// per pending when there are several.
func promotedContext(ps []store.Pending) string {
	if len(ps) == 1 {
		context := fmt.Sprintf("**Origem:** pendência [[%s]] (de %s)", ps[0].ID, ps[0].From)
		if ps[0].Body != "" {
			context += "\n\n" + ps[0].Body
		}
		return context
	}
	links := make([]string, len(ps))
	for i, p := range ps {
		links[i] = fmt.Sprintf("[[%s]] (de %s)", p.ID, p.From)
	}
	context := "**Origem:** pendências " + strings.Join(links, ", ")
	for _, p := range ps {
		context += fmt.Sprintf("\n\n### %s — %s", p.ID, p.Title)
		if p.Body != "" {
			context += "\n\n" + p.Body
		}
	}
	return context
}

// markPromoted is the promotion's second step: each pending still open moves to
// promoted with promoted_to = created, all in one backend commit, the states
// re-read inside the lock. A pending closed meanwhile is left as it is and
// named (ErrPendingClosedConcurrently, with the ones that were marked
// returned); a backend failure names the created entity, each pending that may
// still be open and the command that links it.
func markPromoted(repoDir, projDir string, ids []string, created string) ([]store.Pending, error) {
	var marked []store.Pending
	var closed []string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		marked, closed = nil, nil
		for _, id := range ids {
			p, err := store.LoadPending(repoDir, id)
			if err != nil {
				return "", err
			}
			p, err = movePending(repoDir, p, domain.ActionPromote, created, map[string]string{"promoted_to": created})
			if errors.Is(err, domain.ErrInvalidTransition) {
				closed = append(closed, err.Error())
				continue
			}
			if err != nil {
				return "", err
			}
			marked = append(marked, p)
		}
		if len(marked) == 0 {
			return "", errNothingMarked
		}
		markedIDs := make([]string, len(marked))
		for i, p := range marked {
			markedIDs[i] = p.ID
		}
		return fmt.Sprintf("cerne: promote %s -> %s", strings.Join(markedIDs, ", "), created), nil
	})
	if err != nil && !errors.Is(err, errNothingMarked) {
		checks := make([]string, len(ids))
		for i, id := range ids {
			checks[i] = fmt.Sprintf("'cerne pending show %s' and, if it is open, 'cerne pending link %s %s'", id, id, created)
		}
		return nil, fmt.Errorf("%s was created, but recording the promotion of %s failed: %w — %s may still be open: once the backend is reachable, check %s",
			created, strings.Join(ids, ", "), err, strings.Join(ids, ", "), strings.Join(checks, "; "))
	}
	if len(closed) > 0 {
		tail := ""
		if len(marked) == 0 {
			tail = fmt.Sprintf(" — decide whether %s is still wanted", created)
		}
		return marked, fmt.Errorf("%w: %s was created, but %s was closed by someone else meanwhile, so it does not point to %s%s",
			ErrPendingClosedConcurrently, created, strings.Join(closed, ", "), created, tail)
	}
	return marked, nil
}

// ErrPendingTargetClosed refuses linking a pending to, or unlinking it from, a
// feature that is no longer live (F-0885): its lifecycle already decided the
// debt's fate.
var ErrPendingTargetClosed = errors.New("the feature is not live (candidate, approved or in_development)")

// featureLive reports whether a feature can still pay a pending.
func featureLive(s domain.FeatureState) bool {
	return s == domain.StateCandidate || s == domain.StateApproved || s == domain.StateInDevelopment
}

// LinkPending points an open pending at the live feature that pays it (F-0885):
// open -> promoted with promoted_to = featureID, the event carrying via=link.
// From then on the pending follows that feature (movePaidPendings). An
// implemented, done, rejected or superseded feature refuses with nothing
// written.
func LinkPending(repoDir, projDir, id, featureID string) (store.Pending, error) {
	id, err := canonPendingID(id)
	if err != nil {
		return store.Pending{}, err
	}
	featureID = strings.ToUpper(strings.TrimSpace(featureID))
	if !pendingOriginRx.MatchString(featureID) {
		return store.Pending{}, fmt.Errorf("%q is not an F-/T-/E-NNNN id", featureID)
	}
	var p store.Pending
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, featureID)
		if err != nil {
			return "", err
		}
		if !featureLive(f.State) {
			return "", fmt.Errorf("%w: %s is %s", ErrPendingTargetClosed, f.ID, f.State)
		}
		if p, err = store.LoadPending(repoDir, id); err != nil {
			return "", err
		}
		if p, err = movePending(repoDir, p, domain.ActionPromote, f.ID, map[string]string{"promoted_to": f.ID, "via": "link"}); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: link %s -> %s", id, f.ID), nil
	})
	if err != nil {
		return store.Pending{}, err
	}
	return p, nil
}

// UnlinkPending hands a promoted pending back to open (F-0885): promoted_to
// and closed_at cleared, pending.reopened carrying the feature. A pending that
// is not promoted refuses, and so does one whose feature is implemented: its
// accept resolves it and a reject reopens it. A feature already terminal
// (done, rejected, superseded — a promotion from before F-0885) moves its
// pendings no more, so unlink is the way out.
func UnlinkPending(repoDir, projDir, id string) (store.Pending, error) {
	id, err := canonPendingID(id)
	if err != nil {
		return store.Pending{}, err
	}
	var p store.Pending
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if p, err = store.LoadPending(repoDir, id); err != nil {
			return "", err
		}
		if _, err := domain.TransitionPending(p.State, domain.ActionReopen); err != nil {
			return "", fmt.Errorf("%s: %w", id, err)
		}
		f, err := store.LoadFeature(repoDir, p.PromotedTo)
		if err != nil {
			return "", err
		}
		if f.State == domain.StateImplemented {
			return "", fmt.Errorf("%w: %s is promoted to %s, which is implemented — %s's accept resolves it and a reject reopens it", ErrPendingLinked, id, f.ID, f.ID)
		}
		if p, err = movePending(repoDir, p, domain.ActionReopen, "", map[string]string{"feature": f.ID, "via": "unlink"}); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: unlink %s from %s", id, f.ID), nil
	})
	if err != nil {
		return store.Pending{}, err
	}
	return p, nil
}

// PendingsPaidBy lists the pendings promoted to featureID — the debts that
// feature pays and that follow its lifecycle — in ascending id order.
func PendingsPaidBy(repoDir, featureID string) ([]store.Pending, error) {
	all, err := store.LoadAllPendings(repoDir)
	if err != nil {
		return nil, err
	}
	var paid []store.Pending
	for _, p := range all {
		if p.State == domain.PendingPromoted && p.PromotedTo == featureID {
			paid = append(paid, p)
		}
	}
	return paid, nil
}

// pendingEvents names the event each pending move appends.
var pendingEvents = map[domain.Action]string{
	domain.ActionPromote:  "pending.promoted",
	domain.ActionResolve:  "pending.resolved",
	domain.ActionReopen:   "pending.reopened",
	domain.ActionTransfer: "pending.transferred",
}

// movePending applies action to p inside the caller's open backend critical
// section (it never takes the lock itself, so a feature transition can move
// the pendings it pays in its own commit): promote and transfer point it at
// target, promote and resolve stamp closed_at, reopen clears the link and
// closed_at. It saves the pending and appends the action's event with payload.
func movePending(repoDir string, p store.Pending, action domain.Action, target string, payload map[string]string) (store.Pending, error) {
	to, err := domain.TransitionPending(p.State, action)
	if err != nil {
		return p, fmt.Errorf("%s: %w", p.ID, err)
	}
	now := time.Now().UTC()
	p.State, p.UpdatedAt = to, now
	switch action {
	case domain.ActionPromote:
		p.PromotedTo, p.Reason, p.ClosedAt = target, "", &now
	case domain.ActionTransfer:
		p.PromotedTo = target
	case domain.ActionReopen:
		p.PromotedTo, p.ClosedAt = "", nil
	case domain.ActionResolve:
		p.ClosedAt = &now
	}
	if err := store.SavePending(repoDir, p); err != nil {
		return p, err
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent(p.ID, pendingEvents[action], payload, EventActor())); err != nil {
		return p, err
	}
	return p, nil
}

// errNothingMarked aborts markPromoted's mutation when every pending was closed
// meanwhile: there is nothing to commit.
var errNothingMarked = errors.New("no pending left to mark")

// ErrPendingLinked refuses closing by hand a pending that a live feature pays
// (F-0885): it resolves on that feature's accept.
var ErrPendingLinked = errors.New("the pending is paid by a feature: it resolves on that feature's accept")

// closePending applies a manual close (resolve/drop) to pending id, re-reading
// its state inside the backend lock, stamps closed_at (plus the reason when
// given) and appends pending.<state>. A pending promoted to a live or
// implemented feature is refused: it follows that feature. One promoted to a
// feature already terminal (a promotion from before F-0885) resolves by hand;
// to drop it, unlink it first.
func closePending(repoDir, projDir, id string, action domain.Action, reason string) (store.Pending, error) {
	var p store.Pending
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		p, err = store.LoadPending(repoDir, id)
		if err != nil {
			return "", err
		}
		if p.State == domain.PendingPromoted {
			f, err := store.LoadFeature(repoDir, p.PromotedTo)
			if err != nil {
				return "", err
			}
			switch {
			case featureLive(f.State):
				return "", fmt.Errorf("%w: %s is promoted to %s — accept %s, or run 'cerne pending unlink %s' first", ErrPendingLinked, id, f.ID, f.ID, id)
			case f.State == domain.StateImplemented:
				return "", fmt.Errorf("%w: %s is promoted to %s, which is implemented — %s's accept resolves it and a reject reopens it", ErrPendingLinked, id, f.ID, f.ID)
			case action != domain.ActionResolve:
				return "", fmt.Errorf("%s is promoted to %s, which is %s: run 'cerne pending unlink %s' first", id, f.ID, f.State, id)
			}
		}
		to, err := domain.TransitionPending(p.State, action)
		if err != nil {
			return "", fmt.Errorf("%s: %w", id, err)
		}
		now := time.Now().UTC()
		p.State, p.Reason, p.UpdatedAt, p.ClosedAt = to, reason, now, &now
		if err := store.SavePending(repoDir, p); err != nil {
			return "", err
		}
		payload := map[string]string{}
		if reason != "" {
			payload["reason"] = reason
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

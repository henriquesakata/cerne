package app

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/slug"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrIdeaTitleRequired is returned by CreateIdea with a blank title.
var ErrIdeaTitleRequired = errors.New("the idea title cannot be empty")

// ErrIdeaTitleMultiline is returned by CreateIdea for a title with a line break.
var ErrIdeaTitleMultiline = errors.New("the idea title must be a single line")

// ErrIdeaNotActive: drop/promote on a dropped or promoted idea; the wrap
// names the state and the feature (promoted) or the reason (dropped).
var ErrIdeaNotActive = errors.New("the idea is not active")

// ErrNotIdeaID is returned for an id that is not I-NNNN, before it reaches
// any path: an idea has no FSM to refuse another kind's "active" entity.
var ErrNotIdeaID = errors.New("not an idea id")

var ideaIDRx = regexp.MustCompile(`^I-\d{4,}$`)

// CanonicalIdeaID upper-cases id and checks it is I-NNNN (ErrNotIdeaID).
func CanonicalIdeaID(id string) (string, error) {
	c := strings.ToUpper(strings.TrimSpace(id))
	if !ideaIDRx.MatchString(c) {
		return "", fmt.Errorf("%w (I-NNNN): %q", ErrNotIdeaID, id)
	}
	return c, nil
}

// CreateIdea captures an active idea (I-, F-0312): text and optional tags,
// nothing else — no project, module, work or duplicate guard is read, so the
// capture costs nothing (crit. 1). Tags are validated before any write.
// token is the submission's idempotency key (C2, the F-0121 retry parity):
// checked inside the lock, a token already in the backend returns that idea
// and writes nothing — so retrying a capture whose push landed but whose tail
// failed never mints a twin. An empty token always mints.
func CreateIdea(repoDir, projDir, title, body string, facets, components []string, token string) (store.Idea, error) {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	if title == "" {
		return store.Idea{}, ErrIdeaTitleRequired
	}
	if strings.ContainsAny(title, "\r\n") {
		return store.Idea{}, ErrIdeaTitleMultiline
	}
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Idea{}, err
	}
	token = strings.TrimSpace(token)
	var i store.Idea
	existing := false
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		if token != "" {
			all, err := store.LoadAllIdeas(repoDir)
			if err != nil {
				return "", err
			}
			if k := slices.IndexFunc(all, func(x store.Idea) bool { return x.Token == token }); k >= 0 {
				i, existing = all[k], true
				return "", nil // a retry: writes nothing, surfaces as ErrNothingToCommit
			}
		}
		id, err := store.NextID(repoDir, "I")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		i = store.Idea{
			ID:         id,
			Title:      title,
			Body:       body,
			State:      domain.IdeaActive,
			Facets:     normFacets,
			Components: domain.NormalizeComponents(components),
			CreatedAt:  now,
			UpdatedAt:  now,
			Token:      token,
		}
		if err := store.SaveIdea(repoDir, i); err != nil {
			return "", err
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "idea.created", nil, EventActor())); err != nil {
			return "", err
		}
		return "cerne: capture " + id, nil
	})
	if existing && errors.Is(err, gitback.ErrNothingToCommit) {
		return i, nil
	}
	if err != nil {
		return store.Idea{}, err
	}
	return i, nil
}

// IdeaFilter selects ideas for ListIdeas: State "" means active and all keeps
// every state; Component is slugged; Search keeps the ideas whose title and
// body contain every word of it, in any order, case- and accent-insensitively.
// Empty fields do not filter.
type IdeaFilter struct{ State, Component, Search string }

// ErrIdeaStateInvalid is a state filter that is not an idea state nor all; its
// text lists the accepted values, for the caller to prefix with the value.
var ErrIdeaStateInvalid = errors.New("want one of " + strings.Join(ideaStateFilters, ", "))

var ideaStateFilters = []string{"active", "resolved", "dropped", "promoted", "all"}

// ValidIdeaState reports whether state is an idea state or all. Empty is not:
// ListIdeas reads it as active, the CLI refuses it.
func ValidIdeaState(state string) bool { return slices.Contains(ideaStateFilters, state) }

// ListIdeas is the one idea selection the CLI list and the ui route share
// (F-0419), newest first (ties: higher id first) and never nil (the JSON's []).
func ListIdeas(repoDir string, f IdeaFilter) ([]store.Idea, error) {
	state := f.State
	if state == "" {
		state = string(domain.IdeaActive)
	}
	if !ValidIdeaState(state) {
		return nil, fmt.Errorf("invalid idea state %q: %w", state, ErrIdeaStateInvalid)
	}
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(slug.Fold(f.Search))
	comp := slug.Make(f.Component)
	out := []store.Idea{}
	for _, i := range ideas {
		text := slug.Fold(i.Title + " " + i.Body)
		if (state == "all" || string(i.State) == state) && (comp == "" || slices.Contains(i.Components, comp)) && !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if !out[a].CreatedAt.Equal(out[b].CreatedAt) {
			return out[a].CreatedAt.After(out[b].CreatedAt)
		}
		return out[a].ID > out[b].ID
	})
	return out, nil
}

// DropIdea discards an active idea without deleting it: active -> dropped,
// the reason (optional) persisted, the state re-read inside the backend lock.
func DropIdea(repoDir, projDir, id, reason string) (store.Idea, error) {
	id, err := CanonicalIdeaID(id)
	if err != nil {
		return store.Idea{}, err
	}
	var i store.Idea
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if i, err = loadActiveIdea(repoDir, id); err != nil {
			return "", err
		}
		if err := moveIdea(repoDir, &i, domain.ActionDrop, "", reasonPayload(reason)); err != nil {
			return "", err
		}
		return "cerne: drop " + id, nil
	})
	if err != nil {
		return store.Idea{}, err
	}
	return i, nil
}

// PromoteIdea turns an active idea into a candidate feature: the first line
// of the text (80 runes max) is the title, the whole text the spec's context
// under "**Origem:** ideia [[I-NNNN]] (capturada em AAAA-MM-DD)", and the
// idea's tags are inherited. The idea then records promoted_to. Checking the
// idea, minting the feature and marking the idea are ONE mutation under the
// backend lock (C1): a racing drop either closes the idea first — the promote
// is refused, nothing created — or finds it already promoted. A non-blank
// title replaces the derived one (C9: an idea whose first line is a live
// feature's exact title). module and force go to the feature core
// (ValidateModule, the F-0147 guard).
func PromoteIdea(repoDir, projDir, id, title, module string, force bool) (store.Idea, store.Feature, error) {
	id, err := CanonicalIdeaID(id)
	if err != nil {
		return store.Idea{}, store.Feature{}, err
	}
	var i store.Idea
	var f store.Feature
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if i, err = loadActiveIdea(repoDir, id); err != nil {
			return "", err
		}
		// The context carries the body; a title cut to 80 runes keeps its full
		// text there so nothing the capture said is lost.
		text := i.Body
		if title = strings.TrimSpace(title); title == "" {
			title = truncateDraftRunes(i.Title, 80)
			if title != i.Title {
				text = strings.TrimSpace(i.Title + "\n\n" + i.Body)
			}
		}
		context := fmt.Sprintf("**Origem:** ideia [[%s]] (capturada em %s)\n\n%s", i.ID, i.CreatedAt.Format(time.DateOnly), text)
		n, err := prepareFeature("F", title, module, "", i.Facets, i.Components, spectmpl.StubWithContextIn(store.DocsLanguage(repoDir), title, context), force, "", "")
		if err != nil {
			return "", err
		}
		n.fromIdea = i.ID
		n.light = !projectGates(projDir).GauntletEnabled()
		if f, _, err = n.mint(repoDir); err != nil {
			return "", err
		}
		if err := moveIdea(repoDir, &i, domain.ActionPromote, f.ID, map[string]string{"promoted_to": f.ID}); err != nil {
			return "", err
		}
		if err := carryReferenceLinks(repoDir, i.ID, f.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: promote %s to %s (%s)", id, f.ID, f.Title), nil
	})
	if err != nil {
		return store.Idea{}, store.Feature{}, err
	}
	return i, f, nil
}

// loadActiveIdea re-reads the idea inside the lock; ErrIdeaNotActive unless
// it is still active.
func loadActiveIdea(repoDir, id string) (store.Idea, error) {
	i, err := store.LoadIdea(repoDir, id)
	if err != nil {
		return store.Idea{}, err
	}
	if i.State != domain.IdeaActive {
		return store.Idea{}, notActive(i)
	}
	return i, nil
}

// ideaEventTypes names the event each idea move appends.
var ideaEventTypes = map[domain.Action]string{
	domain.ActionDrop:     "idea.dropped",
	domain.ActionPromote:  "idea.promoted",
	domain.ActionResolve:  "idea.resolved",
	domain.ActionReopen:   "idea.reopened",
	domain.ActionTransfer: "idea.transferred",
}

// reasonPayload is the event payload of a manual close: the trimmed reason,
// when there is one.
func reasonPayload(reason string) map[string]string {
	if reason = strings.TrimSpace(reason); reason == "" {
		return map[string]string{}
	}
	return map[string]string{"reason": reason}
}

// moveIdea applies action to i inside the caller's open backend critical
// section (it never takes the lock itself, so a feature transition can move
// the ideas it carries in its own commit): promote and transfer point it at
// target; promote, resolve and drop stamp closed_at; reopen clears the link
// and closed_at; drop and resolve keep payload["reason"]. It saves the idea
// and appends the action's event with payload.
func moveIdea(repoDir string, i *store.Idea, action domain.Action, target string, payload map[string]string) error {
	to, err := domain.TransitionIdea(i.State, action)
	if err != nil {
		return fmt.Errorf("%s: %w", i.ID, err)
	}
	now := time.Now().UTC()
	i.State, i.UpdatedAt = to, now
	switch action {
	case domain.ActionPromote:
		i.PromotedTo, i.Reason, i.ClosedAt = target, "", &now
	case domain.ActionTransfer:
		i.PromotedTo = target
	case domain.ActionReopen:
		i.PromotedTo, i.ClosedAt = "", nil
	case domain.ActionDrop, domain.ActionResolve:
		i.Reason, i.ClosedAt = payload["reason"], &now
	}
	if err := store.SaveIdea(repoDir, *i); err != nil {
		return err
	}
	return store.AppendEvent(repoDir, domain.NewEvent(i.ID, ideaEventTypes[action], payload, EventActor()))
}

// notActive names the idea's state and where it went: the feature it was
// promoted to, or why it was dropped.
func notActive(i store.Idea) error {
	switch {
	case i.PromotedTo != "":
		return fmt.Errorf("%w: %s is %s (promoted to %s)", ErrIdeaNotActive, i.ID, i.State, i.PromotedTo)
	case i.Reason != "":
		return fmt.Errorf("%w: %s is %s (reason: %s)", ErrIdeaNotActive, i.ID, i.State, i.Reason)
	}
	return fmt.Errorf("%w: %s is %s", ErrIdeaNotActive, i.ID, i.State)
}

// carryReferenceLinks adds feature fID to every live reference informing the
// idea, keeping the idea (F-0389): the source stays tied to where it went.
func carryReferenceLinks(repoDir, ideaID, fID string) error {
	refs, err := ReferencesInforming(repoDir, ideaID)
	if err != nil {
		return err
	}
	for _, r := range refs {
		if r.State != domain.ReferenceCurrent || slices.Contains(r.Informs, fID) {
			continue
		}
		r.Informs = append(r.Informs, fID)
		r.UpdatedAt = time.Now().UTC()
		if err := store.SaveReference(repoDir, r); err != nil {
			return err
		}
		if err := appendLinkEvents(repoDir, r.ID, []string{fID}, nil, "promote"); err != nil {
			return err
		}
	}
	return nil
}

// legacyIdeas lists the ideas still stored in the pre-F-0419 text-only
// format, in ascending id order.
func legacyIdeas(repoDir string) ([]store.Idea, error) {
	all, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(i store.Idea) bool { return !i.Legacy() }), nil
}

// MigrateIdeas rewrites every idea stored as text only into title + body, in
// one commit with an idea.migrated event each (F-0419). It returns the ids it
// migrated — or, with dryRun, the ones it would — and nothing when none is
// left, so a second run writes nothing.
func MigrateIdeas(repoDir, projDir string, dryRun bool) ([]string, error) {
	legacy, err := legacyIdeas(repoDir)
	if err != nil || len(legacy) == 0 || dryRun {
		return ideaIDs(legacy), err
	}
	var migrated []store.Idea
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if migrated, err = legacyIdeas(repoDir); err != nil { // re-read under the lock
			return "", err
		}
		if len(migrated) == 0 {
			return "", nil
		}
		for _, i := range migrated {
			if err := store.SaveIdea(repoDir, i); err != nil {
				return "", err
			}
			if err := store.AppendEvent(repoDir, domain.NewEvent(i.ID, "idea.migrated", nil, EventActor())); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("cerne: migrate %d ideas to title and body", len(migrated)), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		err = nil // someone migrated them between the read and the lock
	}
	return ideaIDs(migrated), err
}

func ideaIDs(ideas []store.Idea) []string {
	ids := make([]string, len(ideas))
	for k, i := range ideas {
		ids[k] = i.ID
	}
	return ids
}

// ResolveIdea closes an active idea as taken care of: active -> resolved,
// closed_at stamped, the reason (optional) persisted. A promoted idea is not
// resolved by hand — it follows its feature.
func ResolveIdea(repoDir, projDir, id, reason string) (store.Idea, error) {
	id, err := CanonicalIdeaID(id)
	if err != nil {
		return store.Idea{}, err
	}
	var i store.Idea
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if i, err = loadActiveIdea(repoDir, id); err != nil {
			return "", err
		}
		if err := moveIdea(repoDir, &i, domain.ActionResolve, "", reasonPayload(reason)); err != nil {
			return "", err
		}
		return "cerne: resolve " + id, nil
	})
	if err != nil {
		return store.Idea{}, err
	}
	return i, nil
}

// ErrIdeaTargetClosed refuses linking an idea to a feature that is not live
// (candidate, approved or in_development): its lifecycle already decided the
// idea's fate.
var ErrIdeaTargetClosed = errors.New("the feature is not live (candidate, approved or in_development)")

// ErrIdeaLinked refuses unlinking an idea whose feature is implemented: the
// accept resolved it and a reject reopens it.
var ErrIdeaLinked = errors.New("the idea is carried by an implemented feature")

var ideaFeatureRx = regexp.MustCompile(`^F-\d{4,}$`)

// LinkIdea points an active idea at the live feature that carries it: active
// -> promoted with promoted_to = featureID, the event carrying via=link, and
// the feature's from_idea set when it has none. From then on the idea follows
// that feature (moveLinkedIdeas). A feature that is not live refuses with
// nothing written.
func LinkIdea(repoDir, projDir, id, featureID string) (store.Idea, error) {
	id, err := CanonicalIdeaID(id)
	if err != nil {
		return store.Idea{}, err
	}
	featureID = strings.ToUpper(strings.TrimSpace(featureID))
	if !ideaFeatureRx.MatchString(featureID) {
		return store.Idea{}, fmt.Errorf("%q is not an F-NNNN id", featureID)
	}
	var i store.Idea
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, featureID)
		if err != nil {
			return "", err
		}
		if !featureLive(f.State) {
			return "", fmt.Errorf("%w: %s is %s", ErrIdeaTargetClosed, f.ID, f.State)
		}
		if i, err = store.LoadIdea(repoDir, id); err != nil {
			return "", err
		}
		if err := moveIdea(repoDir, &i, domain.ActionPromote, f.ID, map[string]string{"promoted_to": f.ID, "via": "link"}); err != nil {
			return "", err
		}
		if f.FromIdea == "" {
			f.FromIdea = i.ID
			if err := store.SaveFeature(repoDir, f); err != nil {
				return "", err
			}
		}
		if err := carryReferenceLinks(repoDir, i.ID, f.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: link %s -> %s", id, f.ID), nil
	})
	if err != nil {
		return store.Idea{}, err
	}
	return i, nil
}

// UnlinkIdea hands a promoted idea back to active: promoted_to and closed_at
// cleared, idea.reopened carrying the feature, and the feature's from_idea
// cleared when it points at this idea. An idea that is not promoted refuses,
// and so does one whose feature is implemented: its accept resolves it and a
// reject reopens it.
func UnlinkIdea(repoDir, projDir, id string) (store.Idea, error) {
	id, err := CanonicalIdeaID(id)
	if err != nil {
		return store.Idea{}, err
	}
	var i store.Idea
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if i, err = store.LoadIdea(repoDir, id); err != nil {
			return "", err
		}
		if _, err := domain.TransitionIdea(i.State, domain.ActionReopen); err != nil {
			return "", fmt.Errorf("%s: %w", id, err)
		}
		f, err := store.LoadFeature(repoDir, i.PromotedTo)
		if err != nil {
			return "", err
		}
		if f.State == domain.StateImplemented {
			return "", fmt.Errorf("%w: %s is promoted to %s, which is implemented — %s's accept resolves it and a reject reopens it", ErrIdeaLinked, id, f.ID, f.ID)
		}
		if err := moveIdea(repoDir, &i, domain.ActionReopen, "", map[string]string{"feature": f.ID, "via": "unlink"}); err != nil {
			return "", err
		}
		if f.FromIdea == id {
			f.FromIdea = ""
			if err := store.SaveFeature(repoDir, f); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("cerne: unlink %s from %s", id, f.ID), nil
	})
	if err != nil {
		return store.Idea{}, err
	}
	return i, nil
}

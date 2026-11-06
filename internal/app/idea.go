package app

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrIdeaTextRequired is returned by CreateIdea with a blank text.
var ErrIdeaTextRequired = errors.New("the idea text cannot be empty")

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
func CreateIdea(repoDir, projDir, text string, facets, components []string, token string) (store.Idea, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return store.Idea{}, ErrIdeaTextRequired
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
			Text:       text,
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

// ListIdeas returns the ideas newest first (ties: higher id first): active
// only unless all; search keeps those whose text contains every word of it, in
// any order, case- and accent-insensitively.
func ListIdeas(repoDir string, all bool, search string) ([]store.Idea, error) {
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(slug.Fold(search))
	out := ideas[:0]
	for _, i := range ideas {
		text := slug.Fold(i.Text)
		if (all || i.State == domain.IdeaActive) && !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
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
		if err := markIdea(repoDir, &i, domain.IdeaDropped, strings.TrimSpace(reason), ""); err != nil {
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
		if title = strings.TrimSpace(title); title == "" {
			for _, l := range strings.Split(i.Text, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					title = truncateDraftRunes(l, 80)
					break
				}
			}
		}
		context := fmt.Sprintf("**Origem:** ideia [[%s]] (capturada em %s)\n\n%s", i.ID, i.CreatedAt.Format(time.DateOnly), i.Text)
		n, err := prepareFeature("F", title, module, "", i.Facets, i.Components, spectmpl.StubWithContext(title, context), force, "", "")
		if err != nil {
			return "", err
		}
		n.fromIdea = i.ID
		if f, _, err = n.mint(repoDir); err != nil {
			return "", err
		}
		if err := markIdea(repoDir, &i, domain.IdeaPromoted, "", f.ID); err != nil {
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

// markIdea stamps the closing state on i (closed_at, plus reason /
// promoted_to when given), saves it and appends idea.<to>.
func markIdea(repoDir string, i *store.Idea, to domain.IdeaState, reason, promotedTo string) error {
	now := time.Now().UTC()
	i.State, i.Reason, i.PromotedTo, i.UpdatedAt, i.ClosedAt = to, reason, promotedTo, now, &now
	if err := store.SaveIdea(repoDir, *i); err != nil {
		return err
	}
	payload := map[string]string{}
	if reason != "" {
		payload["reason"] = reason
	}
	if promotedTo != "" {
		payload["promoted_to"] = promotedTo
	}
	return store.AppendEvent(repoDir, domain.NewEvent(i.ID, "idea."+string(to), payload, EventActor()))
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

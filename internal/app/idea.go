package app

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
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
func CreateIdea(repoDir, projDir, text string, facets, components []string) (store.Idea, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return store.Idea{}, ErrIdeaTextRequired
	}
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Idea{}, err
	}
	var i store.Idea
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
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
		}
		if err := store.SaveIdea(repoDir, i); err != nil {
			return "", err
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "idea.created", nil, EventActor())); err != nil {
			return "", err
		}
		return "cerne: capture " + id, nil
	})
	if err != nil {
		return store.Idea{}, err
	}
	return i, nil
}

// ListIdeas returns the ideas newest first (ties: higher id first): active
// only unless all; search keeps those whose text contains the term, case- and
// accent-insensitively.
func ListIdeas(repoDir string, all bool, search string) ([]store.Idea, error) {
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	term := slug.Fold(strings.TrimSpace(search))
	out := ideas[:0]
	for _, i := range ideas {
		if (all || i.State == domain.IdeaActive) && strings.Contains(slug.Fold(i.Text), term) {
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
// the reason (optional) persisted.
func DropIdea(repoDir, projDir, id, reason string) (store.Idea, error) {
	id, err := CanonicalIdeaID(id)
	if err != nil {
		return store.Idea{}, err
	}
	return closeIdea(repoDir, projDir, id, domain.IdeaDropped, strings.TrimSpace(reason), "")
}

// PromoteIdea turns an active idea into a candidate feature: the first line
// of the text (80 runes max) is the title, the whole text the spec's context
// under "**Origem:** ideia [[I-NNNN]] (capturada em AAAA-MM-DD)" — the link
// back — and the idea's tags are inherited. The idea then records promoted_to.
// A closed idea is refused before any feature exists. module and force go to
// the feature core (ValidateModule, the F-0147 guard).
//
// ponytail: two mutations, not one transaction — the same ceiling as
// PromotePending. If the idea closes in between, the feature exists without
// the idea pointing to it; the error names both.
func PromoteIdea(repoDir, projDir, id, module string, force bool) (store.Idea, store.Feature, error) {
	id, err := CanonicalIdeaID(id)
	if err != nil {
		return store.Idea{}, store.Feature{}, err
	}
	i, err := store.LoadIdea(repoDir, id)
	if err != nil {
		return store.Idea{}, store.Feature{}, err
	}
	if i.State != domain.IdeaActive {
		return store.Idea{}, store.Feature{}, notActive(i)
	}
	title := ""
	for _, l := range strings.Split(i.Text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			title = truncateDraftRunes(l, 80)
			break
		}
	}
	context := fmt.Sprintf("**Origem:** ideia [[%s]] (capturada em %s)\n\n%s", i.ID, i.CreatedAt.Format(time.DateOnly), i.Text)
	f, err := CreateFeatureWithSpec(repoDir, projDir, title, module, "", i.Facets, i.Components, spectmpl.StubWithContext(title, context), force, "", "")
	if err != nil {
		return store.Idea{}, store.Feature{}, err
	}
	i, err = closeIdea(repoDir, projDir, id, domain.IdeaPromoted, "", f.ID)
	switch {
	case errors.Is(err, ErrIdeaNotActive):
		return store.Idea{}, f, fmt.Errorf("%s was created, but %w meanwhile, so it does not point to %s — decide whether %s is still wanted", f.ID, err, f.ID, f.ID)
	case err != nil:
		return store.Idea{}, f, fmt.Errorf("%s was created, but recording the promotion on %s failed: %w — check 'cerne idea show %s' and, if it is still active, drop it with --reason \"promoted to %s\"", f.ID, id, err, id, f.ID)
	}
	return i, f, nil
}

// closeIdea moves an active idea to `to`, re-reading it inside the backend
// lock (only an active idea changes), stamps closed_at (plus reason /
// promoted_to when given) and appends idea.<to>.
func closeIdea(repoDir, projDir, id string, to domain.IdeaState, reason, promotedTo string) (store.Idea, error) {
	var i store.Idea
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		i, err = store.LoadIdea(repoDir, id)
		if err != nil {
			return "", err
		}
		if i.State != domain.IdeaActive {
			return "", notActive(i)
		}
		now := time.Now().UTC()
		i.State, i.Reason, i.PromotedTo, i.UpdatedAt, i.ClosedAt = to, reason, promotedTo, now, &now
		if err := store.SaveIdea(repoDir, i); err != nil {
			return "", err
		}
		payload := map[string]string{}
		if reason != "" {
			payload["reason"] = reason
		}
		if promotedTo != "" {
			payload["promoted_to"] = promotedTo
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "idea."+string(to), payload, EventActor())); err != nil {
			return "", err
		}
		verb := "drop"
		if to == domain.IdeaPromoted {
			verb = "promote"
		}
		return fmt.Sprintf("cerne: %s %s", verb, id), nil
	})
	if err != nil {
		return store.Idea{}, err
	}
	return i, nil
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

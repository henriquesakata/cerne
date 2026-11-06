package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrIdeaTextRequired is returned by CreateIdea with a blank text.
var ErrIdeaTextRequired = errors.New("the idea text cannot be empty")

// ErrIdeaNotActive: drop/promote on a dropped or promoted idea; the wrap
// names the state and the feature (promoted) or the reason (dropped).
var ErrIdeaNotActive = errors.New("the idea is not active")

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
	return closeIdea(repoDir, projDir, id, domain.IdeaDropped, strings.TrimSpace(reason), "")
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

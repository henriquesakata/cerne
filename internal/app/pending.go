package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrPendingFromRequired is returned by CreatePending without an origin: a
// pending is always a debt of a concrete delivery (F-0322), which is what
// keeps it apart from a speculative idea.
var ErrPendingFromRequired = errors.New("--from is required (the F-/T-/E- id the pending was left by)")

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
	from = strings.TrimSpace(from)
	if from == "" {
		return store.Pending{}, ErrPendingFromRequired
	}
	normFacets, err := domain.NormalizeFacets(facets)
	if err != nil {
		return store.Pending{}, err
	}
	normComponents := domain.NormalizeComponents(components)
	var p store.Pending
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		origin, err := store.LoadFeature(repoDir, from)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("origin %s: %w", from, err)
		}
		if err != nil {
			return "", err
		}
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

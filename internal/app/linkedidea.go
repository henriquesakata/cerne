package app

import (
	"fmt"
	"maps"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// IdeasCarriedBy lists the ideas promoted or linked to featureID — the ones
// that follow its lifecycle — in ascending id order.
func IdeasCarriedBy(repoDir, featureID string) ([]store.Idea, error) {
	all, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	var carried []store.Idea
	for _, i := range all {
		if i.State == domain.IdeaPromoted && i.PromotedTo == featureID {
			carried = append(carried, i)
		}
	}
	return carried, nil
}

// moveLinkedIdeas is movePaidPendings for ideas (F-0419): the ideas promoted
// or linked to f follow the transition f just went through, inside its
// critical section and commit. One line per idea moved.
func moveLinkedIdeas(repoDir string, f store.Feature, reason string) ([]string, error) {
	action, target, payload, ok, err := followFeature(repoDir, f, reason)
	if err != nil || !ok {
		return nil, err
	}
	carried, err := IdeasCarriedBy(repoDir, f.ID)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, i := range carried {
		from := i.State
		if err := moveIdea(repoDir, &i, action, target, maps.Clone(payload)); err != nil {
			return nil, err
		}
		line := fmt.Sprintf("%s: %s -> %s", i.ID, from, i.State)
		if target != "" {
			line += " (" + target + ")"
		}
		lines = append(lines, line)
	}
	return lines, nil
}

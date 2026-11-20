package app

import "github.com/henriquesakata/cerne/internal/domain"

// FirstStep is one first step of a new project and whether its event exists.
type FirstStep struct {
	Key  string
	Done bool
}

// FirstSteps derives the first-use checklist (E-0004) from the event log:
// nothing is stored for it. Empty once any feature was accepted. Keys: ask
// (feature.created), approve (feature.approved), accept (feature.accepted);
// *.imported events mark nothing.
func FirstSteps(events []domain.Event) []FirstStep {
	var created, approved bool
	for _, ev := range events {
		switch ev.Type {
		case "feature.created":
			created = true
		case "feature.approved":
			approved = true
		case "feature.accepted":
			return nil
		}
	}
	return []FirstStep{{"ask", created}, {"approve", approved}, {"accept", false}}
}

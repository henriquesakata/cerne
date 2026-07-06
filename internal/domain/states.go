package domain

// ValidStates returns the lifecycle states an entity of kind can hold, in
// lifecycle order, and whether kind is a known entity kind. The agent-assisted
// import (F-0061) proposes a state per item; the domain is the authority on
// which states are legal, so the importer validates against this list rather
// than maintaining its own copy that could drift from the FSMs.
func ValidStates(kind string) ([]string, bool) {
	switch kind {
	case "feature":
		return []string{
			string(StateCandidate), string(StateApproved), string(StateInDevelopment),
			string(StateImplemented), string(StateDone), string(StateRejected), string(StateSuperseded),
		}, true
	case "decision":
		return []string{string(DecisionActive), string(DecisionSuperseded)}, true
	case "reference":
		return []string{string(ReferenceCurrent), string(ReferenceObsolete)}, true
	case "runbook":
		return []string{string(RunbookActive), string(RunbookSuperseded)}, true
	case "note":
		return []string{string(NoteActive), string(NoteSuperseded)}, true
	case "work":
		return []string{string(WorkActive), string(WorkDone), string(WorkAbandoned)}, true
	}
	return nil, false
}

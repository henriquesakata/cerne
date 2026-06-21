package domain

import "fmt"

type NoteState string

const (
	NoteActive     NoteState = "active"
	NoteSuperseded NoteState = "superseded"
)

var noteTransitions = map[NoteState]map[Action]NoteState{
	NoteActive: {
		ActionSupersede: NoteSuperseded,
	},
	// Terminal state: explicitly present with no actions, so it reports
	// ErrInvalidTransition rather than ErrUnknownState.
	NoteSuperseded: {},
}

// TransitionNote returns the state reached by applying action to from.
func TransitionNote(from NoteState, action Action) (NoteState, error) {
	actions, ok := noteTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}

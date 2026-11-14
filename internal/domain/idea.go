package domain

import "fmt"

// IdeaState is the state of an idea (I-, F-0312): raw material outside the
// demand funnel. F-0419: it follows the pending lifecycle — a promoted or
// linked idea follows the feature that carries it.
type IdeaState string

const (
	IdeaActive   IdeaState = "active"
	IdeaResolved IdeaState = "resolved"
	IdeaDropped  IdeaState = "dropped"
	IdeaPromoted IdeaState = "promoted"
)

// IdeaEventPrefix is the type prefix of every idea event (idea.created,
// idea.dropped, idea.promoted); store.ReadEvents leaves them out of the funnel.
const IdeaEventPrefix = "idea."

var ideaTransitions = map[IdeaState]map[Action]IdeaState{
	IdeaActive: {
		ActionResolve: IdeaResolved,
		ActionDrop:    IdeaDropped,
		ActionPromote: IdeaPromoted,
	},
	IdeaPromoted: {
		ActionResolve:  IdeaResolved,
		ActionReopen:   IdeaActive,
		ActionTransfer: IdeaPromoted,
	},
	IdeaResolved: {},
	IdeaDropped:  {},
}

// TransitionIdea returns the state reached by applying action to from.
func TransitionIdea(from IdeaState, action Action) (IdeaState, error) {
	actions, ok := ideaTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}

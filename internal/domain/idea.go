package domain

// IdeaState is the state of an idea (I-, F-0312): raw material outside the
// demand funnel. No FSM: only an active idea changes, checked in the app.
type IdeaState string

const (
	IdeaActive   IdeaState = "active"
	IdeaDropped  IdeaState = "dropped"
	IdeaPromoted IdeaState = "promoted"
)

// IdeaEventPrefix is the type prefix of every idea event (idea.created,
// idea.dropped, idea.promoted); store.ReadEvents leaves them out of the funnel.
const IdeaEventPrefix = "idea."

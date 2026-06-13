package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// DecisionOutcome reports a decision transition that was applied and synced.
type DecisionOutcome struct {
	ID   string
	From domain.DecisionState
	To   domain.DecisionState
}

// CreateDecision mints a new active decision in the backend clone at repoDir:
// allocates the next D- id, persists the entity, writes a stub ADR document,
// appends the decision.created event, commits+pushes the backend and refreshes
// the docs.lock snapshot in projDir.
func CreateDecision(repoDir, projDir, title string) (store.Decision, error) {
	id, err := store.NextID(repoDir, "D")
	if err != nil {
		return store.Decision{}, err
	}
	now := time.Now().UTC()
	d := store.Decision{
		ID:        id,
		Title:     title,
		State:     domain.DecisionActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveDecision(repoDir, d); err != nil {
		return store.Decision{}, err
	}
	if err := writeDecisionStub(repoDir, id, title); err != nil {
		return store.Decision{}, err
	}
	ev := domain.NewEvent(id, "decision.created", map[string]string{"title": title}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return store.Decision{}, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: create %s (%s)", id, title)); err != nil {
		return store.Decision{}, err
	}
	return d, nil
}

// TransitionDecision supersedes decision id by decision `by` in the backend
// clone at repoDir: validates the flags, loads both decisions (errors when one
// is missing, when by is the decision itself, or when id is already terminal),
// applies the domain transition, persists state+superseded_by+reason, appends
// the decision.superseded event and syncs the backend + docs.lock in projDir.
func TransitionDecision(repoDir, projDir, id, by, reason string) (DecisionOutcome, error) {
	var out DecisionOutcome
	if err := ValidateTransitionFlags(domain.ActionSupersede, by, reason); err != nil {
		return out, err
	}
	d, err := store.LoadDecision(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return out, fmt.Errorf("decisão %s não encontrada no backend", id)
	}
	if err != nil {
		return out, err
	}
	if by == id {
		return out, fmt.Errorf("--by não pode ser a própria decisão (%s)", id)
	}
	if _, err := store.LoadDecision(repoDir, by); errors.Is(err, store.ErrNotFound) {
		return out, fmt.Errorf("decisão %s (--by) não encontrada no backend", by)
	} else if err != nil {
		return out, err
	}

	newState, err := domain.TransitionDecision(d.State, domain.ActionSupersede)
	if err != nil {
		return out, err
	}
	oldState := d.State
	d.State = newState
	d.SupersededBy = by
	d.Reason = reason
	d.UpdatedAt = time.Now().UTC()
	if err := store.SaveDecision(repoDir, d); err != nil {
		return out, err
	}
	ev := domain.NewEvent(id, "decision.superseded", map[string]string{"by": by, "reason": reason}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return out, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: supersede %s", id)); err != nil {
		return out, err
	}
	return DecisionOutcome{ID: id, From: oldState, To: newState}, nil
}

// DecisionDocPath returns the backend path of a decision's content document.
func DecisionDocPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "decisions", id+".md")
}

// writeDecisionStub creates content/decisions/<id>.md with the placeholder ADR
// body the author replaces with the real decision record.
func writeDecisionStub(repoDir, id, title string) error {
	path := DecisionDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("---\ncerne_id: %s\n---\n\n# %s\n\n> Decisão pendente — descreva contexto, decisão e consequências.\n", id, title)
	return os.WriteFile(path, []byte(content), 0o644)
}

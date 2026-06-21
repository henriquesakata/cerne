package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/runtmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// RunbookOutcome reports a runbook transition that was applied and synced.
type RunbookOutcome struct {
	ID   string
	From domain.RunbookState
	To   domain.RunbookState
}

// CreateRunbook mints a new active runbook in the backend clone at repoDir:
// allocates the next RB- id, persists the entity, scaffolds a stub runbook
// document from the canonical template, appends the runbook.created event,
// commits+pushes the backend and refreshes the docs.lock snapshot in projDir.
func CreateRunbook(repoDir, projDir, title string) (store.Runbook, error) {
	id, err := store.NextID(repoDir, "RB")
	if err != nil {
		return store.Runbook{}, err
	}
	now := time.Now().UTC()
	rb := store.Runbook{
		ID:        id,
		Title:     title,
		State:     domain.RunbookActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveRunbook(repoDir, rb); err != nil {
		return store.Runbook{}, err
	}
	if err := writeRunbookStub(repoDir, id, title); err != nil {
		return store.Runbook{}, err
	}
	ev := domain.NewEvent(id, "runbook.created", map[string]string{"title": title}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return store.Runbook{}, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: create %s (%s)", id, title)); err != nil {
		return store.Runbook{}, err
	}
	return rb, nil
}

// TransitionRunbook supersedes runbook id by runbook `by` in the backend clone at
// repoDir: validates the flags, loads both runbooks (errors when one is missing,
// when by is the runbook itself, or when id is already terminal), applies the
// domain transition, persists state+superseded_by+reason, appends the
// runbook.superseded event and syncs the backend + docs.lock in projDir.
func TransitionRunbook(repoDir, projDir, id, by, reason string) (RunbookOutcome, error) {
	var out RunbookOutcome
	if err := ValidateTransitionFlags(domain.ActionSupersede, by, reason); err != nil {
		return out, err
	}
	rb, err := store.LoadRunbook(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return out, fmt.Errorf("runbook %s não encontrado no backend", id)
	}
	if err != nil {
		return out, err
	}
	if by == id {
		return out, fmt.Errorf("--by não pode ser o próprio runbook (%s)", id)
	}
	if _, err := store.LoadRunbook(repoDir, by); errors.Is(err, store.ErrNotFound) {
		return out, fmt.Errorf("runbook %s (--by) não encontrado no backend", by)
	} else if err != nil {
		return out, err
	}

	newState, err := domain.TransitionRunbook(rb.State, domain.ActionSupersede)
	if err != nil {
		return out, err
	}
	oldState := rb.State
	rb.State = newState
	rb.SupersededBy = by
	rb.Reason = reason
	rb.UpdatedAt = time.Now().UTC()
	if err := store.SaveRunbook(repoDir, rb); err != nil {
		return out, err
	}
	ev := domain.NewEvent(id, "runbook.superseded", map[string]string{"by": by, "reason": reason}, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return out, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: supersede %s", id)); err != nil {
		return out, err
	}
	return RunbookOutcome{ID: id, From: oldState, To: newState}, nil
}

// RunbookDocPath returns the backend path of a runbook's content document.
func RunbookDocPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "runbooks", id+".md")
}

// writeRunbookStub creates content/runbooks/<id>.md scaffolded from the canonical
// runbook template (runtmpl): the section skeleton with inline guidance. doccheck
// treats that scaffold as a stub until the author fills a section.
func writeRunbookStub(repoDir, id, title string) error {
	path := RunbookDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + runtmpl.Stub(title)
	return os.WriteFile(path, []byte(content), 0o644)
}

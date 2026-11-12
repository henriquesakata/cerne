package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/runtmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// RunbookOutcome reports a runbook transition that was applied and synced.
type RunbookOutcome struct {
	ID   string
	From domain.RunbookState
	To   domain.RunbookState
}

// CreateRunbook mints a new active runbook in the backend clone at repoDir:
// allocates the next RB- id, persists the entity, writes the runbook document
// (from body when given, else a stub scaffolded from the canonical template),
// appends the runbook.created event, commits+pushes the backend and refreshes
// the docs.lock snapshot in projDir. A non-empty body makes 'runbook new'
// one-shot: the runbook is born with real content, no draft+push needed.
func CreateRunbook(repoDir, projDir, title, body string) (store.Runbook, error) {
	// Id mint and writes run inside the backend lock's critical section
	// (F-0122): concurrent creations serialize instead of minting the same id.
	var rb store.Runbook
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		id, err := store.NextID(repoDir, "RB")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		rb = store.Runbook{
			ID:        id,
			Title:     title,
			State:     domain.RunbookActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.SaveRunbook(repoDir, rb); err != nil {
			return "", err
		}
		if err := writeRunbookBody(repoDir, id, title, body); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "runbook.created", map[string]string{"title": title}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: create %s (%s)", id, title), nil
	})
	if err != nil {
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
	// `by` is OPTIONAL (mirrors TransitionNote): with it, `by` replaces id;
	// without it, id is merely retired (still moved to superseded, no
	// successor). The substituir CLI still requires --by via
	// ValidateTransitionFlags; the retire CLI passes an empty by.
	if strings.TrimSpace(reason) == "" {
		return out, fmt.Errorf("--reason is required")
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		rb, err := store.LoadRunbook(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("runbook %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if by != "" {
			if by == id {
				return "", fmt.Errorf("--by cannot be the runbook itself (%s)", id)
			}
			if _, err := store.LoadRunbook(repoDir, by); errors.Is(err, store.ErrNotFound) {
				return "", fmt.Errorf("runbook %s (--by) not found in backend", by)
			} else if err != nil {
				return "", err
			}
		}

		newState, err := domain.TransitionRunbook(rb.State, domain.ActionSupersede)
		if err != nil {
			return "", err
		}
		oldState := rb.State
		rb.State = newState
		rb.SupersededBy = by
		rb.Reason = reason
		rb.UpdatedAt = time.Now().UTC()
		if err := store.SaveRunbook(repoDir, rb); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "runbook.superseded", map[string]string{"by": by, "reason": reason}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		out = RunbookOutcome{ID: id, From: oldState, To: newState}
		return fmt.Sprintf("cerne: supersede %s", id), nil
	})
	if err != nil {
		return RunbookOutcome{}, err
	}
	return out, nil
}

// RetagRunbook sets the tag axes of an active runbook in the backend clone at
// repoDir. Each axis is replaced only when its set flag is true, so a caller can
// update facets without touching components (or clear an axis by passing an empty
// slice with set=true); state, superseded_by, reason and the content doc are
// preserved. Facets are validated against the closed set before any write, so an
// unknown facet aborts leaving the runbook untouched. Appends runbook.retagged and
// syncs the backend + docs.lock in projDir.
func RetagRunbook(repoDir, projDir, id string, facets, components []string, setFacets, setComponents bool) (store.Runbook, error) {
	var rb store.Runbook
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		rb, err = store.LoadRunbook(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("runbook %s not found in backend", id)
		}
		if err != nil {
			return "", err
		}
		if rb.State != domain.RunbookActive {
			return "", fmt.Errorf("runbook %s is not active (it is %s) — retag only applies to an active runbook", id, rb.State)
		}
		if setFacets {
			nf, err := domain.NormalizeFacets(facets)
			if err != nil {
				return "", err // unknown facet: runbook untouched
			}
			rb.Facets = nf
		}
		if setComponents {
			rb.Components = domain.NormalizeComponents(components)
		}
		rb.UpdatedAt = time.Now().UTC()
		if err := store.SaveRunbook(repoDir, rb); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "runbook.retagged", map[string]string{
			"facets":     strings.Join(rb.Facets, ","),
			"components": strings.Join(rb.Components, ","),
		}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: retag %s", id), nil
	})
	if err != nil {
		return store.Runbook{}, err
	}
	return rb, nil
}

// RunbookDocPath returns the backend path of a runbook's content document.
func RunbookDocPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "runbooks", id+".md")
}

// writeRunbookBody writes content/runbooks/<id>.md as cerne's stamped cerne_id
// frontmatter followed by body. An empty body falls back to writeRunbookStub (the
// canonical template scaffold). cerne stamps the frontmatter itself — the body
// supplied for a one-shot runbook never carries cerne_id.
func writeRunbookBody(repoDir, id, title, body string) error {
	if strings.TrimSpace(body) == "" {
		return writeRunbookStub(repoDir, id, title)
	}
	path := RunbookDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// writeRunbookStub creates content/runbooks/<id>.md scaffolded from the canonical
// runbook template (runtmpl): the section skeleton with inline guidance. doccheck
// treats that scaffold as a stub until the author fills a section.
func writeRunbookStub(repoDir, id, title string) error {
	path := RunbookDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + runtmpl.StubIn(store.DocsLanguage(repoDir), title)
	return os.WriteFile(path, []byte(content), 0o644)
}

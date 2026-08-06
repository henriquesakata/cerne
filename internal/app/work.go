package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// WorkOutcome reports a work transition that was applied and synced.
type WorkOutcome struct {
	ID   string
	From domain.WorkState
	To   domain.WorkState
}

// StartWork mints a new active work (agent activity, F-0130) in the backend
// clone at repoDir: validates each ref (F-/D- id) exists, allocates the next
// W- id, persists the entity, appends the work.started event and commits+
// pushes the backend, refreshing the docs.lock snapshot in projDir. actor is
// agent when non-empty, else "<os user>@<hostname>" — the default identity of
// whoever/whatever is running this activity.
func StartWork(repoDir, projDir, title string, refs []string, agent string, force bool, paths []string) (store.Work, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return store.Work{}, fmt.Errorf("the activity description cannot be empty")
	}
	actor := strings.TrimSpace(agent)
	if actor == "" {
		actor = defaultWorkActor()
	}
	paths = cleanWorkPaths(paths)
	var w store.Work
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		cleaned, err := validateWorkRefs(repoDir, refs)
		if err != nil {
			return "", err
		}
		// Duplicate-registration guard (F-0147): someone may already be on
		// this. The auto-registered works (F-0146) never pass through here —
		// they are idempotent by construction.
		forced, err := guardRegistration(repoDir, title, force)
		if err != nil {
			return "", err
		}
		id, err := store.NextID(repoDir, "W")
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		w = store.Work{
			ID:        id,
			Title:     title,
			State:     domain.WorkActive,
			Actor:     actor,
			Refs:      cleaned,
			Paths:     paths,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.SaveWork(repoDir, w); err != nil {
			return "", err
		}
		payload := map[string]string{"title": title, "actor": actor}
		if forced {
			payload["guard"] = "forced"
		}
		ev := domain.NewEvent(id, "work.started", payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: start %s (%s)", id, title), nil
	})
	if err != nil {
		return store.Work{}, err
	}
	dispatchWebhook(projDir, w.ID, "work", "start", "", string(w.State), w.Title)
	return w, nil
}

// FinishWork ends work id in the backend clone at repoDir: active -> done
// (note optional, the outcome of the activity) when abandon is false, or
// active -> abandoned (reason required) when abandon is true. A terminal work
// refuses with domain.ErrInvalidTransition — the record stays as history.
// Appends the matching work.done/work.abandoned event and commits+pushes,
// refreshing the docs.lock snapshot in projDir.
func FinishWork(repoDir, projDir, id, note string, abandon bool, reason string) (WorkOutcome, error) {
	if abandon && strings.TrimSpace(reason) == "" {
		return WorkOutcome{}, fmt.Errorf("--reason is required to abandon")
	}
	var out WorkOutcome
	var title, actionName string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		w, err := store.LoadWork(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("activity %s not found in the backend", id)
		}
		if err != nil {
			return "", err
		}

		action := domain.ActionComplete
		closingNote := strings.TrimSpace(note)
		eventType := "work.done"
		if abandon {
			action = domain.ActionAbandon
			closingNote = strings.TrimSpace(reason)
			eventType = "work.abandoned"
		}

		newState, err := domain.TransitionWork(w.State, action)
		if err != nil {
			return "", err
		}
		oldState := w.State
		w.State = newState
		w.Note = closingNote
		w.UpdatedAt = time.Now().UTC()
		if err := store.SaveWork(repoDir, w); err != nil {
			return "", err
		}
		payload := map[string]string{}
		if abandon {
			payload["reason"] = closingNote
		} else if closingNote != "" {
			payload["note"] = closingNote
		}
		ev := domain.NewEvent(id, eventType, payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		title = w.Title
		actionName = string(action)
		out = WorkOutcome{ID: id, From: oldState, To: newState}
		return fmt.Sprintf("cerne: finish %s", id), nil
	})
	if err != nil {
		return WorkOutcome{}, err
	}
	dispatchWebhook(projDir, id, "work", actionName, string(out.From), string(out.To), title)
	return out, nil
}

// defaultWorkActor returns "<user>@<hostname>" for the current process, best
// effort: an empty/unavailable piece is simply omitted rather than failing
// StartWork over an identity nicety.
func defaultWorkActor() string {
	host, _ := os.Hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case user != "":
		return user
	case host != "":
		return host
	default:
		return "unknown"
	}
}

// cleanWorkPaths trims and slash-normalizes the declared path globs (F-0151),
// dropping empties; nil when nothing survives.
func cleanWorkPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// validateWorkRefs trims, dedupes (preserving order) and checks each ref
// target: it must be a feature (F-) or decision (D-) that exists in the
// backend, so no dangling ref is stored. Returns the cleaned list (nil when
// empty).
func validateWorkRefs(repoDir string, refs []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range refs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		switch {
		case domain.IsFeatureID(id):
			if _, err := store.LoadFeature(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--ref: feature %s not found in the backend", id)
			} else if err != nil {
				return nil, err
			}
		case strings.HasPrefix(id, "D-"):
			if _, err := store.LoadDecision(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--ref: decision %s not found in the backend", id)
			} else if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("--ref: %q must be a feature (F-) or decision (D-)", id)
		}
		out = append(out, id)
	}
	return out, nil
}

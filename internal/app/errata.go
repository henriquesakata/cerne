package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrEmptyErrata is returned when an errata has no text.
var ErrEmptyErrata = errors.New("errata has no text")

// ErrUnsupportedErrataKind is returned for an id whose prefix has no
// errata-bearing entity type.
var ErrUnsupportedErrataKind = errors.New("entity type does not support errata")

// AddErrata appends an additive errata entry to an entity (typically a concluded,
// immutable one), after validating the entity exists. It records an event and
// commits+pushes. It never transitions the entity nor rewrites its body —
// correction is by annotation, so history stays immutable (supersede, don't
// rewrite). Returns the id on success.
func AddErrata(repoDir, projDir, id, text, author string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrEmptyErrata
	}
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		if err := ensureErrataEntityExists(repoDir, id); err != nil {
			return "", err
		}
		if err := store.AppendErrata(repoDir, id, text, author, time.Now().UTC()); err != nil {
			return "", err
		}
		// The author IS the event actor: a human errata records the human actor,
		// an automatic one (drift-auto, F-0115) is distinguishable in the log.
		ev := domain.NewEvent(id, "errata.added", map[string]string{}, author)
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: errata %s", id), nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// ensureErrataEntityExists validates the target entity exists, dispatching by id
// prefix. Errata is supported for feature/decision/runbook/note.
func ensureErrataEntityExists(repoDir, id string) error {
	switch {
	case strings.HasPrefix(id, "F-"):
		_, err := store.LoadFeature(repoDir, id)
		return err
	case strings.HasPrefix(id, "D-"):
		_, err := store.LoadDecision(repoDir, id)
		return err
	case strings.HasPrefix(id, "RB-"):
		_, err := store.LoadRunbook(repoDir, id)
		return err
	case strings.HasPrefix(id, "N-"):
		_, err := store.LoadNote(repoDir, id)
		return err
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedErrataKind, id)
	}
}

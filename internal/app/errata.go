package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
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
	if _, err := AddErrataSteps(repoDir, projDir, id, text, author, nil, false); err != nil {
		return "", err
	}
	return id, nil
}

// NoStepFlag is the CLI flag of a runbook erratum that changes no step. A name,
// not a literal, so error text can cite it without tripping the English guard.
const NoStepFlag = "--sem-passo"

// AddErrataSteps is AddErrata with the steps the erratum changes declared
// (F-0872, F-0345): each step is written as the `Altera:` line of its own item,
// so the consolidated view can place it next to that step. A runbook's steps
// must exist in its live body (store.StepExists); with several steps the call
// writes one item per step, all with the same text and a `Parte de:` line naming
// the siblings, in a single backend write. stepless (runbook only) writes the
// item with no step on purpose (`Sem passo: sim`). Returns the item ids written.
func AddErrataSteps(repoDir, projDir, id, text, author string, steps []string, stepless bool) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyErrata
	}
	isRB := strings.HasPrefix(id, "RB-")
	if stepless && (!isRB || len(steps) > 0) {
		return nil, fmt.Errorf("%s only applies to a runbook erratum that declares no --altera", NoStepFlag)
	}
	var uniq []string // a repeated step is one item, order kept
	for _, st := range steps {
		st = strings.TrimSpace(st)
		if st == "" || strings.ContainsAny(st, "\r\n") {
			return nil, fmt.Errorf("--altera takes step references (e.g. S6.6), got %q", st)
		}
		if st == store.NoStep {
			return nil, fmt.Errorf("--altera %q is the no-step marker, not a step: name a step, or use %s", st, NoStepFlag)
		}
		if !slices.Contains(uniq, st) {
			uniq = append(uniq, st)
		}
	}
	steps = uniq
	var ids []string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		ids = nil
		if err := ensureErrataEntityExists(repoDir, id); err != nil {
			return "", err
		}
		doc, err := store.LoadErrata(repoDir, id)
		if err != nil {
			return "", err
		}
		next := len(store.ParseErrataItems(doc))
		if isRB && len(steps) > 0 {
			body, err := store.LoadRunbookContent(repoDir, id)
			if err != nil {
				return "", err
			}
			for _, st := range steps {
				if !store.StepExists(body, st) {
					return "", fmt.Errorf("step %s not found in the body of %s: --altera takes a step the runbook names", st, id)
				}
			}
		}
		targets := steps
		if len(targets) == 0 {
			targets = []string{""}
		}
		for i := range targets {
			ids = append(ids, fmt.Sprintf("E%d", next+i+1))
		}
		for i, target := range targets {
			stored := text
			switch {
			case stepless:
				stored = "Altera: " + store.NoStep + "\nSem passo: sim\n\n" + text
			case isRB && target == "": // the block cerne owns: free text after the blank line is never metadata
				stored = "Altera: " + store.NoStep + "\n\n" + text
			case target != "":
				stored = "Altera: " + target + "\n\n" + text
			}
			if len(targets) > 1 {
				var sib []string
				for j, o := range ids {
					if j != i {
						sib = append(sib, o)
					}
				}
				stored += "\n\nParte de: " + strings.Join(sib, ", ")
			}
			if err := store.AppendErrata(repoDir, id, stored, author, time.Now().UTC()); err != nil {
				return "", err
			}
			// The author IS the event actor: a human errata records the human actor,
			// an automatic one (drift-auto, F-0115) is distinguishable in the log.
			// The excerpt lets notification surfaces say WHAT changed (F-0163).
			ev := domain.NewEvent(id, "errata.added", map[string]string{"excerpt": errataExcerpt(text)}, author)
			if err := store.AppendEvent(repoDir, ev); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("cerne: errata %s", id), nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// errataExcerptMax caps the event excerpt at one dropdown-friendly line.
const errataExcerptMax = 120

// errataExcerpt reduces an errata text to its first line, truncated at 120
// runes with an ellipsis — the notification-sized summary of the correction.
func errataExcerpt(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	runes := []rune(line)
	if len(runes) <= errataExcerptMax {
		return line
	}
	return string(runes[:errataExcerptMax]) + "…"
}

// ensureErrataEntityExists validates the target entity exists, dispatching by id
// prefix. Errata is supported for feature/decision/runbook/note.
func ensureErrataEntityExists(repoDir, id string) error {
	switch {
	case domain.IsFeatureID(id):
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

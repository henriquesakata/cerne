package domain

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// MinAliasLen is the minimum rune length for a historical entity alias. The
// floor guards the lint against noise from short/generic needles; it mirrors
// the word-boundary guard already used for entity titles.
const MinAliasLen = 5

var (
	// ErrNameRequired is the sentinel for a historical entity with an empty name.
	ErrNameRequired = errors.New("historical entity requires a non-empty name")
	// ErrSupersededByRequired is the sentinel for a historical entity with no successor.
	ErrSupersededByRequired = errors.New("historical entity requires a non-empty superseded_by")
	// ErrReasonRequired is the sentinel for a historical entity with no reason.
	ErrReasonRequired = errors.New("historical entity requires a non-empty reason")
	// ErrAliasTooShort is the sentinel for an alias below MinAliasLen runes.
	ErrAliasTooShort = errors.New("alias too short")
)

// ValidateHistorical checks the invariants of a historical entity: a name, a
// successor (superseded_by, a cerne ID or a free label), a reason, and aliases
// each at least MinAliasLen runes long. A historical entity is born terminal,
// so there is no state machine to consult here.
func ValidateHistorical(name, supersededBy, reason string, aliases []string) error {
	if name == "" {
		return ErrNameRequired
	}
	if supersededBy == "" {
		return ErrSupersededByRequired
	}
	if reason == "" {
		return ErrReasonRequired
	}
	for _, a := range aliases {
		if utf8.RuneCountInString(a) < MinAliasLen {
			return fmt.Errorf("%w: %q (minimum %d characters)", ErrAliasTooShort, a, MinAliasLen)
		}
	}
	return nil
}

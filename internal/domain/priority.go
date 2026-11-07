package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ValidPriorities is the closed set of feature/epic/task priority labels
// (F-0204). Ordered p0 (highest) to p3 (lowest) — a market-standard vocabulary
// (Jira/Linear), compared and sorted as plain strings since the set is closed
// and single-digit.
var ValidPriorities = []string{"p0", "p1", "p2", "p3"}

// ErrInvalidPriority is returned by NormalizePriority for a value outside
// ValidPriorities.
var ErrInvalidPriority = errors.New("invalid priority")

func validPriority(p string) bool {
	for _, v := range ValidPriorities {
		if v == p {
			return true
		}
	}
	return false
}

// NormalizePriority lowercases and trims in; an empty result is valid (no
// priority set — the field stays omitempty). A non-empty value outside
// ValidPriorities yields ErrInvalidPriority, naming the offender.
func NormalizePriority(in string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(in))
	if p == "" {
		return "", nil
	}
	if !validPriority(p) {
		return "", fmt.Errorf("%w: %s (valid: %s)", ErrInvalidPriority, p, strings.Join(ValidPriorities, ", "))
	}
	return p, nil
}

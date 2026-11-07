package domain

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrParentSelf is returned when a feature is set as its own parent.
	ErrParentSelf = errors.New("feature cannot be its own parent")
	// ErrParentCycle is returned when a parent link would close a cycle.
	ErrParentCycle = errors.New("parent link would create a cycle")
)

// CanonicalID is the stored form of an entity id: trimmed and upper-cased, so
// " f-0001 " and "F-0001" name the same feature.
func CanonicalID(id string) string { return strings.ToUpper(strings.TrimSpace(id)) }

// ValidateParentLink checks that pointing childID's parent at newParent keeps
// the parent forest acyclic. parentOf maps each feature ID to its current
// parent ID (entries without a parent may be omitted) and represents the state
// BEFORE the change. Self-reference yields ErrParentSelf; when childID is
// already an ancestor of newParent the new link would close a cycle, yielding
// ErrParentCycle. Existence of newParent is the caller's concern, not checked
// here. The walk is guarded against a pre-existing cycle in parentOf. Ids
// compare canonically (CanonicalID), legacy lowercase parents included.
func ValidateParentLink(childID, newParent string, parentOf map[string]string) error {
	childID, newParent = CanonicalID(childID), CanonicalID(newParent)
	if newParent == childID {
		return fmt.Errorf("%w: %s", ErrParentSelf, childID)
	}
	visited := map[string]bool{}
	for cur := newParent; cur != ""; cur = CanonicalID(parentOf[cur]) {
		if cur == childID {
			return fmt.Errorf("%w: %s is already an ancestor of %s", ErrParentCycle, childID, newParent)
		}
		if visited[cur] {
			break // pre-existing cycle in the map: not the one we are introducing
		}
		visited[cur] = true
	}
	return nil
}

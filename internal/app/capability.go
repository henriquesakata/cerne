package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// Sentinels for the capability taxonomy (F-0188).
var (
	// ErrUnknownCapability marks a C- id that does not exist in the backend.
	ErrUnknownCapability = errors.New("unknown capability")
	// ErrCapabilityHasChildren blocks retiring a node with active children.
	ErrCapabilityHasChildren = errors.New("capability has active children")
	// ErrCapabilityLinked blocks retiring a node still linked by features.
	ErrCapabilityLinked = errors.New("capability is linked by features")
)

// CreateCapability mints the next C- id as an active capability under the
// optional parent (which must exist and keep the taxonomy acyclic) and
// commits+pushes the backend. Nothing is written on a validation error.
func CreateCapability(repoDir, projDir, title, parent string) (store.Capability, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return store.Capability{}, fmt.Errorf("capability title cannot be empty")
	}
	parent = strings.TrimSpace(parent)
	var c store.Capability
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		all, err := store.LoadAllCapabilities(repoDir)
		if err != nil {
			return "", err
		}
		if parent != "" {
			if _, err := store.LoadCapability(repoDir, parent); err != nil {
				return "", fmt.Errorf("%w: %s", ErrUnknownCapability, parent)
			}
		}
		id, err := store.NextID(repoDir, "C")
		if err != nil {
			return "", err
		}
		if parent != "" {
			parentOf := map[string]string{}
			for _, e := range all {
				parentOf[e.ID] = e.Parent
			}
			if err := domain.ValidateParentLink(id, parent, parentOf); err != nil {
				return "", err
			}
		}
		now := time.Now().UTC()
		c = store.Capability{
			ID:        id,
			Title:     title,
			Parent:    parent,
			State:     domain.CapabilityActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.SaveCapability(repoDir, c); err != nil {
			return "", err
		}
		return "cerne: capability new " + id, nil
	})
	return c, err
}

// RetireCapability moves an active capability to superseded, refusing while
// the node still has active children or features linked to it (crit. 6) —
// reallocate first, then retire.
func RetireCapability(repoDir, projDir, id, reason string) (store.Capability, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return store.Capability{}, fmt.Errorf("--reason is required to retire a capability")
	}
	var c store.Capability
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		c, err = store.LoadCapability(repoDir, id)
		if err != nil {
			return "", fmt.Errorf("%w: %s", ErrUnknownCapability, id)
		}
		all, err := store.LoadAllCapabilities(repoDir)
		if err != nil {
			return "", err
		}
		for _, e := range all {
			if e.Parent == id && e.State == domain.CapabilityActive {
				return "", fmt.Errorf("%w: %s under %s — reparent or retire it first", ErrCapabilityHasChildren, e.ID, id)
			}
		}
		features, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			return "", err
		}
		for _, f := range features {
			for _, cid := range f.Capabilities {
				if cid == id {
					return "", fmt.Errorf("%w: %s links %s — relink with 'cerne feature capability' first", ErrCapabilityLinked, f.ID, id)
				}
			}
		}
		c.State = domain.CapabilitySuperseded
		c.Reason = reason
		c.UpdatedAt = time.Now().UTC()
		if err := store.SaveCapability(repoDir, c); err != nil {
			return "", err
		}
		return "cerne: capability retire " + id, nil
	})
	return c, err
}

// LinkFeatureCapabilities sets (or clears) the validated capability list on a
// feature. Unknown feature or capability ids abort with nothing written.
func LinkFeatureCapabilities(repoDir, projDir, featureID string, capIDs []string, clear bool) (store.Feature, error) {
	var f store.Feature
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		f, err = store.LoadFeature(repoDir, featureID)
		if err != nil {
			return "", fmt.Errorf("feature %s: %w", featureID, err)
		}
		if clear {
			f.Capabilities = nil
		} else {
			for _, cid := range capIDs {
				if _, err := store.LoadCapability(repoDir, cid); err != nil {
					return "", fmt.Errorf("%w: %s", ErrUnknownCapability, cid)
				}
			}
			f.Capabilities = capIDs
		}
		f.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		return "cerne: feature capability " + featureID, nil
	})
	return f, err
}

// CountFeaturesByCapability aggregates how many features link each capability
// id — the derived side of the N:N (the list on the feature is the source).
func CountFeaturesByCapability(features []store.Feature) map[string]int {
	counts := map[string]int{}
	for _, f := range features {
		for _, cid := range f.Capabilities {
			counts[cid]++
		}
	}
	return counts
}

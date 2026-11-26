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

// Sentinels for the capability taxonomy (F-0188).
var (
	// ErrUnknownCapability marks a C- id that does not exist in the backend.
	ErrUnknownCapability = errors.New("unknown capability")
	// ErrCapabilityHasChildren blocks retiring a node with active children.
	ErrCapabilityHasChildren = errors.New("capability has active children")
	// ErrCapabilityLinked blocks retiring a node still linked by features.
	ErrCapabilityLinked = errors.New("capability is linked by features")
)

// unknownCapability wraps ErrUnknownCapability with the id and the command
// that lists the valid ones (B-0082), so no refusal leaves the user guessing.
func unknownCapability(id string) error {
	return fmt.Errorf("%w: %s (see 'cerne capability list --tree')", ErrUnknownCapability, id)
}

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
				return "", unknownCapability(parent)
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
			return "", unknownCapability(id)
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
					return "", unknownCapability(cid)
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

// knownCapabilities canonicalizes ids (N-0061: a case-insensitive disk would
// load "c-0001" and store it lowercase), drops a repeat and refuses one the
// backend lacks.
func knownCapabilities(repoDir string, ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		id = domain.CanonicalID(id)
		if slices.Contains(out, id) {
			continue
		}
		if _, err := store.LoadCapability(repoDir, id); err != nil {
			return nil, unknownCapability(id)
		}
		out = append(out, id)
	}
	return out, nil
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

// activeCapabilities is knownCapabilities that also refuses a retired node:
// a card born or linked in bulk must not point at a superseded capability.
func activeCapabilities(repoDir string, ids []string) ([]string, error) {
	out, err := knownCapabilities(repoDir, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range out {
		if c, _ := store.LoadCapability(repoDir, id); c.State != domain.CapabilityActive {
			return nil, fmt.Errorf("%w: %s is retired (see 'cerne capability list --tree')", ErrUnknownCapability, id)
		}
	}
	return out, nil
}

// LinkFeaturesCapabilities is LinkFeatureCapabilities over a batch (F-0514):
// every target and capability is validated before the first write, and the
// whole batch lands in one commit. Ids are canonicalized and deduplicated.
func LinkFeaturesCapabilities(repoDir, projDir string, featureIDs, capIDs []string, clear bool) ([]store.Feature, error) {
	var out []store.Feature
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		out = nil
		var caps []string
		if !clear {
			var err error
			if caps, err = activeCapabilities(repoDir, capIDs); err != nil {
				return "", err
			}
		}
		var ids []string
		for _, id := range featureIDs {
			if id = domain.CanonicalID(id); !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			f, err := store.LoadFeature(repoDir, id)
			if err != nil {
				return "", fmt.Errorf("feature %s: %w", id, err)
			}
			f.Capabilities = caps
			f.UpdatedAt = time.Now().UTC()
			out = append(out, f)
		}
		for _, f := range out {
			if err := store.SaveFeature(repoDir, f); err != nil {
				return "", err
			}
		}
		return "cerne: feature capability " + strings.Join(ids, ","), nil
	})
	return out, err
}

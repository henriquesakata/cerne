package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/slug"
	"github.com/henriquesakata/cerne/internal/store"
)

const (
	// historicalRecordedEvent is the event type appended when a new historical
	// entity is registered (no lifecycle transition — it is born terminal).
	historicalRecordedEvent = "historical.recorded"
	// historicalUpdatedEvent is the event type appended when re-recording an
	// existing historical entity (same normalized name) updates it in place.
	historicalUpdatedEvent = "historical.updated"
)

// SaveHistoricalEntity validates and upserts a historical entity in the backend
// clone at repoDir, keyed by the normalized name (slug.Fold — case- and
// accent-insensitive). It does NOT commit — callers decide when to sync
// (RecordHistorical for the single-shot CLI path, the import command for a
// batch sharing one commit). source records the origin ("cli" or "import").
//
// When no entity with the same normalized name exists it allocates the next H-
// id, writes the entity (born domain.StateSuperseded), appends the
// historical.recorded event and returns created=true. When one exists it
// updates that entity in place — keeping its ID and CreatedAt, overwriting
// SupersededBy/Reason/ImportedFrom, unioning aliases (existing ∪ new,
// order-stable, deduped case-insensitively) — appends historical.updated and
// returns created=false. Validation runs before the lookup in both paths, so a
// malformed input never touches the backend.
func SaveHistoricalEntity(repoDir, name, supersededBy, reason string, aliases []string, importedFrom, source string) (store.Historical, bool, error) {
	if err := domain.ValidateHistorical(name, supersededBy, reason, aliases); err != nil {
		return store.Historical{}, false, err
	}
	existing, found, err := findHistoricalByName(repoDir, name)
	if err != nil {
		return store.Historical{}, false, err
	}

	var (
		h       store.Historical
		evType  string
		created bool
	)
	if found {
		h = existing
		h.Name = name
		h.Aliases = unionAliases(existing.Aliases, aliases)
		h.SupersededBy = supersededBy
		h.Reason = reason
		h.ImportedFrom = importedFrom
		evType = historicalUpdatedEvent
	} else {
		id, err := store.NextID(repoDir, "H")
		if err != nil {
			return store.Historical{}, false, err
		}
		h = store.Historical{
			ID:           id,
			Name:         name,
			Aliases:      aliases,
			SupersededBy: supersededBy,
			Reason:       reason,
			State:        domain.StateSuperseded,
			ImportedFrom: importedFrom,
			CreatedAt:    time.Now().UTC(),
		}
		evType = historicalRecordedEvent
		created = true
	}

	if err := store.SaveHistorical(repoDir, h); err != nil {
		return store.Historical{}, false, err
	}
	payload := map[string]string{
		"name":          h.Name,
		"superseded_by": supersededBy,
		"reason":        reason,
		"source":        source,
	}
	if len(h.Aliases) > 0 {
		payload["aliases"] = strings.Join(h.Aliases, ",")
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent(h.ID, evType, payload, EventActor())); err != nil {
		return store.Historical{}, false, err
	}
	return h, created, nil
}

// findHistoricalByName returns the persisted historical entity whose name folds
// (slug.Fold) to the same key as name; found is false when none matches.
func findHistoricalByName(repoDir, name string) (store.Historical, bool, error) {
	all, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		return store.Historical{}, false, err
	}
	key := slug.Fold(name)
	for _, h := range all {
		if slug.Fold(h.Name) == key {
			return h, true, nil
		}
	}
	return store.Historical{}, false, nil
}

// unionAliases returns existing followed by the new aliases not already present
// (case-insensitive comparison via slug.Fold), preserving order and the
// original casing of the first occurrence of each alias.
func unionAliases(existing, added []string) []string {
	seen := make(map[string]bool, len(existing)+len(added))
	out := make([]string, 0, len(existing)+len(added))
	add := func(a string) {
		key := slug.Fold(a)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, a)
	}
	for _, a := range existing {
		add(a)
	}
	for _, a := range added {
		add(a)
	}
	return out
}

// RecordHistorical upserts a historical entity and syncs the backend: it runs
// SaveHistoricalEntity (source "cli"), then commits+pushes and refreshes the
// docs.lock snapshot in projDir. created reports whether a new entity was
// allocated (false when an existing one with the same normalized name was
// updated). This is the single-shot path behind the `cerne retire` command; the
// import command reuses SaveHistoricalEntity in a loop under one commit instead.
func RecordHistorical(repoDir, projDir, name, supersededBy, reason string, aliases []string, importedFrom string) (store.Historical, bool, error) {
	var (
		h       store.Historical
		created bool
	)
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		h, created, err = SaveHistoricalEntity(repoDir, name, supersededBy, reason, aliases, importedFrom, "cli")
		if err != nil {
			return "", err
		}
		verb := "retire"
		if !created {
			verb = "update"
		}
		return fmt.Sprintf("cerne: %s %s (%s)", verb, h.ID, name), nil
	})
	if err != nil {
		return store.Historical{}, false, err
	}
	return h, created, nil
}

// removeAliases returns existing without the aliases in forget (case-insensitive
// via slug.Fold), preserving order and casing of the kept entries. Entries in
// forget that are absent are ignored; an entity may legitimately end with zero
// aliases.
func removeAliases(existing, forget []string) []string {
	drop := make(map[string]bool, len(forget))
	for _, a := range forget {
		drop[slug.Fold(a)] = true
	}
	kept := make([]string, 0, len(existing))
	for _, a := range existing {
		if !drop[slug.Fold(a)] {
			kept = append(kept, a)
		}
	}
	return kept
}

// ForgetHistoricalAliases removes the given aliases (case-insensitive, via
// slug.Fold) from the historical entity named name, then syncs the backend.
// Aliases not present are silently skipped. It errors when no historical matches
// name. This is the curation counterpart to retire's union semantics: drop a
// noisy/ambiguous alias (e.g. a common domain word like "bills" that the lint
// would otherwise flag in every document). Leaving an entity alias-less is
// allowed — it then matches by id only.
func ForgetHistoricalAliases(repoDir, projDir, name string, forget []string) (store.Historical, error) {
	var h store.Historical
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var found bool
		var err error
		h, found, err = findHistoricalByName(repoDir, name)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf("no historical entity named %q", name)
		}
		h.Aliases = removeAliases(h.Aliases, forget)
		if err := store.SaveHistorical(repoDir, h); err != nil {
			return "", err
		}
		payload := map[string]string{"name": h.Name}
		if len(h.Aliases) > 0 {
			payload["aliases"] = strings.Join(h.Aliases, ",")
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(h.ID, historicalUpdatedEvent, payload, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: forget aliases on %s (%s)", h.ID, name), nil
	})
	if err != nil {
		return store.Historical{}, err
	}
	return h, nil
}

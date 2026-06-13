package app

import (
	"fmt"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// historicalRecordedEvent is the event type appended when a historical entity
// is registered (no lifecycle transition — it is born terminal).
const historicalRecordedEvent = "historical.recorded"

// SaveHistoricalEntity validates and persists a historical entity in the
// backend clone at repoDir: it allocates the next H- id, writes the entity
// (born domain.StateSuperseded) and appends the historical.recorded event. It
// does NOT commit — callers decide when to sync (RecordHistorical for the
// single-shot CLI path, the import command for a batch sharing one commit).
// source records the origin ("cli" or "import").
func SaveHistoricalEntity(repoDir, name, supersededBy, reason string, aliases []string, importedFrom, source string) (store.Historical, error) {
	var h store.Historical
	if err := domain.ValidateHistorical(name, supersededBy, reason, aliases); err != nil {
		return h, err
	}
	id, err := store.NextID(repoDir, "H")
	if err != nil {
		return h, err
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
	if err := store.SaveHistorical(repoDir, h); err != nil {
		return store.Historical{}, err
	}
	payload := map[string]string{
		"name":          name,
		"superseded_by": supersededBy,
		"reason":        reason,
		"source":        source,
	}
	if len(aliases) > 0 {
		payload["aliases"] = strings.Join(aliases, ",")
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent(id, historicalRecordedEvent, payload, EventActor())); err != nil {
		return store.Historical{}, err
	}
	return h, nil
}

// RecordHistorical registers a historical entity and syncs the backend: it
// runs SaveHistoricalEntity (source "cli"), then commits+pushes and refreshes
// the docs.lock snapshot in projDir. This is the single-shot path behind the
// `cerne retire` command; the import command reuses SaveHistoricalEntity in a
// loop under one commit instead.
func RecordHistorical(repoDir, projDir, name, supersededBy, reason string, aliases []string, importedFrom string) (store.Historical, error) {
	h, err := SaveHistoricalEntity(repoDir, name, supersededBy, reason, aliases, importedFrom, "cli")
	if err != nil {
		return store.Historical{}, err
	}
	if err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: retire %s (%s)", h.ID, name)); err != nil {
		return store.Historical{}, err
	}
	return h, nil
}

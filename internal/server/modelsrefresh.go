package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/models"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

var (
	// catalogRefreshOnce holds the boot refresh to once per process (F-0311
	// criteria 18/20): a second project — even a second ui handler — never
	// queries the provider again.
	catalogRefreshOnce sync.Once
	// catalogRefreshDone closes when that refresh settles (tests wait on it).
	catalogRefreshDone = make(chan struct{})
)

// startCatalogRefresh launches the opportunistic catalog refresh in the
// background under the server's ctx: it never holds the boot nor the first
// render (criterion 21), and shutting the server down cancels the listing.
func startCatalogRefresh(ctx context.Context, hub *multiHub, projDir string) {
	catalogRefreshOnce.Do(func() {
		done := make(chan struct{})
		catalogRefreshDone = done
		go func() {
			defer close(done)
			refreshCatalogAtBoot(ctx, hub, projDir)
		}()
	})
}

// modelsChange is the "models" live event: what a refresh applied (D-0005:
// apply, then notify).
type modelsChange struct {
	At time.Time `json:"at"`
	models.Diff
}

// refreshCatalogAtBoot refreshes a catalog older than the 24h window. A failure
// of any kind is logged (F-0149) and leaves the catalog untouched; no provider
// configured is silent; a change is announced on the live stream.
func refreshCatalogAtBoot(ctx context.Context, hub *multiHub, projDir string) {
	now := nowFunc()
	if models.Fresh(loadCatalog(), now) {
		return
	}
	diff, err := app.RefreshModels(ctx, projDir, now)
	switch {
	case errors.Is(err, app.ErrNoModelProvider):
		return
	case err != nil:
		telemetry.Warn("model catalog refresh failed; keeping the current catalog", "error", err.Error())
		return
	case diff.Empty():
		return
	}
	data, _ := json.Marshal(modelsChange{At: now.UTC(), Diff: diff})
	hub.broadcast(liveEvent{kind: "models", data: string(data)})
}

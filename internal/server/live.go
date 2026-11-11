package server

import (
	"context"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// headPollInterval is how often the watcher reads the backend clone's HEAD.
// One ref read per second per running `cerne ui` is negligible.
const headPollInterval = time.Second

// keepaliveInterval bounds idle time on an SSE connection so proxies and the
// browser keep it open between real change events.
const keepaliveInterval = 25 * time.Second

// pullLoop refreshes the backend clone from origin every interval so a
// teammate's push reaches the dashboard. It never talks to the hub: a pull that
// brings commits advances HEAD, which watchHeadMulti then notices — one
// notification path for local and remote changes alike. Each tick is a
// gitback.SafePullNow — locked, skipped on a dirty worktree, ff-only — so a
// tick can never poison the shared clone (N-0027). Now, not SafePull: freshness
// is this loop's entire job, and inheriting the CLI's 60s elision window
// (F-0200) would stretch the interval to a minute and refreeze the UI that
// F-0517 unfroze. Pull errors (offline, auth) are ignored so the dashboard
// keeps serving the cached clone.
func pullLoop(ctx context.Context, repoDir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = gitback.SafePullNow(repoDir)
		}
	}
}

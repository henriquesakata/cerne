package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/refresh"
	"github.com/henriquesakata/cerne/internal/version"
)

// autoRefresh renews the agents whose managed files are all current or ones a
// past release wrote (F-0477), at the points where the out-of-date warning
// shows. It returns the one line that says what it renewed ("" when nothing)
// and the paths of the agents it left alone, which the warning names. It reads
// files and writes only the renewed agents' own; it opens no connection, never
// replaces the binary and connects no agent that was not connected. Off with
// hooks.auto_refresh = false, and fail-open: any problem renews nothing.
func autoRefresh(projDir string) (line string, leftover []string) {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || !project.AutoRefresh(projDir) {
		return "", nil
	}
	// With sync_skills on the guide is sync_skills' own (decision 3).
	withGuide := !cfg.SyncSkills
	classify := func() (renewable []hooks.Provider, leftover []string) {
		for _, p := range refresh.Connected(projDir) {
			// The plugin delivers Claude Code's hooks and guide: absent copies are
			// not stale there, and creating them would run every hook twice.
			if p == hooks.ProviderClaude && fromPlugin() {
				continue
			}
			switch v := refresh.Classify(projDir, p, withGuide); v.State {
			case refresh.Renewable:
				renewable = append(renewable, p)
			case refresh.Unrecognized:
				leftover = append(leftover, v.Unknown...)
			}
		}
		slices.Sort(leftover)
		return renewable, slices.Compact(leftover)
	}
	renewable, leftover := classify()
	if len(renewable) == 0 {
		return "", leftover
	}
	// Two sessions starting at once: one renews, the other goes on silently.
	unlock, ok := lockRefresh(cfg.ProjectID)
	if !ok {
		return "", leftover
	}
	defer unlock()
	renewable, leftover = classify() // what the other holder did is already on disk
	var renewed []string
	codexReview := false
	for _, p := range renewable {
		hookPart, guidePart := renewAgents(projDir, []hooks.Provider{p})
		if hookPart.state == stNotDone || guidePart.state == stNotDone {
			continue
		}
		if hookPart.state == stUpdated || guidePart.state == stUpdated { // only what really changed is said
			renewed = append(renewed, string(p))
			if p == hooks.ProviderCodex && strings.Contains(hookPart.detail, "hooks.json") {
				codexReview = true
			}
		}
	}
	if len(renewed) == 0 {
		return "", leftover
	}
	line = fmt.Sprintf("cerne: renewed %s to %s; hook changes apply from the next session.", strings.Join(renewed, ", "), version.Version)
	if codexReview {
		line += " Codex asks to review changed hooks again (/hooks)."
	}
	return line, leftover
}

// refreshLockStale is how long a lock file is believed: a renewal takes well
// under a second, so an older one belongs to a process that died.
const refreshLockStale = 30 * time.Second

// lockRefresh takes the project's renewal lock (an O_EXCL file in the cache
// folder); ok is false when another process holds a live one or the lock cannot
// be made (fail-open: nothing is renewed then).
func lockRefresh(projectID string) (unlock func(), ok bool) {
	dir, err := project.CacheDir(projectID)
	if err != nil || os.MkdirAll(dir, 0o755) != nil {
		return nil, false
	}
	path := filepath.Join(dir, "refresh.lock")
	for try := 0; try < 2; try++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, true
		}
		if fi, sErr := os.Stat(path); sErr == nil && time.Since(fi.ModTime()) > refreshLockStale {
			os.Remove(path) // a dead holder's
			continue
		}
		return nil, false
	}
	return nil, false
}

// leftoverNote says why the leftover paths were not renewed.
func leftoverNote(leftover []string) string {
	return "left as they are, edited in the project or written by a newer cerne: " + strings.Join(leftover, ", ")
}

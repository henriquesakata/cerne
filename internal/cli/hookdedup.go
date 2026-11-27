package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// hookDedupDir is the cache subdirectory that holds dedup markers.
const hookDedupDir = "hook-dedup"

// hookDedupTTL is how long a dedup marker is considered valid. The two
// executions of the same event (from .cursor/hooks.json and
// .claude/settings.json) happen milliseconds apart; 10 minutes is generous.
const hookDedupTTL = 10 * time.Minute

// claimCursorHookCall reports whether this hook invocation should run. It is
// active only when in.cursor() is true and the payload carries enough identity
// fields to build a deterministic call key.
//
// First invocation (first layer, e.g. .cursor/hooks.json): creates a marker
// file atomically and returns true — the hook should run.
// Second invocation of the same call (e.g. .claude/settings.json, milliseconds
// later): finds the marker already exists and returns false — suppress silently.
//
// Always fail-open (N-0021): any filesystem error or missing identity returns
// true so the hook runs.
func claimCursorHookCall(cacheDir, hookName string, in hookInput) bool {
	if !in.cursor() {
		return true // only dedup Cursor payloads
	}
	c := in.Cursor
	// Require session identity; without it the key is not unique.
	if in.SessionID == "" {
		return true
	}

	key := dedupKey(hookName, in.SessionID, c.HookEventName, c)
	dir := filepath.Join(cacheDir, hookDedupDir)
	path := filepath.Join(dir, key)

	// Prune old markers opportunistically (best-effort, ignore errors).
	_ = pruneOldMarkers(dir)

	// Try to claim with O_CREATE|O_EXCL. If the directory does not exist
	// yet, create it first and retry once. Any other error is fail-open.
	f, err := openExcl(path)
	if err == nil {
		f.Close()
		return true // claimed — first invocation
	}
	if os.IsExist(err) {
		return false // marker exists — second invocation, suppress
	}
	// Path error (dir missing): create the dir and retry.
	if os.MkdirAll(dir, 0o755) != nil {
		return true // fail-open: cannot create dedup dir
	}
	f, err = openExcl(path)
	if err == nil {
		f.Close()
		return true // claimed after creating dir
	}
	if os.IsExist(err) {
		return false // race: another layer just created the marker
	}
	return true // any other error: fail-open
}

// openExcl opens path with O_CREATE|O_EXCL|O_WRONLY.
func openExcl(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
}

// dedupKey builds a deterministic, path-safe sha256 hex hash that uniquely
// identifies one specific Cursor tool invocation.
func dedupKey(hookName, sessionID, hookEventName string, c *cursorInput) string {
	var callKey string
	switch {
	case c.ToolUseID != "":
		callKey = c.ToolUseID
	case c.GenerationID != "":
		// generation_id + loop_count + status (status distinguishes completed
		// from aborted stops in the same generation).
		callKey = c.GenerationID + ":" + strconv.Itoa(c.LoopCount) + ":" + c.Status
	case hookEventName == "sessionStart":
		callKey = "sessionStart"
	default:
		callKey = hookEventName + ":" + strconv.Itoa(c.LoopCount) + ":" + c.Status
	}
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s", hookName, sessionID, hookEventName, callKey)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// pruneOldMarkers removes dedup marker files older than hookDedupTTL.
// Best-effort: errors are ignored.
func pruneOldMarkers(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-hookDedupTTL)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

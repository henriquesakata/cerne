package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// Session→work binding (F-0151): the work gate needs to answer "does THIS
// Claude Code session have a live registration?" mechanically. The PostToolUse
// bind hook writes <cacheDir>/sessions/<session_id> containing the W-/F- id;
// the PreToolUse gate reads it back through CurrentBinding, which validates
// the entity is still live (self-healing: a stale binding reads as absent and
// is removed). The binding is local per machine — exactly the scope of a
// Claude Code session — so it lives in the cache dir, never in the backend.

// sessionsSubdir is the per-project cache subdirectory holding one file per
// bound session.
const sessionsSubdir = "sessions"

// ErrUnsafeSessionID is the sentinel for a session id that cannot be used as a
// file name (empty, or carrying path separators / dot-dot).
var ErrUnsafeSessionID = errors.New("unsafe session id")

// safeSessionID validates sess can be used verbatim as a file name inside the
// sessions dir.
func safeSessionID(sess string) error {
	if sess == "" || sess == "." || sess == ".." ||
		strings.ContainsAny(sess, `/\`) {
		return fmt.Errorf("%w: %q", ErrUnsafeSessionID, sess)
	}
	return nil
}

// BindSession records that session sess is working under entity id (a W- work
// or F- feature), overwriting any previous binding for the session.
func BindSession(cacheDir, sess, id string) error {
	if err := safeSessionID(sess); err != nil {
		return err
	}
	dir := filepath.Join(cacheDir, sessionsSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, sess), []byte(id+"\n"), 0o644)
}

// CurrentBinding resolves the binding for session sess against the backend
// clone at repoDir: it returns the bound id only while the entity is still
// live (active work / in_development feature). A missing binding is (_, false,
// nil); a stale one is removed (self-heal) and also reads as absent.
func CurrentBinding(cacheDir, repoDir, sess string) (string, bool, error) {
	if err := safeSessionID(sess); err != nil {
		return "", false, err
	}
	path := filepath.Join(cacheDir, sessionsSubdir, sess)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	id := strings.TrimSpace(string(raw))
	if bindingLive(repoDir, id) {
		return id, true, nil
	}
	_ = os.Remove(path) // self-heal: stale binding
	return "", false, nil
}

// bindingLive reports whether id still names a live registration: an active
// work or an in_development feature. Anything else — terminal states, unknown
// prefixes, a missing entity — is stale.
func bindingLive(repoDir, id string) bool {
	switch {
	case strings.HasPrefix(id, "W-"):
		w, err := store.LoadWork(repoDir, id)
		return err == nil && w.State == domain.WorkActive
	case strings.HasPrefix(id, "F-"):
		f, err := store.LoadFeature(repoDir, id)
		return err == nil && f.State == domain.StateInDevelopment
	default:
		return false
	}
}

// LiveBindings enumerates the ids with a live registration binding on this
// machine for the project (F-0173): every session file is resolved with the
// same liveness check as CurrentBinding — dead bindings self-heal off disk —
// and the surviving ids are deduplicated. The commit trailer stamps only when
// exactly one id comes back: ambiguity never becomes a guess.
func LiveBindings(cacheDir, repoDir string) ([]string, error) {
	dir := filepath.Join(cacheDir, sessionsSubdir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(raw))
		if !bindingLive(repoDir, id) {
			_ = os.Remove(path) // self-heal: stale binding
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ClearBindings removes every session binding pointing at id (used when the
// work is done/abandoned). Other sessions' bindings are untouched; a missing
// sessions dir is a no-op.
func ClearBindings(cacheDir, id string) error {
	dir := filepath.Join(cacheDir, sessionsSubdir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(raw)) == id {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

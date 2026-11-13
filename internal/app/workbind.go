package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// Session→work binding (F-0151): the work gate needs to answer "does THIS
// Claude Code session have a live registration?" mechanically. The PostToolUse
// bind hook writes <cacheDir>/sessions/<session_id> containing the W-/F- id;
// the PreToolUse gate reads it back through CurrentBinding, which validates
// the entity is still live (self-healing: a stale binding reads as absent and
// is removed). The binding is local per machine — exactly the scope of a
// Claude Code session — so it lives in the cache dir, never in the backend.
//
// Every bind also appends a line to the cost ledger (F-0309,
// <cacheDir>/costbind.ndjson): additive, outside sessions/, and never touched
// by any cleanup path below — it is the history cost attributes tokens by.

// sessionsSubdir is the per-project cache subdirectory holding one file per
// bound session.
const sessionsSubdir = "sessions"

// ErrUnsafeSessionID is the sentinel for a session (or agent) id that cannot
// be used as a file name (empty, or carrying path separators or dots).
var ErrUnsafeSessionID = errors.New("unsafe session id")

// safeSessionID validates sess (or an agent id) can be used verbatim as one
// piece of a binding file name: no separator and no dot, so "<sess>.<agent>"
// always splits back into the two (F-0376).
func safeSessionID(sess string) error {
	if sess == "" || strings.ContainsAny(sess, `/\.`) {
		return fmt.Errorf("%w: %q", ErrUnsafeSessionID, sess)
	}
	return nil
}

// bindingName is the file of (sess, agent) in the sessions dir: "<sess>" for
// the main thread — the pre-F-0376 name, read as is, no migration — and
// "<sess>.<agent>" for a subagent, whose session_id is the parent's (F-0376).
func bindingName(sess, agent string) (string, error) {
	if err := safeSessionID(sess); err != nil {
		return "", err
	}
	if agent == "" {
		return sess, nil
	}
	if err := safeSessionID(agent); err != nil {
		return "", err
	}
	return sess + "." + agent, nil
}

// splitBindingName is bindingName's inverse: the session a binding file
// belongs to, and its agent ("" for the main thread).
func splitBindingName(name string) (sess, agent string) {
	sess, agent, _ = strings.Cut(name, ".")
	return sess, agent
}

// BindSession records that session sess is working under entity id (a W- work
// or F- feature), overwriting any previous binding for the session.
func BindSession(cacheDir, sess, id string) error {
	return BindSessionAgent(cacheDir, sess, "", cost.CurrentHarness(), id)
}

// BindSessionAgent is BindSession from inside subagent agent ("" for the main
// thread) under harness: a subagent gets its own binding file, never the main
// thread's (F-0376); the cost ledger line also carries the agent and the
// harness (F-0309).
func BindSessionAgent(cacheDir, sess, agent, harness, id string) error {
	name, err := bindingName(sess, agent)
	if err != nil {
		return err
	}
	dir := filepath.Join(cacheDir, sessionsSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(id+"\n"), 0o644); err != nil {
		return err
	}
	return cost.AppendBind(cacheDir, cost.Bind{Session: sess, Agent: agent, Entity: id, Harness: harness, TS: time.Now().UTC()})
}

// TouchBinding refreshes the mtime of the (sess, agent) binding file — its
// "last signal" (F-0361, keyed per agent since F-0376). A key without a
// binding file is a silent no-op: it never creates one. Contents are
// untouched; local only.
func TouchBinding(cacheDir, sess, agent string) error {
	name, err := bindingName(sess, agent)
	if err != nil {
		return err
	}
	now := time.Now()
	err = os.Chtimes(filepath.Join(cacheDir, sessionsSubdir, name), now, now)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// SessionSignal is one session binding on this machine and when it last
// signalled (F-0361).
type SessionSignal struct {
	Session    string
	Agent      string // "" for the main thread (F-0376)
	Entity     string
	LastSignal time.Time
}

// SessionSignals lists every binding file under <cacheDir>/sessions read-only:
// no cleanup, no liveness check — callers decide what is relevant (F-0361).
func SessionSignals(cacheDir string) []SessionSignal {
	dir := filepath.Join(cacheDir, sessionsSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []SessionSignal
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		sess, agent := splitBindingName(e.Name())
		out = append(out, SessionSignal{Session: sess, Agent: agent, Entity: strings.TrimSpace(string(raw)), LastSignal: info.ModTime().UTC()})
	}
	return out
}

// CurrentBinding resolves the binding of (sess, agent) against the backend
// clone at repoDir: it returns the bound id only while the entity is still
// live (active work / in_development feature). A missing binding is (_, false,
// nil); a stale one is removed (self-heal) and also reads as absent. Agent ""
// reads the main thread's binding only; a subagent reads its own and, without
// a live one, the main thread's (F-0376 crit. 4).
func CurrentBinding(cacheDir, repoDir, sess, agent string) (string, bool, error) {
	name, err := bindingName(sess, agent)
	if err != nil {
		return "", false, err
	}
	if agent != "" {
		if id, ok, err := readBinding(cacheDir, repoDir, name); err != nil || ok {
			return id, ok, err
		}
	}
	return readBinding(cacheDir, repoDir, sess)
}

// readBinding is CurrentBinding for one binding file name.
func readBinding(cacheDir, repoDir, name string) (string, bool, error) {
	path := filepath.Join(cacheDir, sessionsSubdir, name)
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
	case domain.IsFeatureID(id):
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

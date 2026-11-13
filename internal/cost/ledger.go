// Package cost attributes the real token usage recorded in Claude Code session
// transcripts to cerne entities (F-0309). The source is the transcript, never
// the agent's own report; the attribution key is an append-only ledger of
// session bindings, kept beside the ephemeral sessions/ binding files. Tokens
// only, never money.
package cost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// HarnessClaudeCode marks a bind made inside Claude Code (CLAUDECODE=1); any
// other harness is recorded as "other" and refused by cost (F-0309).
const HarnessClaudeCode = "claude-code"

// ledgerFile lives at the cache root, never inside sessions/ (where every file
// is read as a binding).
const ledgerFile = "costbind.ndjson"

// Bind is one line of <cache>/costbind.ndjson: session (and, from inside a
// subagent, agent) started working under entity at TS. Append-only. A Retro
// bind (`cerne cost attribute`, F-0384) covers every message of its agent,
// before TS too; Role names what the agent did there.
type Bind struct {
	Session string    `json:"session_id"`
	Agent   string    `json:"agent_id,omitempty"`
	Entity  string    `json:"entity_id"`
	Harness string    `json:"harness"`
	Role    string    `json:"role,omitempty"`
	Retro   bool      `json:"retro,omitempty"`
	TS      time.Time `json:"ts"`
}

// CurrentHarness names the harness this process runs under: claude-code when
// Claude Code's CLAUDECODE=1 marker is inherited, "other" otherwise.
func CurrentHarness() string {
	if os.Getenv("CLAUDECODE") == "1" {
		return HarnessClaudeCode
	}
	return "other"
}

// AppendBind appends b as one JSON line to the ledger, creating it on first
// use. One Write of the whole line keeps concurrent appends from interleaving.
func AppendBind(cacheDir string, b Bind) error {
	line, err := json.Marshal(b)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(cacheDir, ledgerFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// readLedger reads every well-formed ledger line in file order; a malformed
// line (a write cut short) is skipped. A missing ledger is (nil, false, nil).
func readLedger(cacheDir string) (binds []Bind, exists bool, err error) {
	raw, err := os.ReadFile(filepath.Join(cacheDir, ledgerFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var b Bind
		if json.Unmarshal(line, &b) != nil || b.Session == "" || b.Entity == "" {
			continue
		}
		binds = append(binds, b)
	}
	return binds, true, nil
}

var (
	// ErrAgentNotFound: `cerne cost attribute` names a subagent with no
	// transcript in the session — the bind would attribute nothing.
	ErrAgentNotFound = errors.New("no transcript of that subagent in the session")
	// ErrUnknownRole: a role outside the closed vocabulary (bindRoles).
	ErrUnknownRole = errors.New("unknown role")
)

// bindRoles is the closed vocabulary of a bind's role; "main" is the main
// thread's, never bound.
var bindRoles = []string{"critic", "judge", "builder", "other"}

// Attribute records that subagent agent of session sess worked under entity
// id as role ("" lets its agentType decide): a retro bind, covering all its
// messages (F-0384). Refused, writing nothing, on an unknown role or when the
// session's transcripts hold no file of the agent. The session is then
// re-harvested, so a snapshot of the entity it leaves drops it at once.
func Attribute(in Inputs, sess, agent, id, role string, now time.Time) error {
	if role != "" && !slices.Contains(bindRoles, role) {
		return fmt.Errorf("%w %q: want one of %s", ErrUnknownRole, role, strings.Join(bindRoles, ", "))
	}
	files, err := sessionFiles(in.Roots, []string{sess})
	if err != nil {
		return err
	}
	name := "agent-" + agent + ".jsonl"
	if !slices.ContainsFunc(files[sess], func(f tfile) bool { return path.Base(f.rel) == name }) {
		dirs := make([]string, len(in.Roots))
		for i, root := range in.Roots {
			dirs[i] = filepath.Join(root, "projects", "*", sess, "subagents")
		}
		return fmt.Errorf("%w: no %s under %s", ErrAgentNotFound, name, strings.Join(dirs, ", "))
	}
	// The transcript was found, so the session is Claude Code's whatever the
	// harness this command runs under.
	if err := AppendBind(in.CacheDir, Bind{Session: sess, Agent: agent, Entity: id, Harness: HarnessClaudeCode, Role: role, Retro: true, TS: now}); err != nil {
		return err
	}
	_ = Harvest(in, sess, "", now) // fails open like the Stop hook, which harvests again
	return nil
}

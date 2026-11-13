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
	"io/fs"
	"os"
	"path/filepath"
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

package cost

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

// TestCurrentHarness (F-0309 crit. 13): a bind is marked claude-code only
// under the CLAUDECODE=1 marker Claude Code exports to its children.
func TestCurrentHarness(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	if got := CurrentHarness(); got != HarnessClaudeCode {
		t.Fatalf("CLAUDECODE=1: got %q, want %q", got, HarnessClaudeCode)
	}
	t.Setenv("CLAUDECODE", "")
	if got := CurrentHarness(); got != "other" {
		t.Fatalf("CLAUDECODE empty: got %q, want other", got)
	}
}

// TestReadLedgerSkipsMalformedLine: a line truncated mid-write is skipped and
// the others are read; a missing ledger is (nil, false, nil).
func TestReadLedgerSkipsMalformedLine(t *testing.T) {
	cache := t.TempDir()
	body := `{"session_id":"s1","entity_id":"W-0001","harness":"claude-code","ts":"2026-11-01T10:00:00Z"}
{"session_id":"s2","enti
{"session_id":"s3","agent_id":"a1","entity_id":"F-0002","harness":"claude-code","ts":"2026-11-01T11:00:00Z"}
`
	if err := os.WriteFile(filepath.Join(cache, "costbind.ndjson"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	binds, exists, err := readLedger(cache)
	if err != nil || !exists {
		t.Fatalf("readLedger: exists=%v err=%v", exists, err)
	}
	if len(binds) != 2 || binds[0].Session != "s1" || binds[1].Session != "s3" || binds[1].Agent != "a1" {
		t.Fatalf("binds = %+v, want s1 and s3 (agent a1)", binds)
	}

	binds, exists, err = readLedger(t.TempDir())
	if err != nil || exists || binds != nil {
		t.Fatalf("missing ledger: binds=%v exists=%v err=%v, want nil/false/nil", binds, exists, err)
	}
}

// TestClaudeRootsMergesEnvAndDefaults (C16): CLAUDE_CONFIG_DIR may list
// several dirs, comma-separated; the defaults are always scanned too, deduped
// (sessions are looked up by UUID, so an extra root is harmless).
func TestClaudeRootsMergesEnvAndDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claude, config := filepath.Join(home, ".claude"), filepath.Join(home, ".config", "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", "/a, /b ,,/a,"+claude)
	if got, want := ClaudeRoots(), []string{"/a", "/b", claude, config}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ClaudeRoots() = %v, want %v", got, want)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got, want := ClaudeRoots(), []string{config, claude}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ClaudeRoots() = %v, want %v", got, want)
	}
}

// TestBindWithoutRoleKeepsTheOldLine (F-0384 crit. 7): a bind with no role and
// no retro writes the same line as before the fields existed, and an old line
// reads with both empty.
func TestBindWithoutRoleKeepsTheOldLine(t *testing.T) {
	cache := t.TempDir()
	ts := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if err := AppendBind(cache, Bind{Session: "s1", Agent: "a1", Entity: "F-0002", Harness: HarnessClaudeCode, TS: ts}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(cache, "costbind.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	const old = `{"session_id":"s1","agent_id":"a1","entity_id":"F-0002","harness":"claude-code","ts":"2026-11-01T10:00:00Z"}` + "\n"
	if string(raw) != old {
		t.Fatalf("ledger line = %s, want the pre-F-0384 line %s", raw, old)
	}
	binds, _, err := readLedger(cache)
	if err != nil || len(binds) != 1 || binds[0].Role != "" || binds[0].Retro {
		t.Fatalf("binds = %+v, %v", binds, err)
	}
}

// TestAppendOpenedSeparateFile (F-0486 crit. 1): the session that opens a card
// is recorded in its own file, which the cost ledger never reads — so a card's
// cost is the same with or without the mark, under this binary or an older one.
func TestAppendOpenedSeparateFile(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(1), id: "m1", req: "r1", c: cc(1, 2, 3, 4)},
		tmsg{ts: at(3), id: "m3", req: "r3", c: cc(5, 6, 7, 8)})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(2))
	before := compute(t, in, "F-0001")

	if err := AppendOpened(in.CacheDir, Bind{Session: "s", Entity: "F-0001", Harness: HarnessClaudeCode, TS: at(0)}); err != nil {
		t.Fatal(err)
	}
	after := compute(t, in, "F-0001")
	if after.Counters != before.Counters || after.Unattributed != before.Unattributed || after.Messages != before.Messages {
		t.Fatalf("cost changed by the opening mark: before %+v, after %+v", before, after)
	}
	binds, _, err := readLedger(in.CacheDir)
	if err != nil || len(binds) != 1 {
		t.Fatalf("ledger = %+v (%v), want the one bind only", binds, err)
	}
	opened, err := ReadOpened(in.CacheDir)
	if err != nil || len(opened) != 1 || opened[0].Session != "s" || opened[0].Entity != "F-0001" {
		t.Fatalf("ReadOpened = %+v (%v), want s opened F-0001", opened, err)
	}
}

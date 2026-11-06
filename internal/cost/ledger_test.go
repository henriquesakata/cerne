package cost

import (
	"os"
	"path/filepath"
	"testing"
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

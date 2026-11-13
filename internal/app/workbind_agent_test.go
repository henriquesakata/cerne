package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
)

func readBindingFile(t *testing.T, cacheDir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cacheDir, sessionsSubdir, name))
	if err != nil {
		t.Fatalf("binding %s: %v", name, err)
	}
	return string(raw)
}

// TestBindSessionAgentWritesAgentFile (F-0376 crit. 1/2): a subagent's bind
// lands on <session>.<agent> and leaves the main thread's <session> file as it
// was; a bind with no agent writes <session>, as before.
func TestBindSessionAgentWritesAgentFile(t *testing.T) {
	cacheDir := t.TempDir()
	if err := BindSessionAgent(cacheDir, "sess-1", "", cost.HarnessClaudeCode, "W-0001"); err != nil {
		t.Fatal(err)
	}
	if err := BindSessionAgent(cacheDir, "sess-1", "a17d7f8b", cost.HarnessClaudeCode, "F-0002"); err != nil {
		t.Fatal(err)
	}
	if got := readBindingFile(t, cacheDir, "sess-1"); got != "W-0001\n" {
		t.Fatalf("main binding = %q, want W-0001 untouched by the subagent", got)
	}
	if got := readBindingFile(t, cacheDir, "sess-1.a17d7f8b"); got != "F-0002\n" {
		t.Fatalf("agent binding = %q, want F-0002", got)
	}
	_, binds := readCostLedger(t, cacheDir)
	if len(binds) != 2 || binds[1].Session != "sess-1" || binds[1].Agent != "a17d7f8b" || binds[1].Entity != "F-0002" {
		t.Fatalf("ledger = %+v, want the agent on the second line (crit. 5)", binds)
	}
}

// TestCurrentBindingAgentFallsBackToMain (F-0376 crit. 3/4): the main thread
// ("" agent) reads only its own file; a subagent reads its own, and without a
// live one, the main thread's. A stale agent file self-heals and falls back.
func TestCurrentBindingAgentFallsBackToMain(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0001", domain.WorkActive)
	bindTestWork(t, repoDir, "W-0002", domain.WorkActive)
	bindTestWork(t, repoDir, "W-0003", domain.WorkDone)

	if err := BindSessionAgent(cacheDir, "s", "dev", "", "W-0002"); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := CurrentBinding(cacheDir, repoDir, "s", ""); err != nil || ok {
		t.Fatalf("main = (%q, %v, %v), want absent: a subagent's binding is not the main thread's", id, ok, err)
	}
	if id, ok, err := CurrentBinding(cacheDir, repoDir, "s", "dev"); err != nil || !ok || id != "W-0002" {
		t.Fatalf("agent = (%q, %v, %v), want W-0002", id, ok, err)
	}

	if err := BindSession(cacheDir, "s", "W-0001"); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := CurrentBinding(cacheDir, repoDir, "s", "other"); err != nil || !ok || id != "W-0001" {
		t.Fatalf("unbound agent = (%q, %v, %v), want the main W-0001", id, ok, err)
	}

	if err := BindSessionAgent(cacheDir, "s", "dead", "", "W-0003"); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := CurrentBinding(cacheDir, repoDir, "s", "dead"); err != nil || !ok || id != "W-0001" {
		t.Fatalf("stale agent = (%q, %v, %v), want the main W-0001", id, ok, err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, sessionsSubdir, "s.dead")); !os.IsNotExist(err) {
		t.Fatalf("stale agent binding not removed: %v", err)
	}
}

// TestCurrentBindingLegacyFile (F-0376 crit. 6): a <session> file written by
// hand in the pre-F-0376 format is the main thread's binding, no migration.
func TestCurrentBindingLegacyFile(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0001", domain.WorkActive)
	dir := filepath.Join(cacheDir, sessionsSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const sid = "6efc11f8-fc53-4f9b-b858-565f7b6acf95"
	if err := os.WriteFile(filepath.Join(dir, sid), []byte("W-0001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"", "a17d7f8bcc197381c"} {
		if id, ok, err := CurrentBinding(cacheDir, repoDir, sid, agent); err != nil || !ok || id != "W-0001" {
			t.Fatalf("agent %q: (%q, %v, %v), want W-0001", agent, id, ok, err)
		}
	}
	got := SessionSignals(cacheDir)
	if len(got) != 1 || got[0].Session != sid || got[0].Agent != "" {
		t.Fatalf("signals = %+v, want the main session, no agent", got)
	}
}

// TestBindingKeyRejectsDots (F-0376): neither piece of <session>.<agent> may
// carry a dot or a separator, or be empty/dot-dot — the key must split back
// into exactly (session, agent).
func TestBindingKeyRejectsDots(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	for _, tc := range []struct{ sess, agent string }{
		{"a.b", ""}, {"..", ""}, {".", ""},
		{"s", "."}, {"s", ".."}, {"s", "x.y"}, {"s", "x/y"}, {"s", `x\y`},
	} {
		if err := BindSessionAgent(cacheDir, tc.sess, tc.agent, "", "W-0001"); !errors.Is(err, ErrUnsafeSessionID) {
			t.Errorf("BindSessionAgent(%q, %q) = %v, want ErrUnsafeSessionID", tc.sess, tc.agent, err)
		}
		if _, _, err := CurrentBinding(cacheDir, repoDir, tc.sess, tc.agent); !errors.Is(err, ErrUnsafeSessionID) {
			t.Errorf("CurrentBinding(%q, %q) = %v, want ErrUnsafeSessionID", tc.sess, tc.agent, err)
		}
		if err := TouchBinding(cacheDir, tc.sess, tc.agent); !errors.Is(err, ErrUnsafeSessionID) {
			t.Errorf("TouchBinding(%q, %q) = %v, want ErrUnsafeSessionID", tc.sess, tc.agent, err)
		}
	}
}

// TestTouchBindingAgentKey (F-0376 crit. 5): a subagent's sign of life
// refreshes its own file only; SessionSignals reports it as (session, agent).
func TestTouchBindingAgentKey(t *testing.T) {
	cacheDir := t.TempDir()
	mainPath, old := oldBinding(t, cacheDir, "s", "W-0001")
	if err := BindSessionAgent(cacheDir, "s", "dev", "", "F-0002"); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(cacheDir, sessionsSubdir, "s.dev")
	if err := os.Chtimes(agentPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := TouchBinding(cacheDir, "s", "dev"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(mainPath); err != nil || !fi.ModTime().Equal(old) {
		t.Fatalf("main binding moved on a subagent's signal: %v", err)
	}
	if fi, err := os.Stat(agentPath); err != nil || fi.ModTime().Equal(old) {
		t.Fatalf("agent mtime not refreshed: %v", err)
	}
	byKey := map[string]SessionSignal{}
	for _, s := range SessionSignals(cacheDir) {
		byKey[s.Session+"/"+s.Agent] = s
	}
	if s, ok := byKey["s/dev"]; !ok || s.Entity != "F-0002" || s.LastSignal.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("signals = %+v, want s/dev F-0002 fresh", byKey)
	}
	if s, ok := byKey["s/"]; !ok || s.Entity != "W-0001" {
		t.Fatalf("signals = %+v, want s/ W-0001", byKey)
	}
}

// TestLiveBindingsMainFirst (F-0376 critic round 1, item 1): a live main
// thread binding wins over live subagent bindings, so a subagent's lingering
// in_development feature does not silence the TL's commit trailer; subagent
// bindings count only when no main binding is live.
func TestLiveBindingsMainFirst(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0001", domain.WorkActive)
	bindTestWork(t, repoDir, "W-0002", domain.WorkActive)
	if err := BindSessionAgent(cacheDir, "tl", "dev", "", "W-0002"); err != nil {
		t.Fatal(err)
	}
	if got, err := LiveBindings(cacheDir, repoDir); err != nil || len(got) != 1 || got[0] != "W-0002" {
		t.Fatalf("subagent only: %v %v, want [W-0002]", got, err)
	}
	if err := BindSession(cacheDir, "tl", "W-0001"); err != nil {
		t.Fatal(err)
	}
	if got, err := LiveBindings(cacheDir, repoDir); err != nil || len(got) != 1 || got[0] != "W-0001" {
		t.Fatalf("main + subagent: %v %v, want [W-0001]", got, err)
	}
	// An id first met in a subagent file (sorted before) still counts for a
	// main thread that holds it too: two main ids stay ambiguous.
	if err := BindSessionAgent(cacheDir, "aa", "dev", "", "W-0002"); err != nil {
		t.Fatal(err)
	}
	if err := BindSession(cacheDir, "zz", "W-0002"); err != nil {
		t.Fatal(err)
	}
	if got, err := LiveBindings(cacheDir, repoDir); err != nil || len(got) != 2 {
		t.Fatalf("two main ids: %v %v, want both", got, err)
	}
}

package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStaleDecisionWindowDays (F-0385): stale_decision_days is a top-level
// .cernerc key read like stale_ready_days — absent or 0 is the 180-day
// default, N>0 is N — and a rewrite keeps it.
func TestStaleDecisionWindowDays(t *testing.T) {
	for _, tc := range []struct {
		name, toml string
		want       int
	}{
		{"absent defaults to 180", "", 180},
		{"zero defaults to 180", "stale_decision_days = 0\n", 180},
		{"explicit 30", "stale_decision_days = 30\n", 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"+tc.toml)
			c, _, err := LoadConfig(dir)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if got := c.StaleDecisionWindowDays(); got != tc.want {
				t.Errorf("StaleDecisionWindowDays() = %d, want %d", got, tc.want)
			}
		})
	}

	dir := t.TempDir()
	writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\nstale_decision_days = 30\n")
	c, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := SaveConfig(dir, c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".cernerc")); !strings.Contains(string(data), "stale_decision_days = 30") {
		t.Fatalf("stale_decision_days lost on rewrite:\n%s", data)
	}
}

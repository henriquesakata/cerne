package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStaleDecisionDays (F-0385 criterion 8): N of the stale-decision lint is
// the registry key lint.stale_decision_days, default 180. A top-level
// stale_decision_days in .cernerc is the same setting, read when [lint] has
// none; when both exist [lint] wins. A value that is no positive integer is
// skipped, never fatal.
func TestStaleDecisionDays(t *testing.T) {
	for _, tc := range []struct {
		name, project string
		want          int
		origin        string
	}{
		{"absent defaults to 180", "", 180, OriginDefault},
		{"top-level key", "stale_decision_days = 30\n", 30, ScopeProject},
		{"[lint] key", "[lint]\nstale_decision_days = 45\n", 45, ScopeProject},
		{"[lint] wins over top-level", "stale_decision_days = 30\n[lint]\nstale_decision_days = 45\n", 45, ScopeProject},
		{"zero is skipped", "[lint]\nstale_decision_days = 0\n", 180, OriginDefault},
		{"a string is skipped", "[lint]\nstale_decision_days = \"x\"\n", 180, OriginDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, tc.project, "", false)
			if got := StaleDecisionDays(projDir); got != tc.want {
				t.Errorf("StaleDecisionDays = %d, want %d", got, tc.want)
			}
			if s := findSetting(t, ResolveSettings(projDir), "lint.stale_decision_days"); s.Origin != tc.origin {
				t.Errorf("origin = %s, want %s (%+v)", s.Origin, tc.origin, s)
			}
		})
	}
	t.Run("no project", func(t *testing.T) {
		projDir, _ := settingsFixture(t, "", "", true)
		if got := StaleDecisionDays(projDir); got != 180 {
			t.Errorf("StaleDecisionDays = %d, want 180", got)
		}
	})
}

// TestSetStaleDecisionDays: the int key takes a positive whole number from
// the CLI literal and from a JSON number, writes [lint] stale_decision_days,
// and refuses anything else with no write.
func TestSetStaleDecisionDays(t *testing.T) {
	const id = "lint.stale_decision_days"
	projDir, _ := settingsFixture(t, "", "", false)
	rc := filepath.Join(projDir, ".cernerc")
	before, _ := os.ReadFile(rc)
	for _, raw := range []string{"abc", "0", "-3", "1.5", ""} {
		if err := SetSetting(projDir, id, ScopeProject, raw); !errors.Is(err, ErrInvalidSettingValue) {
			t.Errorf("SetSetting %q: err = %v, want ErrInvalidSettingValue", raw, err)
		}
	}
	if err := SetSettingValue(projDir, id, ScopeProject, 1.5); !errors.Is(err, ErrInvalidSettingValue) {
		t.Errorf("SetSettingValue 1.5: err = %v, want ErrInvalidSettingValue", err)
	}
	if after, _ := os.ReadFile(rc); string(after) != string(before) {
		t.Fatalf("a refused write changed .cernerc:\n%s", after)
	}

	if err := SetSetting(projDir, id, ScopeProject, "45"); err != nil {
		t.Fatalf("SetSetting 45: %v", err)
	}
	if data, _ := os.ReadFile(rc); !strings.Contains(string(data), "[lint]\nstale_decision_days = 45\n") {
		t.Fatalf(".cernerc lacks [lint] stale_decision_days = 45:\n%s", data)
	}
	if err := SetSettingValue(projDir, id, ScopeProject, float64(60)); err != nil { // a JSON number
		t.Fatalf("SetSettingValue 60: %v", err)
	}
	if got := StaleDecisionDays(projDir); got != 60 {
		t.Fatalf("StaleDecisionDays = %d, want 60", got)
	}
}

// TestStaleDecisionDaysSurvivesRewrite: SaveConfig keeps the top-level key.
func TestStaleDecisionDaysSurvivesRewrite(t *testing.T) {
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

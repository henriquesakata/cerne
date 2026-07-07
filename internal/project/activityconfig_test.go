package project

import "testing"

// TestActivityConfig (F-0146 crit. 7): absent [activity] means auto-registration
// enabled; only an explicit `auto = false` opts out.
func TestActivityConfig(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want bool
	}{
		{"absent block defaults on", "", true},
		{"explicit true", "[activity]\nauto = true\n", true},
		{"explicit false opts out", "[activity]\nauto = false\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"+tc.toml)
			c, _, err := LoadConfig(dir)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if got := c.Activity.AutoEnabled(); got != tc.want {
				t.Errorf("AutoEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSaveConfigPreservesActivity: a round-trip through SaveConfig keeps the
// explicit [activity] opt-out (the UI's LLM switcher rewrites .cernerc).
func TestSaveConfigPreservesActivity(t *testing.T) {
	dir := t.TempDir()
	off := false
	if err := SaveConfig(dir, Config{ProjectID: "p", Backend: "b", Activity: ActivityConfig{Auto: &off}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	c, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if c.Activity.AutoEnabled() {
		t.Fatalf("opt-out lost in SaveConfig round-trip")
	}
}

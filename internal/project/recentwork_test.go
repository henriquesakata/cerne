package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecentWorkWindowDays (F-0319 crit. 7, 8): absent or 0 → 14 days; N>0 → N.
func TestRecentWorkWindowDays(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want int
	}{
		{"absent defaults to 14", "", 14},
		{"zero defaults to 14", "recent_work_days = 0\n", 14},
		{"explicit 7", "recent_work_days = 7\n", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"+tc.toml)
			c, _, err := LoadConfig(dir)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if got := c.RecentWorkWindowDays(); got != tc.want {
				t.Errorf("RecentWorkWindowDays() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestRecentWorkDaysSurvivesRewrite (F-0319 crit. 9): load + SaveConfig keeps the key.
func TestRecentWorkDaysSurvivesRewrite(t *testing.T) {
	dir := t.TempDir()
	writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\nrecent_work_days = 7\n")
	c, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := SaveConfig(dir, c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".cernerc"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "recent_work_days = 7") {
		t.Fatalf("recent_work_days lost on rewrite:\n%s", data)
	}
}

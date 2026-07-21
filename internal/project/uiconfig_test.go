package project

import "testing"

// TestUIConfigMindmapDefault pins F-0187 criterion 5: an absent [ui] section
// (or absent mindmap key) means the mindmap tab is enabled — zero migration
// for existing projects.
func TestUIConfigMindmapDefault(t *testing.T) {
	dir := t.TempDir()
	writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n")

	c, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !c.UI.MindmapEnabled() {
		t.Errorf("MindmapEnabled() = false with absent [ui], want true (default on)")
	}
}

// TestUIConfigMindmapExplicit pins F-0187 criterion 4: [ui] mindmap = false
// disables the tab; an explicit true keeps it enabled.
func TestUIConfigMindmapExplicit(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want bool
	}{
		{"explicit false", "[ui]\nmindmap = false\n", false},
		{"explicit true", "[ui]\nmindmap = true\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCernerc(t, dir, "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"+tc.toml)

			c, _, err := LoadConfig(dir)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if got := c.UI.MindmapEnabled(); got != tc.want {
				t.Errorf("MindmapEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

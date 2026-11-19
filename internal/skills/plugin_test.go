package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPluginLoadsEmbeddedSkills (F-0454 criterion 2): the Claude Code plugin
// at the repo root loads its skills from this package's directory — the very
// files the binary embeds — so there is one source and no copy to drift. Every
// canonical skill, SKILL.md and references, sits where the plugin scans.
func TestPluginLoadsEmbeddedSkills(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("plugin manifest: %v", err)
	}
	var m struct {
		Skills string `json:"skills"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	loaded, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(m.Skills)))
	if err != nil {
		t.Fatal(err)
	}
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	if m.Skills == "" || loaded != here {
		t.Fatalf("plugin skills = %q (%s), want the embedded skills directory %s", m.Skills, loaded, here)
	}
	for _, s := range All() {
		want := map[string]string{s.RelPath: s.Content}
		for _, r := range s.References {
			want[s.Name+"/"+r.RelPath] = r.Content
		}
		for rel, content := range want {
			got, err := os.ReadFile(filepath.Join(loaded, filepath.FromSlash(rel)))
			if err != nil || string(got) != content {
				t.Errorf("%s: not the embedded file at the plugin's skills path (%v)", rel, err)
			}
		}
	}
}

package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates path (and parents) with content, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func settingsWiring(script string) string {
	return `{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/` + script + `\""}]}]}}`
}

func TestReadLayersMergesUserProjectLocal(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), settingsWiring("from-user.sh"))
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), settingsWiring("from-project.sh"))
	writeFile(t, filepath.Join(proj, ".claude", "settings.local.json"), settingsWiring("from-local.sh"))

	ls := readLayers(proj, home)
	if len(ls.layers) != 3 {
		t.Fatalf("layers = %d, want 3", len(ls.layers))
	}
	for _, l := range ls.layers {
		if !l.Readable {
			t.Errorf("layer %s unreadable: %s", l.Path, l.Error)
		}
	}
	wantOrigin := map[string]string{
		"from-user.sh":    OriginUser,
		"from-project.sh": OriginProject,
		"from-local.sh":   OriginLocal,
	}
	for script, origin := range wantOrigin {
		h, ok := ls.wiringFor(script)
		if !ok {
			t.Fatalf("script %s not wired", script)
		}
		if h.Origin != origin {
			t.Errorf("script %s origin = %q, want %q", script, h.Origin, origin)
		}
		if h.Event != "PostToolUse" {
			t.Errorf("script %s event = %q, want PostToolUse", script, h.Event)
		}
	}
}

func TestReadLayersMalformedIsMarkedNotFatal(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), "{not json")
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), settingsWiring("ok.sh"))

	ls := readLayers(proj, home)
	var bad, good bool
	for _, l := range ls.layers {
		if filepath.Base(filepath.Dir(l.Path)) == ".claude" && !l.Readable && l.Error != "" && filepath.HasPrefix(l.Path, proj) {
			bad = true
		}
	}
	if _, ok := ls.wiringFor("ok.sh"); ok {
		good = true
	}
	if !bad {
		t.Error("malformed project settings not marked unreadable with reason")
	}
	if !good {
		t.Error("healthy user layer not read despite sibling malformed layer")
	}
}

func TestReadLayersMissingHome(t *testing.T) {
	proj := t.TempDir()
	ls := readLayers(proj, "")
	// No layer may be fatal; user layer absent is reported, not invented.
	for _, l := range ls.layers {
		if l.Readable && l.Error != "" {
			t.Errorf("layer %s readable but carries error %q", l.Path, l.Error)
		}
	}
	if _, ok := ls.wiringFor("anything.sh"); ok {
		t.Error("wiring found in empty project")
	}
}

func TestInspectNeverWrites(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), settingsWiring("x.sh"))
	writeFile(t, filepath.Join(proj, ".claude", "hooks", "x.sh"), "#!/bin/sh\n")
	before := treeState(t, proj, home)
	Inspect(Options{ProjDir: proj, Home: home})
	after := treeState(t, proj, home)
	if before != after {
		t.Errorf("Inspect mutated the tree:\nbefore: %s\nafter:  %s", before, after)
	}
}

// treeState fingerprints every file (path+size+content) under the roots.
func treeState(t *testing.T, roots ...string) string {
	t.Helper()
	out := ""
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out += path + "|" + string(b) + "\n"
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

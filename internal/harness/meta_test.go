package harness

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCerneClassConfigDefaultsAndValues(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".cernerc"), "project_id = \"teste\"\nbackend = \"git@example.com:x/docs.git\"\npin_branch = \"main\"\n")
	writeFile(t, filepath.Join(proj, "docs.lock"), `{"version":1,"backend":"git@example.com:x/docs.git","snapshot":"abc123"}`)

	inv := Inspect(Options{ProjDir: proj})

	if it, ok := findItem(inv, "cerne", "backend"); !ok || !strings.Contains(it.Detail, "example.com") {
		t.Errorf("backend item = %+v, want remote in detail", it)
	}
	if it, ok := findItem(inv, "cerne", "docs.lock"); !ok || !strings.Contains(it.Detail, "abc123") {
		t.Errorf("docs.lock item = %+v, want pin in detail", it)
	}
	// Present key: the read value; absent key: default marked as such.
	if it, ok := findItem(inv, "cerne", "pin_branch"); !ok || !strings.Contains(it.Detail, "main") || strings.Contains(it.Detail, "default") {
		t.Errorf("pin_branch = %+v, want value main without default marker", it)
	}
	if it, ok := findItem(inv, "cerne", "compile_on_push"); !ok || !strings.Contains(it.Detail, "default") {
		t.Errorf("compile_on_push = %+v, want default marker", it)
	}
	if it, ok := findItem(inv, "cerne", ".docs"); !ok || it.State != StateMissing {
		t.Errorf(".docs not materialized = %+v, want missing", it)
	}
}

func TestCerneClassWithoutCernerc(t *testing.T) {
	proj := t.TempDir()
	inv := Inspect(Options{ProjDir: proj})
	// No .cernerc: the layer is marked, the inventory still renders.
	var marked bool
	for _, l := range inv.Layers {
		if strings.HasSuffix(l.Path, ".cernerc") && !l.Readable {
			marked = true
		}
	}
	if !marked {
		t.Error("missing .cernerc not reported as unreadable layer")
	}
}

func TestConfigOriginWithProjectNestedUnderHome(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "dev", "proj")
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), settingsWiring("a.sh"))
	writeFile(t, filepath.Join(proj, ".claude", "settings.local.json"), settingsWiring("b.sh"))
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), settingsWiring("c.sh"))

	inv := Inspect(Options{ProjDir: proj, Home: home})
	var got []string
	for _, c := range inv.Classes {
		if c.Name != "config" {
			continue
		}
		for _, it := range c.Items {
			if filepath.Base(it.Path) == "settings.json" || filepath.Base(it.Path) == "settings.local.json" {
				got = append(got, it.Origin)
			}
		}
	}
	want := map[string]int{OriginUser: 1, OriginProject: 1, OriginLocal: 1}
	for _, o := range got {
		want[o]--
	}
	for o, n := range want {
		if n != 0 {
			t.Errorf("config origins wrong (project nested under home): got %v, missing/excess %s", got, o)
		}
	}
}

func TestContextFilesListed(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(proj, "CLAUDE.md"), "# projeto")
	writeFile(t, filepath.Join(proj, "AGENTS.md"), "# agentes")
	writeFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), "# global")

	inv := Inspect(Options{ProjDir: proj, Home: home})

	it, ok := findItem(inv, "context", "CLAUDE.md")
	if !ok || it.State != StateUnmanaged || it.Origin != OriginProject {
		t.Errorf("project CLAUDE.md = %+v, want unmanaged/project", it)
	}
	if _, ok := findItem(inv, "context", "AGENTS.md"); !ok {
		t.Error("AGENTS.md absent")
	}
	it, ok = findItem(inv, "context", "~/.claude/CLAUDE.md")
	if !ok || it.Origin != OriginUser || !it.OutsideRepo {
		t.Errorf("user CLAUDE.md = %+v, want user origin outside repo", it)
	}
}

func TestPluginsEnabledAndBroken(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), `{
		"version": 2,
		"plugins": {
			"context-mode@context-mode": [{"scope": "user", "version": "1.0.79"}],
			"desligado@mkt": [{"scope": "user", "version": "0.1.0"}]
		}
	}`)
	writeFile(t, filepath.Join(home, ".claude", "settings.json"),
		`{"enabledPlugins": {"context-mode@context-mode": true, "fantasma@mkt": true, "desligado@mkt": false}}`)

	inv := Inspect(Options{ProjDir: proj, Home: home})

	it, ok := findItem(inv, "plugins", "context-mode@context-mode")
	if !ok || it.State != StateConfigured || it.Origin != OriginPlugin || !it.OutsideRepo {
		t.Errorf("enabled installed plugin = %+v, want configured/plugin/outside", it)
	}
	if !strings.Contains(it.Detail, "1.0.79") {
		t.Errorf("plugin version not surfaced: %q", it.Detail)
	}
	it, ok = findItem(inv, "plugins", "fantasma@mkt")
	if !ok || it.State != StateBroken {
		t.Errorf("enabled but not installed plugin = %+v, want broken", it)
	}
	if _, ok := findItem(inv, "plugins", "desligado@mkt"); ok {
		t.Error("disabled plugin listed — inventory is of what the project uses")
	}
}

func TestProviderMatrixForDetectedProviders(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".cursor", "rules.md"), "x")

	inv := Inspect(Options{ProjDir: proj})

	var covered, degraded int
	for _, c := range inv.Classes {
		if c.Name != "providers" {
			continue
		}
		for _, it := range c.Items {
			if !strings.HasPrefix(it.Name, "cursor: ") {
				t.Errorf("unexpected provider item %q with only .cursor present", it.Name)
			}
			switch it.State {
			case StateConfigured:
				covered++
			case StateDegraded:
				degraded++
			default:
				t.Errorf("provider row state %q, want configured/degraded", it.State)
			}
		}
	}
	if covered == 0 || degraded == 0 {
		t.Errorf("cursor matrix covered=%d degraded=%d, want both > 0", covered, degraded)
	}
}

func TestNoProviderDirsNoProviderItems(t *testing.T) {
	proj := t.TempDir()
	inv := Inspect(Options{ProjDir: proj})
	for _, c := range inv.Classes {
		if c.Name == "providers" && len(c.Items) > 0 {
			t.Errorf("provider items without provider dirs: %+v", c.Items)
		}
	}
}

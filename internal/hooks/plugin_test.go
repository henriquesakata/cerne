package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// pluginDir is the repository's Claude Code plugin manifest folder (F-0454):
// the repo root is the plugin, so the tests read the real files.
const pluginDir = "../../.claude-plugin"

// pluginEntry is the plugin's one entry script, as the manifest calls it.
const pluginEntry = `bash "${CLAUDE_PLUGIN_ROOT}/.claude-plugin/run.sh"`

// wire is one hook wiring, normalized: the script it ends up running.
type wire struct {
	Event, Matcher, Script string
	Timeout                int
}

var scriptRe = regexp.MustCompile(`[A-Za-z0-9._-]+\.sh`)

// hooksMap is a Claude Code hooks map: settings.json "hooks", or the plugin
// manifest's inline "hooks" (same shape).
type hooksMap map[string][]struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	} `json:"hooks"`
}

type hooksDoc struct {
	Hooks hooksMap `json:"hooks"`
}

// wiresOf normalizes a hooks map into sorted wires.
func wiresOf(t *testing.T, hooksMap hooksMap) []wire {
	t.Helper()
	var out []wire
	for event, entries := range hooksMap {
		for _, e := range entries {
			for _, h := range e.Hooks {
				m := scriptRe.FindAllString(h.Command, -1)
				if h.Type != "command" || len(m) == 0 {
					t.Fatalf("%s: unexpected hook %+v", event, h)
				}
				out = append(out, wire{Event: event, Matcher: e.Matcher, Script: m[len(m)-1], Timeout: h.Timeout})
			}
		}
	}
	slices.SortFunc(out, func(a, b wire) int {
		return strings.Compare(a.Event+a.Matcher+a.Script, b.Event+b.Matcher+b.Script)
	})
	return out
}

func readPluginManifest(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json"))
	if err != nil {
		t.Fatalf("plugin manifest: %v", err)
	}
	return raw
}

// TestPluginHooksMatchInstall (F-0454 criterion 3): the plugin wires exactly
// the hooks `cerne hook install` writes into an empty settings.json — same
// events, matchers, timeouts and scripts — every one through the entry script,
// and every script it names is an embedded asset.
func TestPluginHooksMatchInstall(t *testing.T) {
	installed, _, err := mergeSettings([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	var settings, plugin hooksDoc
	if err := json.Unmarshal(installed, &settings); err != nil {
		t.Fatal(err)
	}
	raw := readPluginManifest(t)
	if err := json.Unmarshal(raw, &plugin); err != nil {
		t.Fatalf("plugin manifest: %v", err)
	}
	want, got := wiresOf(t, settings.Hooks), wiresOf(t, plugin.Hooks)
	if !slices.Equal(got, want) {
		t.Fatalf("plugin hooks differ from `cerne hook install`:\n got %+v\nwant %+v", got, want)
	}
	for _, entries := range plugin.Hooks {
		for _, e := range entries {
			for _, h := range e.Hooks {
				if !strings.HasPrefix(h.Command, pluginEntry+" hook ") {
					t.Errorf("hook %q does not run through %s", h.Command, pluginEntry)
				}
			}
		}
	}
	for _, w := range got {
		if _, err := files.ReadFile("assets/" + w.Script); err != nil {
			t.Errorf("plugin names %s, which is not an embedded asset", w.Script)
		}
	}
}

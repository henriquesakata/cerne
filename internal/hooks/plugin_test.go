package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
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

// TestPluginMCPRunsCerneDirectly (F-0454 criterion 6): the plugin's MCP server
// is the cerne binary itself, against the session's project — never a bash
// wrapper, which native Windows starts without a shell.
func TestPluginMCPRunsCerneDirectly(t *testing.T) {
	var m struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(readPluginManifest(t), &m); err != nil {
		t.Fatal(err)
	}
	s, ok := m.MCPServers["cerne"]
	if want := []string{"mcp", "--dir", "${CLAUDE_PROJECT_DIR}"}; !ok || s.Command != "cerne" || !slices.Equal(s.Args, want) {
		t.Fatalf("mcpServers.cerne = %+v (present %v), want cerne %v", s, ok, want)
	}
}

// entryRun runs the plugin's entry script the way Claude Code runs a plugin
// hook: bash, the plugin root and project dir in the environment, a bare
// system PATH and home (so only a cerne placed there is found).
func entryRun(t *testing.T, home, proj string, args ...string) (stdout string, code int) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{filepath.Join(root, ".claude-plugin", "run.sh")}, args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "CLAUDE_PLUGIN_ROOT=" + root, "CLAUDE_PROJECT_DIR=" + proj}
	cmd.Stdin = strings.NewReader("{}")
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if errOut.Len() > 0 {
		t.Errorf("%v: unexpected stderr %q", args, errOut.String())
	}
	return out.String(), code
}

// loggingCerne puts a cerne in home's ~/go/bin that logs "<CERNE_PLUGIN> <args>"
// per call, and returns the log path.
func loggingCerne(t *testing.T, home string) string {
	t.Helper()
	bin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(home, "calls.log")
	stub := "#!/bin/sh\nprintf '%s %s\\n' \"${CERNE_PLUGIN:-unset}\" \"$*\" >> '" + log + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "cerne"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

func calls(t *testing.T, log string) string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(raw)
}

// TestPluginEntryScript (F-0454 criteria 4 and 5): the entry script runs the
// embedded hook only in a cerne project that does not carry its own copy, with
// CERNE_PLUGIN=1 so the binary knows; it is silent everywhere else; and with no
// cerne at all, SessionStart says how to install it, in any project.
func TestPluginEntryScript(t *testing.T) {
	const reminder = "cerne-register-reminder.sh"
	cerneProj := func(t *testing.T) string {
		p := t.TempDir()
		if err := os.WriteFile(filepath.Join(p, ".cernerc"), []byte("project_id = \"x\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("not a cerne project is silent", func(t *testing.T) {
		home := t.TempDir()
		log := loggingCerne(t, home)
		for _, a := range Assets() {
			if out, code := entryRun(t, home, t.TempDir(), "hook", a.Script); out != "" || code != 0 {
				t.Errorf("%s: out %q code %d, want silence", a.Script, out, code)
			}
		}
		if c := calls(t, log); c != "" {
			t.Fatalf("cerne ran outside a cerne project: %q", c)
		}
	})

	t.Run("the project's own install wins", func(t *testing.T) {
		home := t.TempDir()
		log := loggingCerne(t, home)
		proj := cerneProj(t)
		if _, err := Install(proj); err != nil {
			t.Fatal(err)
		}
		for _, a := range Assets() {
			if out, code := entryRun(t, home, proj, "hook", a.Script); out != "" || code != 0 {
				t.Errorf("%s: out %q code %d, want the plugin to step aside", a.Script, out, code)
			}
		}
		if c := calls(t, log); c != "" {
			t.Fatalf("the plugin's copy fired next to the project's: %q", c)
		}
	})

	t.Run("a cerne project without its own copy runs the embedded hook", func(t *testing.T) {
		home := t.TempDir()
		log := loggingCerne(t, home)
		proj := cerneProj(t)
		out, code := entryRun(t, home, proj, "hook", reminder)
		if code != 0 || !strings.Contains(out, "cerne project rules") {
			t.Fatalf("out %q code %d, want the session rules", out, code)
		}
		if c := calls(t, log); !strings.Contains(c, "1 hook channel --point session-start") {
			t.Fatalf("calls %q, want the session-start call with CERNE_PLUGIN=1", c)
		}
		if out, code := entryRun(t, home, proj, "hook", "../../etc/passwd"); out != "" || code != 0 {
			t.Fatalf("a name outside the assets ran: %q %d", out, code)
		}
	})

	t.Run("no cerne: SessionStart hints the install, the rest is silent", func(t *testing.T) {
		for _, dir := range []string{"/usr/local/bin", "/opt/homebrew/bin"} {
			if _, err := os.Stat(filepath.Join(dir, "cerne")); err == nil {
				t.Skipf("a cerne in %s is on the entry script's PATH fallback (F-0428); CI runs this case", dir)
			}
		}
		home := t.TempDir()
		for _, proj := range []string{t.TempDir(), cerneProj(t)} {
			out, code := entryRun(t, home, proj, "hook", reminder)
			for _, want := range []string{"brew install henriquesakata/tap/cerne", "install.ps1", "INSTALL.md"} {
				if code != 0 || !strings.Contains(out, want) {
					t.Fatalf("out %q code %d, want the install hint naming %s", out, code, want)
				}
			}
			if strings.Count(out, "\n") != 1 {
				t.Fatalf("hint %q, want one line", out)
			}
			if out, code := entryRun(t, home, proj, "hook", "cerne-work-gate.sh"); out != "" || code != 0 {
				t.Fatalf("work gate without cerne: %q %d, want silence", out, code)
			}
		}
	})
}

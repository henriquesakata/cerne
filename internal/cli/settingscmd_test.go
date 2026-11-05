package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
)

// settingsProject writes a backendless .cernerc (plus extra) and points the
// host scope at a private CERNE_CONFIG_DIR.
func settingsProject(t *testing.T, extra string) (projDir, hostDir string) {
	t.Helper()
	projDir, hostDir = t.TempDir(), t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	cernerc := "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n" + extra
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatal(err)
	}
	return projDir, hostDir
}

func settingsJSON(t *testing.T, projDir string) map[string]project.ResolvedSetting {
	t.Helper()
	out, err := execCerne(t, "settings", "list", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("settings list --json: %v\n%s", err, out)
	}
	var view project.SettingsView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	byID := map[string]project.ResolvedSetting{}
	for _, s := range view.Settings {
		byID[s.ID] = s
	}
	return byID
}

// TestSettingsList pins F-0314 criterion 1: one row per declared key with id,
// type, effective value, default and origin, then the file behind each scope.
func TestSettingsList(t *testing.T) {
	projDir, hostDir := settingsProject(t, "[mindmap]\nenabled = false\n")
	out, err := execCerne(t, "settings", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("settings list: %v\n%s", err, out)
	}
	lines := strings.Split(out, "\n")
	if got := strings.Fields(lines[0]); !slices.Equal(got, []string{"KEY", "TYPE", "VALUE", "DEFAULT", "ORIGIN"}) {
		t.Errorf("header = %q", lines[0])
	}
	rows := map[string]bool{}
	for _, l := range lines[1:] {
		rows[strings.Join(strings.Fields(l), " ")] = true
	}
	for _, want := range []string{
		"graph.enabled bool true true default",
		"mindmap.enabled bool false true project",
		"wiki.enabled bool true true default",
		"project: " + filepath.Join(projDir, ".cernerc"),
		"host: " + filepath.Join(hostDir, "settings.toml"),
	} {
		if !rows[want] {
			t.Errorf("list lacks row %q:\n%s", want, out)
		}
	}
}

// TestSettingsListJSON: --json is the endpoint's SettingsView shape.
func TestSettingsListJSON(t *testing.T) {
	projDir, _ := settingsProject(t, "[mindmap]\nenabled = false\n")
	byID := settingsJSON(t, projDir)
	if len(byID) != 3 {
		t.Fatalf("settings = %v, want the 3 declared keys", byID)
	}
	if m := byID["mindmap.enabled"]; m.Value != false || m.Origin != project.ScopeProject || m.Title == "" || m.Description == "" {
		t.Errorf("mindmap.enabled = %+v", m)
	}
}

// TestSettingsSetProjectOverridesHost pins criterion 2 through the CLI.
func TestSettingsSetProjectOverridesHost(t *testing.T) {
	projDir, _ := settingsProject(t, "")
	for _, args := range [][]string{
		{"settings", "set", "mindmap.enabled", "false", "--scope", "host", "--dir", projDir},
		{"settings", "set", "mindmap.enabled", "true", "--scope", "project", "--dir", projDir},
	} {
		if out, err := execCerne(t, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if m := settingsJSON(t, projDir)["mindmap.enabled"]; m.Value != true || m.Origin != project.ScopeProject {
		t.Errorf("mindmap.enabled = %v from %s, want true from project", m.Value, m.Origin)
	}
}

// TestSettingsSetWarnsWhenShadowed: a host write the project scope overrides
// says so, so the user knows why the value "did not take".
func TestSettingsSetWarnsWhenShadowed(t *testing.T) {
	projDir, _ := settingsProject(t, "[mindmap]\nenabled = true\n")
	out, err := execCerne(t, "settings", "set", "mindmap.enabled", "false", "--scope", "host", "--dir", projDir)
	if err != nil {
		t.Fatalf("set: %v\n%s", err, out)
	}
	if !strings.Contains(out, "note: overridden by project scope (effective: true)") {
		t.Errorf("output lacks the shadowed note:\n%s", out)
	}
}

// TestSettingsSetHostKeepsWorktreeClean pins criterion 5: a host write leaves
// the project's git worktree clean.
func TestSettingsSetHostKeepsWorktreeClean(t *testing.T) {
	projDir, _ := settingsProject(t, "")
	runGit(t, projDir, "init", "-q")
	runGit(t, projDir, "add", ".cernerc")
	runGit(t, projDir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")

	if out, err := execCerne(t, "settings", "set", "wiki.enabled", "false", "--scope", "host", "--dir", projDir); err != nil {
		t.Fatalf("set: %v\n%s", err, out)
	}
	if st := runGit(t, projDir, "status", "--porcelain"); st != "" {
		t.Errorf("worktree dirty after a host write:\n%s", st)
	}
	if w := settingsJSON(t, projDir)["wiki.enabled"]; w.Value != false || w.Origin != project.ScopeHost {
		t.Errorf("wiki.enabled = %v from %s, want false from host", w.Value, w.Origin)
	}
}

// TestSettingsSetRejectsInvalid pins criterion 6 through the CLI: the named
// sentinel, an English message naming the key and the accepted values, and
// the file untouched. --scope is required.
func TestSettingsSetRejectsInvalid(t *testing.T) {
	projDir, _ := settingsProject(t, "")
	path := filepath.Join(projDir, ".cernerc")
	before, _ := os.ReadFile(path)

	_, err := execCerne(t, "settings", "set", "wiki.enabled", "yes", "--scope", "project", "--dir", projDir)
	if !errors.Is(err, project.ErrInvalidSettingValue) {
		t.Fatalf("err = %v, want ErrInvalidSettingValue", err)
	}
	if !strings.Contains(err.Error(), "wiki.enabled") || !strings.Contains(err.Error(), "true or false") {
		t.Errorf("err = %q, want the key and the accepted values", err)
	}
	if _, err := execCerne(t, "settings", "set", "wiki.enabled", "false", "--dir", projDir); err == nil {
		t.Errorf("set without --scope succeeded, want it required")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf(".cernerc changed on refusal:\n%s", after)
	}
}

// TestSettingsGet: the bare value, or "origin<TAB>value" with --show-origin
// (git config --show-origin parity); an unknown key is a named error.
func TestSettingsGet(t *testing.T) {
	projDir, _ := settingsProject(t, "[mindmap]\nenabled = false\n")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"mindmap.enabled"}, "false\n"},
		{[]string{"mindmap.enabled", "--show-origin"}, "project\tfalse\n"},
		{[]string{"graph.enabled", "--show-origin"}, "default\ttrue\n"},
	} {
		out, err := execCerne(t, append([]string{"settings", "get", "--dir", projDir}, tc.args...)...)
		if err != nil || out != tc.want {
			t.Errorf("get %v = %q (%v), want %q", tc.args, out, err, tc.want)
		}
	}
	if _, err := execCerne(t, "settings", "get", "nope.enabled", "--dir", projDir); !errors.Is(err, project.ErrUnknownSetting) {
		t.Errorf("unknown key: err = %v, want ErrUnknownSetting", err)
	}
}

// TestSettingsUnset: removing the project value hands the key back to the
// host scope; --scope is required.
func TestSettingsUnset(t *testing.T) {
	projDir, hostDir := settingsProject(t, "[mindmap]\nenabled = true\n")
	if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte("[mindmap]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execCerne(t, "settings", "unset", "mindmap.enabled", "--dir", projDir); err == nil {
		t.Errorf("unset without --scope succeeded, want it required")
	}
	if out, err := execCerne(t, "settings", "unset", "mindmap.enabled", "--scope", "project", "--dir", projDir); err != nil {
		t.Fatalf("unset: %v\n%s", err, out)
	}
	if m := settingsJSON(t, projDir)["mindmap.enabled"]; m.Value != false || m.Origin != project.ScopeHost {
		t.Errorf("mindmap.enabled = %v from %s, want false from host", m.Value, m.Origin)
	}
}

// TestSettingsListSurvivesBadHostFile (critic C3): a bad stored value or a
// malformed host file prints the table plus one warning per problem, exit 0.
func TestSettingsListSurvivesBadHostFile(t *testing.T) {
	for _, tc := range []struct{ name, host, warn string }{
		{"bad value", "[wiki]\nenabled = \"no\"\n", `wiki.enabled = "no"`},
		{"malformed", "[x", "host scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projDir, hostDir := settingsProject(t, "[mindmap]\nenabled = false\n")
			if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte(tc.host), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := execCerne(t, "settings", "list", "--dir", projDir)
			if err != nil {
				t.Fatalf("list: %v\n%s", err, out)
			}
			var warnings []string
			for _, l := range strings.Split(out, "\n") {
				if strings.HasPrefix(l, "warning: ") {
					warnings = append(warnings, l)
				}
			}
			if !strings.Contains(out, "mindmap.enabled") || len(warnings) != 1 || !strings.Contains(warnings[0], tc.warn) ||
				!strings.Contains(warnings[0], filepath.Join(hostDir, "settings.toml")) {
				t.Errorf("want the table and one warning naming %q and the file:\n%s", tc.warn, out)
			}
		})
	}
}

// TestSettingsGetFailsOnlyForItsOwnBadValue: get exits non-zero only when the
// key's own value is invalid in the scope that would win, naming the file
// and the raw value; any other key (or a bad value the winner outranks) reads.
func TestSettingsGetFailsOnlyForItsOwnBadValue(t *testing.T) {
	projDir, hostDir := settingsProject(t, "[mindmap]\nenabled = \"no\"\n[wiki]\nenabled = true\n")
	if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte("[mindmap]\nenabled = false\n[wiki]\nenabled = \"off\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execCerne(t, "settings", "get", "mindmap.enabled", "--dir", projDir)
	if !errors.Is(err, project.ErrInvalidSettingValue) || !strings.Contains(err.Error(), filepath.Join(projDir, ".cernerc")) || !strings.Contains(err.Error(), `"no"`) {
		t.Errorf("get mindmap.enabled: err = %v, want ErrInvalidSettingValue naming .cernerc and \"no\"", err)
	}
	for key, want := range map[string]string{"graph.enabled": "true", "wiki.enabled": "true"} {
		out, err := execCerne(t, "settings", "get", key, "--dir", projDir)
		if err != nil || !strings.HasPrefix(out, want+"\n") {
			t.Errorf("get %s = %q (%v), want %s", key, out, err, want)
		}
	}
}

// TestSettingsWriteReportsItsOwnResult (critic C4): a write that landed
// reports success even when the re-read finds problems elsewhere; a scope
// file the re-read could not read becomes a warning, never the write's error.
func TestSettingsWriteReportsItsOwnResult(t *testing.T) {
	for _, tc := range []struct{ name, host, warn string }{
		{"bad host value", "[wiki]\nenabled = \"no\"\n", ""},
		{"malformed host file", "[x", "warning: host scope ignored"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projDir, hostDir := settingsProject(t, "")
			if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte(tc.host), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := execCerne(t, "settings", "set", "mindmap.enabled", "false", "--scope", "project", "--dir", projDir)
			if err != nil || !strings.Contains(out, "set mindmap.enabled = false in project scope") || !strings.Contains(out, tc.warn) {
				t.Errorf("set = %q (%v), want success plus %q", out, err, tc.warn)
			}
			if data, _ := os.ReadFile(filepath.Join(projDir, ".cernerc")); !strings.Contains(string(data), "enabled = false") {
				t.Errorf(".cernerc lacks the write:\n%s", data)
			}
			out, err = execCerne(t, "settings", "unset", "mindmap.enabled", "--scope", "project", "--dir", projDir)
			if err != nil || !strings.Contains(out, "unset mindmap.enabled from project scope") || !strings.Contains(out, tc.warn) {
				t.Errorf("unset = %q (%v), want success plus %q", out, err, tc.warn)
			}
		})
	}
}

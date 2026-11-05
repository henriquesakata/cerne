package project

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const baseCernerc = "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"

// settingsFixture writes a project (.cernerc = baseCernerc + project, or no
// .cernerc at all when noProject) and a host settings.toml (absent when host
// is empty) under a private CERNE_CONFIG_DIR, returning both dirs.
func settingsFixture(t *testing.T, project, host string, noProject bool) (projDir, hostDir string) {
	t.Helper()
	projDir, hostDir = t.TempDir(), t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	if !noProject {
		writeCernerc(t, projDir, baseCernerc+project)
	}
	if host != "" {
		if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte(host), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return projDir, hostDir
}

func findSetting(t *testing.T, v SettingsView, id string) ResolvedSetting {
	t.Helper()
	for _, s := range v.Settings {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("setting %s not in view %+v", id, v.Settings)
	return ResolvedSetting{}
}

// TestSettingsRegistryDeclaresCapabilities pins F-0314 criterion 9: the
// registry declares the wiki, graph and mindmap capability keys.
func TestSettingsRegistryDeclaresCapabilities(t *testing.T) {
	byID := map[string]Setting{}
	for _, s := range settingsRegistry {
		byID[s.ID] = s
	}
	for _, id := range []string{"graph.enabled", "mindmap.enabled", "wiki.enabled"} {
		s, ok := byID[id]
		if !ok {
			t.Errorf("registry lacks %s", id)
			continue
		}
		if s.Type != "bool" || s.Default != true || !slices.Equal(s.Scopes, []string{ScopeProject, ScopeHost}) || s.Title == "" || s.Description == "" {
			t.Errorf("%s = %+v, want bool, default true, scopes [project host], a title and a description", id, s)
		}
	}
}

// TestResolveSettings pins F-0314 criteria 2 and 3: project > host > default,
// with the winning scope reported, for a key set in zero, one and two scopes.
func TestResolveSettings(t *testing.T) {
	cases := []struct {
		name       string
		project    string
		host       string
		noProject  bool
		wantValue  bool
		wantOrigin string
	}{
		{"no scope", "", "", false, true, OriginDefault},
		{"host only", "", "[mindmap]\nenabled = false\n", false, false, ScopeHost},
		{"project only", "[mindmap]\nenabled = false\n", "", false, false, ScopeProject},
		{"project over host", "[mindmap]\nenabled = true\n", "[mindmap]\nenabled = false\n", false, true, ScopeProject},
		{"no .cernerc", "", "[mindmap]\nenabled = false\n", true, false, ScopeHost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projDir, hostDir := settingsFixture(t, tc.project, tc.host, tc.noProject)
			view := ResolveSettings(projDir)
			got := findSetting(t, view, "mindmap.enabled")
			if got.Value != tc.wantValue || got.Origin != tc.wantOrigin {
				t.Errorf("mindmap.enabled = %v from %s, want %v from %s", got.Value, got.Origin, tc.wantValue, tc.wantOrigin)
			}
			if other := findSetting(t, view, "graph.enabled"); other.Value != true || other.Origin != OriginDefault {
				t.Errorf("graph.enabled = %v from %s, want the default", other.Value, other.Origin)
			}
			if got := view.Files[ScopeHost]; got != filepath.Join(hostDir, "settings.toml") {
				t.Errorf("files[host] = %q", got)
			}
			wantProj := filepath.Join(projDir, ".cernerc")
			if tc.noProject {
				wantProj = ""
			}
			if got := view.Files[ScopeProject]; got != wantProj {
				t.Errorf("files[project] = %q, want %q", got, wantProj)
			}
		})
	}
}

// TestResolveSettingsIsolatesBadValues (critic C3): a stored value outside
// the key's type affects that key only. It is reported on the key, with its
// scope, file and raw text, and the key falls through to the next valid
// scope, then the default; every other key resolves as usual. A bad value in
// a scope the winner outranks is still reported, never masked.
func TestResolveSettingsIsolatesBadValues(t *testing.T) {
	cases := []struct {
		name, project, host  string
		key                  string
		wantValue            bool
		wantOrigin, badScope string
	}{
		{"bad host value, default wins", "", "[wiki]\nenabled = \"no\"\n", "wiki.enabled", true, OriginDefault, ScopeHost},
		{"bad project value, host wins", "[wiki]\nenabled = \"no\"\n", "[wiki]\nenabled = false\n", "wiki.enabled", false, ScopeHost, ScopeProject},
		{"bad host value under a project value", "[wiki]\nenabled = true\n", "[wiki]\nenabled = 1\n", "wiki.enabled", true, ScopeProject, ScopeHost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projDir, hostDir := settingsFixture(t, "[mindmap]\nenabled = false\n"+tc.project, tc.host, false)
			view := ResolveSettings(projDir)
			got := findSetting(t, view, tc.key)
			if got.Value != tc.wantValue || got.Origin != tc.wantOrigin {
				t.Errorf("%s = %v from %s, want %v from %s", tc.key, got.Value, got.Origin, tc.wantValue, tc.wantOrigin)
			}
			files := map[string]string{ScopeProject: filepath.Join(projDir, ".cernerc"), ScopeHost: filepath.Join(hostDir, "settings.toml")}
			if len(got.Invalid) != 1 || got.Invalid[0].Scope != tc.badScope || got.Invalid[0].File != files[tc.badScope] ||
				got.Invalid[0].Raw == "" || !strings.Contains(got.Invalid[0].Error, ErrInvalidSettingValue.Error()) {
				t.Errorf("%s invalid = %+v, want one %s problem with file, raw and error", tc.key, got.Invalid, tc.badScope)
			}
			if m := findSetting(t, view, "mindmap.enabled"); m.Value != false || m.Origin != ScopeProject || len(m.Invalid) != 0 {
				t.Errorf("unrelated mindmap.enabled = %+v, want false from project, clean", m)
			}
			if len(view.Errors) != 0 {
				t.Errorf("file-level errors = %+v, want none", view.Errors)
			}
		})
	}
}

// TestResolveSettingsSkipsUnreadableFile (critic C2/C3): a malformed file
// empties its scope for every key and is reported once, file-level; the other
// scope still resolves. A .cernerc that is valid TOML but does not fit the
// Config struct is no settings problem at all.
func TestResolveSettingsSkipsUnreadableFile(t *testing.T) {
	cases := []struct {
		name, project, host string
		badScope            string
		wantMindmap         bool
		wantOrigin          string
	}{
		{"malformed host", "[mindmap]\nenabled = false\n", "[x", ScopeHost, false, ScopeProject},
		{"malformed project", "[mindmap\n", "[mindmap]\nenabled = false\n", ScopeProject, false, ScopeHost},
		{"typed-invalid .cernerc", "okf = \"yes\"\n[mindmap]\nenabled = false\n", "", "", false, ScopeProject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, tc.project, tc.host, false)
			view := ResolveSettings(projDir)
			if m := findSetting(t, view, "mindmap.enabled"); m.Value != tc.wantMindmap || m.Origin != tc.wantOrigin {
				t.Errorf("mindmap.enabled = %v from %s, want %v from %s", m.Value, m.Origin, tc.wantMindmap, tc.wantOrigin)
			}
			if tc.badScope == "" {
				if len(view.Errors) != 0 {
					t.Errorf("errors = %+v, want none", view.Errors)
				}
				return
			}
			if len(view.Errors) != 1 || view.Errors[0].Scope != tc.badScope || view.Errors[0].File != view.Files[tc.badScope] || view.Errors[0].Error == "" {
				t.Errorf("errors = %+v, want one %s file-level problem", view.Errors, tc.badScope)
			}
		})
	}
}

// TestSetSettingProjectMinimalDiff pins F-0314 criterion 4: a project write
// lands in .cernerc as one changed line; order, comments, indentation and
// every other key come out byte-identical, and the host file is not created.
func TestSetSettingProjectMinimalDiff(t *testing.T) {
	projDir, hostDir := settingsFixture(t, "", "", true)
	writeCernerc(t, projDir, editFixture)

	if err := SetSetting(projDir, "mindmap.enabled", ScopeProject, "false"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	if err != nil {
		t.Fatal(err)
	}
	removed, added := changedLines(editFixture, string(got))
	if len(removed) != 1 || len(added) != 1 || added[0] != "    enabled = false   # keep me\n" {
		t.Errorf("diff = -%q +%q, want exactly the enabled line changed", removed, added)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "settings.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("project write touched the host file: %v", err)
	}
}

// TestSetSettingHostLeavesRepoUntouched pins F-0314 criterion 5: a host write
// goes to <CERNE_CONFIG_DIR>/settings.toml (creating the dir) and changes
// nothing in the project directory.
func TestSetSettingHostLeavesRepoUntouched(t *testing.T) {
	projDir, _ := settingsFixture(t, "", "", true)
	writeCernerc(t, projDir, editFixture)
	hostDir := filepath.Join(t.TempDir(), "absent", "cerne")
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	before := snapshotDir(t, projDir)

	if err := SetSetting(projDir, "wiki.enabled", ScopeHost, "false"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if after := snapshotDir(t, projDir); !maps.Equal(before, after) {
		t.Errorf("project dir changed: before %v, after %v", before, after)
	}
	host, err := os.ReadFile(filepath.Join(hostDir, "settings.toml"))
	if err != nil || string(host) != "[wiki]\nenabled = false\n" {
		t.Errorf("host settings.toml = %q (%v), want [wiki] enabled = false", host, err)
	}
	view := ResolveSettings(projDir)
	if got := findSetting(t, view, "wiki.enabled"); got.Value != false || got.Origin != ScopeHost {
		t.Errorf("wiki.enabled = %v from %s, want false from host", got.Value, got.Origin)
	}
}

// TestSetSettingRefusesInvalid pins F-0314 criterion 6: an unknown key, a
// value outside the type, a scope outside the declared ones or a layout the
// editor cannot rewrite is a named sentinel, and neither file changes.
func TestSetSettingRefusesInvalid(t *testing.T) {
	cases := []struct {
		name, project, id, scope, raw string
		noProject                     bool
		want                          error
	}{
		{"unknown key", "", "nope.enabled", ScopeProject, "false", false, ErrUnknownSetting},
		{"yes", "", "wiki.enabled", ScopeProject, "yes", false, ErrInvalidSettingValue},
		{"1", "", "wiki.enabled", ScopeHost, "1", false, ErrInvalidSettingValue},
		{"quoted JSON string", "", "wiki.enabled", ScopeHost, `"false"`, false, ErrInvalidSettingValue},
		{"undeclared scope", "", "wiki.enabled", "global", "false", false, ErrSettingScope},
		{"dotted layout", "wiki.enabled = true\n", "wiki.enabled", ScopeProject, "false", false, ErrConfigLayout},
		{"no project", "", "wiki.enabled", ScopeProject, "false", true, ErrNoProject},
		{"malformed project file", "[x\n", "wiki.enabled", ScopeProject, "false", false, ErrConfigParse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := "[graph]\nenabled = true # mine\n"
			projDir, hostDir := settingsFixture(t, tc.project, host, tc.noProject)
			before := snapshotDir(t, projDir)

			err := SetSetting(projDir, tc.id, tc.scope, tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if after := snapshotDir(t, projDir); !maps.Equal(before, after) {
				t.Errorf("project dir changed on refusal")
			}
			if got, _ := os.ReadFile(filepath.Join(hostDir, "settings.toml")); string(got) != host {
				t.Errorf("host file changed on refusal: %q", got)
			}
		})
	}
}

// snapshotDir maps each regular file name in dir to its content.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := map[string]string{}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		snap[e.Name()] = string(data)
	}
	return snap
}

// TestUnsetSettingRestoresLowerScope: removing the project value hands the key
// back to the host scope as one removed line; the host file is untouched.
func TestUnsetSettingRestoresLowerScope(t *testing.T) {
	host := "[mindmap]\nenabled = false\n"
	projDir, hostDir := settingsFixture(t, "[mindmap]\n# team choice\nenabled = true\n", host, false)
	before, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))

	if err := UnsetSetting(projDir, "mindmap.enabled", ScopeProject); err != nil {
		t.Fatalf("UnsetSetting: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	if removed, added := changedLines(string(before), string(after)); len(removed) != 1 || len(added) != 0 || removed[0] != "enabled = true\n" {
		t.Errorf("diff = -%q +%q, want exactly the enabled line removed", removed, added)
	}
	if got, _ := os.ReadFile(filepath.Join(hostDir, "settings.toml")); string(got) != host {
		t.Errorf("host file changed: %q", got)
	}
	view := ResolveSettings(projDir)
	if got := findSetting(t, view, "mindmap.enabled"); got.Value != false || got.Origin != ScopeHost {
		t.Errorf("mindmap.enabled = %v from %s, want false from host", got.Value, got.Origin)
	}
}

// TestUnsetSettingAbsentIsNoop: unsetting a key no scope file holds writes
// nothing (no host file or dir appears); unknown keys and scopes are refused.
func TestUnsetSettingAbsentIsNoop(t *testing.T) {
	projDir, _ := settingsFixture(t, "", "", false)
	hostDir := filepath.Join(t.TempDir(), "absent")
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	before := snapshotDir(t, projDir)

	for _, scope := range []string{ScopeProject, ScopeHost} {
		if err := UnsetSetting(projDir, "wiki.enabled", scope); err != nil {
			t.Fatalf("UnsetSetting %s: %v", scope, err)
		}
	}
	if after := snapshotDir(t, projDir); !maps.Equal(before, after) {
		t.Errorf("project dir changed by a no-op unset")
	}
	if _, err := os.Stat(hostDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no-op host unset created %s (%v)", hostDir, err)
	}
	if err := UnsetSetting(projDir, "nope.enabled", ScopeHost); !errors.Is(err, ErrUnknownSetting) {
		t.Errorf("unknown key: err = %v, want ErrUnknownSetting", err)
	}
	if err := UnsetSetting(projDir, "wiki.enabled", "global"); !errors.Is(err, ErrSettingScope) {
		t.Errorf("undeclared scope: err = %v, want ErrSettingScope", err)
	}
}

// TestSetSettingConcurrentWritersAllLand pins git's lockfile discipline on
// settings writes (C1): concurrent writers of distinct keys of one file, as
// the ui fires them back to back, serialize instead of losing updates.
func TestSetSettingConcurrentWritersAllLand(t *testing.T) {
	projDir, hostDir := settingsFixture(t, "", "", false)
	files := map[string]string{
		ScopeProject: filepath.Join(projDir, ".cernerc"),
		ScopeHost:    filepath.Join(hostDir, "settings.toml"),
	}
	ids := []string{"graph.enabled", "mindmap.enabled", "wiki.enabled"}
	for round := range 20 {
		want := round%2 == 1
		var wg sync.WaitGroup
		errs := make(chan error, len(ids)*len(files))
		for _, id := range ids {
			for scope := range files {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs <- SetSetting(projDir, id, scope, strconv.FormatBool(want))
				}()
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: SetSetting: %v", round, err)
			}
		}
		for scope, path := range files {
			m, err := readTOMLFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range ids {
				table, key, _ := strings.Cut(id, ".")
				tbl, _ := m[table].(map[string]any)
				if got := tbl[key]; got != want {
					t.Fatalf("round %d: %s %s = %v, want %v (a concurrent write was lost)", round, scope, id, got, want)
				}
			}
		}
	}
	for _, path := range files {
		if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s.lock left behind (%v)", path, err)
		}
	}
}

// TestConfigWritersRefuseAHeldLock: a lock another writer holds past the wait
// is ErrConfigBusy for every writer (set, unset, llm mode); the file stays
// as it was and the foreign lock is left alone. A writer's own failure
// removes its lock.
func TestConfigWritersRefuseAHeldLock(t *testing.T) {
	prev := configLockTimeout
	configLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { configLockTimeout = prev })
	projDir, hostDir := settingsFixture(t, "[graph]\nenabled = true\n", "[graph]\nenabled = true\n", false)
	host, cernerc := filepath.Join(hostDir, "settings.toml"), filepath.Join(projDir, ".cernerc")
	before := map[string]string{}
	for _, p := range []string{host, cernerc} {
		data, _ := os.ReadFile(p)
		before[p] = string(data)
		if err := os.WriteFile(p+".lock", []byte("held"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, write := range map[string]func() error{
		"set":      func() error { return SetSetting(projDir, "graph.enabled", ScopeHost, "false") },
		"unset":    func() error { return UnsetSetting(projDir, "graph.enabled", ScopeProject) },
		"llm mode": func() error { return SetLLMMode(projDir, "claude-cli") },
	} {
		if err := write(); !errors.Is(err, ErrConfigBusy) {
			t.Errorf("%s under a held lock: err = %v, want ErrConfigBusy", name, err)
		}
	}
	for p, want := range before {
		if got, _ := os.ReadFile(p); string(got) != want {
			t.Errorf("%s changed under a held lock: %q", p, got)
		}
		if lock, _ := os.ReadFile(p + ".lock"); string(lock) != "held" {
			t.Errorf("%s.lock = %q, want the foreign lock untouched", p, lock)
		}
		os.Remove(p + ".lock")
	}

	if err := os.WriteFile(host, []byte("graph.enabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(projDir, "graph.enabled", ScopeHost, "false"); !errors.Is(err, ErrConfigLayout) {
		t.Fatalf("err = %v, want ErrConfigLayout", err)
	}
	if _, err := os.Stat(host + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("refused write left its lock behind (%v)", err)
	}
}

// TestSetThenUnsetRestoresFile (critic C6): set then unset of a key the file
// did not hold gives the file back byte for byte, like git config --unset.
func TestSetThenUnsetRestoresFile(t *testing.T) {
	for name, content := range map[string]string{"plain": baseCernerc, "commented": editFixture, "CRLF": crlf(editFixture)} {
		for _, scope := range []string{ScopeProject, ScopeHost} {
			t.Run(name+"/"+scope, func(t *testing.T) {
				projDir, hostDir := settingsFixture(t, "", "", true)
				writeCernerc(t, projDir, content)
				path := map[string]string{ScopeProject: filepath.Join(projDir, ".cernerc"), ScopeHost: filepath.Join(hostDir, "settings.toml")}[scope]
				if scope == ScopeHost {
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				for _, id := range []string{"wiki.enabled", "graph.enabled"} {
					if err := SetSetting(projDir, id, scope, "false"); err != nil {
						t.Fatalf("set %s: %v", id, err)
					}
				}
				for _, id := range []string{"graph.enabled", "wiki.enabled"} {
					if err := UnsetSetting(projDir, id, scope); err != nil {
						t.Fatalf("unset %s: %v", id, err)
					}
				}
				if got, _ := os.ReadFile(path); string(got) != content {
					removed, added := changedLines(content, string(got))
					t.Errorf("file after set+unset differs: -%q +%q", removed, added)
				}
			})
		}
	}
}

// TestConfigWritesKeepModeAndFollowSymlinks (critic C7): a rewrite keeps the
// file's mode bits (a new file gets 0644, as git checks one out) and writes
// through a symlink to its target, keeping the link: true of the settings
// writers and of writeFileAtomic (SaveConfig, docs.lock).
func TestConfigWritesKeepModeAndFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits and symlinks")
	}
	modeOf := func(p string) os.FileMode {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return fi.Mode().Perm()
	}
	for _, mode := range []os.FileMode{0o644, 0o640} {
		projDir, _ := settingsFixture(t, "", "", false)
		cernerc := filepath.Join(projDir, ".cernerc")
		if err := os.Chmod(cernerc, mode); err != nil {
			t.Fatal(err)
		}
		if err := SetSetting(projDir, "wiki.enabled", ScopeProject, "false"); err != nil {
			t.Fatal(err)
		}
		if got := modeOf(cernerc); got != mode {
			t.Errorf("SetSetting: .cernerc mode %v, want %v kept", got, mode)
		}
		if err := SetLLMMode(projDir, "claude-cli"); err != nil {
			t.Fatal(err)
		}
		if err := SaveConfig(projDir, Config{ProjectID: "p", Backend: "b"}); err != nil {
			t.Fatal(err)
		}
		if got := modeOf(cernerc); got != mode {
			t.Errorf("SetLLMMode+SaveConfig: .cernerc mode %v, want %v kept", got, mode)
		}
	}

	projDir, hostDir := t.TempDir(), t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	if err := SaveConfig(projDir, Config{ProjectID: "p", Backend: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(projDir, "wiki.enabled", ScopeHost, "false"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(projDir, ".cernerc"), filepath.Join(hostDir, "settings.toml")} {
		if got := modeOf(p); got != 0o644 {
			t.Errorf("new %s mode %v, want 0644", filepath.Base(p), got)
		}
	}

	dotfiles := t.TempDir()
	target := filepath.Join(dotfiles, "settings.toml")
	if err := os.WriteFile(target, []byte("[graph]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(hostDir, "settings.toml")
	os.Remove(link)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(projDir, "graph.enabled", ScopeHost, "false"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("host settings.toml is no longer a symlink (%v)", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "[graph]\nenabled = false\n" {
		t.Errorf("symlink target = %q, want the write to land there", got)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dotfiles, "*.lock")); len(leftovers) != 0 {
		t.Errorf("lock left behind: %v", leftovers)
	}

	cernercTarget := filepath.Join(dotfiles, "cernerc")
	if err := os.Rename(filepath.Join(projDir, ".cernerc"), cernercTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cernercTarget, filepath.Join(projDir, ".cernerc")); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(projDir, Config{ProjectID: "q", Backend: "b"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(projDir, ".cernerc")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("SaveConfig replaced the .cernerc symlink (%v)", err)
	}
	if got, _ := os.ReadFile(cernercTarget); !strings.Contains(string(got), `project_id = "q"`) {
		t.Errorf("SaveConfig did not write through the symlink: %q", got)
	}
}

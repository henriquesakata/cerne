package project

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
			view, err := ResolveSettings(projDir)
			if err != nil {
				t.Fatalf("ResolveSettings: %v", err)
			}
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

// TestResolveSettingsRejectsBadStoredValue: a stored value of the wrong type
// is a named error citing the file and the key, never a silent default.
func TestResolveSettingsRejectsBadStoredValue(t *testing.T) {
	bad := "[mindmap]\nenabled = \"no\"\n"
	for _, tc := range []struct{ name, project, host, file string }{
		{"project", bad, "", ".cernerc"},
		{"host", "", bad, "settings.toml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, tc.project, tc.host, false)
			_, err := ResolveSettings(projDir)
			if !errors.Is(err, ErrInvalidSettingValue) {
				t.Fatalf("err = %v, want ErrInvalidSettingValue", err)
			}
			if !strings.Contains(err.Error(), tc.file) || !strings.Contains(err.Error(), "mindmap.enabled") {
				t.Errorf("err = %q, want it to cite %s and mindmap.enabled", err, tc.file)
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
	view, err := ResolveSettings(projDir)
	if err != nil {
		t.Fatal(err)
	}
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

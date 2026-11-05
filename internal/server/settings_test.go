package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
)

// settingsBackend is a single-project server whose .cernerc carries extra and
// whose host scope is a private CERNE_CONFIG_DIR.
func settingsBackend(t *testing.T, extra string) (h http.Handler, projDir, hostDir string) {
	t.Helper()
	repoDir, projDir := newBackend(t)
	hostDir = t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	cernerc := "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n" + extra
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(repoDir, projDir, ""), projDir, hostDir
}

func resolvedByID(v project.SettingsView) map[string]project.ResolvedSetting {
	byID := map[string]project.ResolvedSetting{}
	for _, s := range v.Settings {
		byID[s.ID] = s
	}
	return byID
}

// TestSettingsEndpointServesRegistry pins F-0314 criterion 7: one GET carries
// every declared key with its whole declaration, effective value and winning
// scope, plus the file behind each scope.
func TestSettingsEndpointServesRegistry(t *testing.T) {
	h, projDir, hostDir := settingsBackend(t, "[mindmap]\nenabled = false\n")
	var got struct {
		Settings []map[string]any  `json:"settings"`
		Files    map[string]string `json:"files"`
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/settings", "", &got); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	want := project.ResolveSettings(projDir)
	if len(got.Settings) != len(want.Settings) {
		t.Fatalf("served %d settings, registry declares %d", len(got.Settings), len(want.Settings))
	}
	for i, s := range got.Settings {
		for _, field := range []string{"id", "title", "type", "default", "scopes", "description", "value", "origin"} {
			if _, ok := s[field]; !ok {
				t.Errorf("setting %v lacks %q", s["id"], field)
			}
		}
		if s["id"] != want.Settings[i].ID {
			t.Errorf("settings[%d].id = %v, want %s", i, s["id"], want.Settings[i].ID)
		}
		if s["id"] == "mindmap.enabled" && (s["value"] != false || s["origin"] != project.ScopeProject) {
			t.Errorf("mindmap.enabled = %v from %v, want false from project", s["value"], s["origin"])
		}
	}
	if got.Files[project.ScopeProject] != filepath.Join(projDir, ".cernerc") || got.Files[project.ScopeHost] != filepath.Join(hostDir, "settings.toml") {
		t.Errorf("files = %v", got.Files)
	}
}

// TestSettingsPutWritesScope: PUT writes the scope and answers with the new
// view, so the UI needs no second GET; a host write leaves .cernerc alone.
func TestSettingsPutWritesScope(t *testing.T) {
	h, projDir, _ := settingsBackend(t, "")
	before, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	var view project.SettingsView
	rec := doJSON(t, h, http.MethodPut, "/api/settings/wiki.enabled", `{"scope":"host","value":false}`, &view)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if w := resolvedByID(view)["wiki.enabled"]; w.Value != false || w.Origin != project.ScopeHost {
		t.Errorf("wiki.enabled = %v from %s, want false from host", w.Value, w.Origin)
	}
	if after, _ := os.ReadFile(filepath.Join(projDir, ".cernerc")); string(after) != string(before) {
		t.Errorf(".cernerc changed by a host write:\n%s", after)
	}
}

// TestSettingsPutRejects pins F-0314 criterion 6 on the endpoint: each
// sentinel maps to its status and nothing is written.
func TestSettingsPutRejects(t *testing.T) {
	cases := []struct {
		name, extra, id, body string
		want                  int
	}{
		{"unknown key", "", "nope.enabled", `{"scope":"host","value":false}`, http.StatusNotFound},
		{"string value", "", "wiki.enabled", `{"scope":"host","value":"false"}`, http.StatusBadRequest},
		{"missing value", "", "wiki.enabled", `{"scope":"host"}`, http.StatusBadRequest},
		{"undeclared scope", "", "wiki.enabled", `{"scope":"global","value":false}`, http.StatusBadRequest},
		{"invalid body", "", "wiki.enabled", `{`, http.StatusBadRequest},
		{"unsupported layout", "wiki.enabled = true\n", "wiki.enabled", `{"scope":"project","value":false}`, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, projDir, hostDir := settingsBackend(t, tc.extra)
			before, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
			var body map[string]string
			rec := doJSON(t, h, http.MethodPut, "/api/settings/"+tc.id, tc.body, &body)
			// "route not found" is the mux's own 404: the handler must answer.
			if rec.Code != tc.want || body["error"] == "" || body["error"] == "route not found" {
				t.Fatalf("status %d body %v, want %d with an error", rec.Code, body, tc.want)
			}
			if after, _ := os.ReadFile(filepath.Join(projDir, ".cernerc")); string(after) != string(before) {
				t.Errorf(".cernerc changed on refusal")
			}
			if _, err := os.Stat(filepath.Join(hostDir, "settings.toml")); err == nil {
				t.Errorf("host settings.toml written on refusal")
			}
		})
	}
}

// TestSettingsDeleteUnsetsScope: DELETE ?scope= removes the key from that
// scope and answers with the new view; unknown keys and scopes are refused.
func TestSettingsDeleteUnsetsScope(t *testing.T) {
	h, _, hostDir := settingsBackend(t, "[mindmap]\nenabled = true\n")
	if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte("[mindmap]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var view project.SettingsView
	rec := doJSON(t, h, http.MethodDelete, "/api/settings/mindmap.enabled?scope=project", "", &view)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if m := resolvedByID(view)["mindmap.enabled"]; m.Value != false || m.Origin != project.ScopeHost {
		t.Errorf("mindmap.enabled = %v from %s, want false from host", m.Value, m.Origin)
	}
	var body map[string]string
	if rec := doJSON(t, h, http.MethodDelete, "/api/settings/nope.enabled?scope=host", "", &body); rec.Code != http.StatusNotFound {
		t.Errorf("unknown key: status %d, want 404", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/settings/mindmap.enabled?scope=global", "", &body); rec.Code != http.StatusBadRequest {
		t.Errorf("undeclared scope: status %d, want 400", rec.Code)
	}
}

// TestSettingsRouteLocalOnly: the host scope is per-host state, so the route
// lives on the local `cerne ui` and never on the remote `cerne serve`.
func TestSettingsRouteLocalOnly(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(dir string) (string, string, string, string, error) {
		if dir == "A" {
			return repoA, projA, "proj-a", "Projeto A", nil
		}
		return "", "", "", "", fmt.Errorf("dir desconhecido: %s", dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ui, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(ui)
	defer srv.Close()
	var view project.SettingsView
	getJSON(t, srv.URL+"/api/projects/proj-a/settings", &view)
	if len(view.Settings) == 0 {
		t.Errorf("ui settings view is empty")
	}

	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	serve := NewMultiProject(mpSecret)
	doMP(t, serve, http.MethodPut, "/api/projects/proj-a", regBody(t, seedBareBackend(t, "Projeto A")), mpSecret, nil)
	if rec := doMP(t, serve, http.MethodGet, "/api/projects/proj-a/settings", "", mpSecret, nil); rec.Code != http.StatusNotFound {
		t.Errorf("serve /settings: status %d, want 404", rec.Code)
	}
}

// TestSettingsPutBusyIsConflict: a write blocked by another writer's lock is
// 409 and writes nothing.
func TestSettingsPutBusyIsConflict(t *testing.T) {
	h, _, hostDir := settingsBackend(t, "")
	lock := filepath.Join(hostDir, "settings.toml.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	rec := doJSON(t, h, http.MethodPut, "/api/settings/wiki.enabled", `{"scope":"host","value":false}`, &body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d body %v, want 409", rec.Code, body)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "settings.toml")); err == nil {
		t.Errorf("settings.toml written despite the held lock")
	}
}

// TestSettingsEndpointReportsProblems (critic C3): a bad stored value or a
// malformed scope file is reported in the view, never a failed GET.
func TestSettingsEndpointReportsProblems(t *testing.T) {
	for _, tc := range []struct{ name, host string }{
		{"bad value", "[wiki]\nenabled = \"no\"\n"},
		{"malformed", "[x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, hostDir := settingsBackend(t, "[mindmap]\nenabled = false\n")
			if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte(tc.host), 0o644); err != nil {
				t.Fatal(err)
			}
			var view project.SettingsView
			rec := doJSON(t, h, http.MethodGet, "/api/settings", "", &view)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			problems := len(view.Errors)
			for _, s := range view.Settings {
				problems += len(s.Invalid)
			}
			if problems != 1 || resolvedByID(view)["mindmap.enabled"].Value != false {
				t.Errorf("view = %s, want mindmap false and exactly one reported problem", rec.Body.String())
			}
		})
	}
}

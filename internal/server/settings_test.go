package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
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
	// A key the scope does not hold is a 200 no-op, unlike the CLI's exit 1:
	// the UI's "unset" toggle is idempotent.
	if rec := doJSON(t, h, http.MethodDelete, "/api/settings/mindmap.enabled?scope=project", "", &view); rec.Code != http.StatusOK {
		t.Errorf("absent key: status %d, want 200 no-op", rec.Code)
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

// TestSettingsWriteAnnouncesProjects pins F-0315 criteria 6 and 7 live: a
// landed settings write (set or unset) announces a projects refresh on the
// SSE stream, so every browser tab re-reads /api/projects and its tab gates
// without a server restart; a refused write announces nothing.
func TestSettingsWriteAnnouncesProjects(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	repoA, projA, _ := newGitBackendNoClean(t)
	if err := os.WriteFile(filepath.Join(projA, ".cernerc"), []byte("project_id = \"proj-a\"\nbackend = \"x\"\ndefault_scope = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	req, _ := http.NewRequestWithContext(sctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer resp.Body.Close()
	events := make(chan string, 8)
	go func() {
		br := bufio.NewReader(resp.Body)
		for {
			d, err := readSSEData(br)
			if err != nil {
				return
			}
			events <- d
		}
	}()
	if got := waitEvent(t, events); got != "" {
		t.Fatalf("connect frame data = %q, want empty (projects event)", got)
	}
	graph := func() bool {
		var list projectsList
		getJSON(t, srv.URL+"/api/projects", &list)
		return list.Projects[0].Graph
	}

	var view project.SettingsView
	if rec := doJSON(t, ui, http.MethodPut, "/api/projects/proj-a/settings/graph.enabled", `{"scope":"project","value":false}`, &view); rec.Code != http.StatusOK {
		t.Fatalf("PUT: status %d %s", rec.Code, rec.Body.String())
	}
	if got := waitEvent(t, events); got != "" {
		t.Fatalf("after PUT: event data = %q, want a projects event", got)
	}
	if graph() {
		t.Errorf("after PUT graph.enabled=false: /api/projects graph = true")
	}

	if rec := doJSON(t, ui, http.MethodDelete, "/api/projects/proj-a/settings/graph.enabled?scope=project", "", &view); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: status %d %s", rec.Code, rec.Body.String())
	}
	if got := waitEvent(t, events); got != "" {
		t.Fatalf("after DELETE: event data = %q, want a projects event", got)
	}
	if !graph() {
		t.Errorf("after DELETE graph.enabled: /api/projects graph = false")
	}

	var body map[string]string
	if rec := doJSON(t, ui, http.MethodPut, "/api/projects/proj-a/settings/graph.enabled", `{"scope":"project","value":"false"}`, &body); rec.Code != http.StatusBadRequest {
		t.Fatalf("refused PUT: status %d, want 400", rec.Code)
	}
	select {
	case e := <-events:
		t.Errorf("refused write announced %q", e)
	case <-time.After(200 * time.Millisecond):
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

// TestSettingsWriteSucceedsDespiteBadHostFile (critic C4): a write that
// landed is 200 with the new view, whatever else the re-read finds.
func TestSettingsWriteSucceedsDespiteBadHostFile(t *testing.T) {
	for _, host := range []string{"[wiki]\nenabled = \"no\"\n", "[x"} {
		h, _, hostDir := settingsBackend(t, "")
		if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte(host), 0o644); err != nil {
			t.Fatal(err)
		}
		var view project.SettingsView
		rec := doJSON(t, h, http.MethodPut, "/api/settings/graph.enabled", `{"scope":"project","value":false}`, &view)
		if g := resolvedByID(view)["graph.enabled"]; rec.Code != http.StatusOK || g.Value != false || g.Origin != project.ScopeProject {
			t.Errorf("host %q: PUT status %d, graph.enabled %v from %s; want 200, false from project", host, rec.Code, g.Value, g.Origin)
		}
		rec = doJSON(t, h, http.MethodDelete, "/api/settings/graph.enabled?scope=project", "", &view)
		if g := resolvedByID(view)["graph.enabled"]; rec.Code != http.StatusOK || g.Origin != project.OriginDefault {
			t.Errorf("host %q: DELETE status %d, graph.enabled from %s; want 200, default", host, rec.Code, g.Origin)
		}
	}
}

// TestSettingsWriteStatusMapping (critic C5): every refusal of a write has
// its status: no .cernerc for a project write is 409, a malformed file 422.
func TestSettingsWriteStatusMapping(t *testing.T) {
	cases := []struct {
		name, cernerc, host, method, target, body string
		want                                      int
	}{
		{"no .cernerc", "", "", http.MethodPut, "/api/settings/wiki.enabled", `{"scope":"project","value":false}`, http.StatusConflict},
		{"malformed .cernerc", "[x\n", "", http.MethodPut, "/api/settings/wiki.enabled", `{"scope":"project","value":false}`, http.StatusUnprocessableEntity},
		{"malformed host file", "project_id = \"p\"\n", "[x", http.MethodPut, "/api/settings/wiki.enabled", `{"scope":"host","value":false}`, http.StatusUnprocessableEntity},
		{"unset in malformed .cernerc", "[x\n", "", http.MethodDelete, "/api/settings/wiki.enabled?scope=project", "", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir := newBackend(t)
			hostDir := t.TempDir()
			t.Setenv("CERNE_CONFIG_DIR", hostDir)
			for path, content := range map[string]string{filepath.Join(projDir, ".cernerc"): tc.cernerc, filepath.Join(hostDir, "settings.toml"): tc.host} {
				if content != "" {
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			var body map[string]string
			rec := doJSON(t, New(repoDir, projDir, ""), tc.method, tc.target, tc.body, &body)
			if rec.Code != tc.want || body["error"] == "" {
				t.Errorf("status %d body %v, want %d with an error", rec.Code, body, tc.want)
			}
		})
	}
}

// TestSettingsEndpointCarriesStoredValues: the payload carries each scope's
// stored value, so the UI can show the one a higher scope overrides.
func TestSettingsEndpointCarriesStoredValues(t *testing.T) {
	h, _, hostDir := settingsBackend(t, "[mindmap]\nenabled = true\n")
	if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte("[mindmap]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Settings []map[string]any `json:"settings"`
	}
	doJSON(t, h, http.MethodGet, "/api/settings", "", &got)
	for _, s := range got.Settings {
		if s["id"] != "mindmap.enabled" {
			continue
		}
		stored, _ := s["stored"].(map[string]any)
		if s["value"] != true || s["origin"] != project.ScopeProject || stored[project.ScopeProject] != true || stored[project.ScopeHost] != false || len(stored) != 2 {
			t.Errorf("mindmap.enabled = %v, want true from project with stored {project: true, host: false}", s)
		}
		return
	}
	t.Fatal("mindmap.enabled not served")
}

// TestSettingsPutStringValue (F-0325): a string key takes a JSON string and
// refuses a bool; a bool key still refuses the JSON string "false".
func TestSettingsPutStringValue(t *testing.T) {
	h, _, _ := settingsBackend(t, "")
	var view project.SettingsView
	rec := doJSON(t, h, http.MethodPut, "/api/settings/channel.human_name", `{"scope":"host","value":"Henrique"}`, &view)
	if rec.Code != http.StatusOK || resolvedByID(view)["channel.human_name"].Value != "Henrique" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if rec := doJSON(t, h, http.MethodPut, "/api/settings/channel.human_name", `{"scope":"host","value":true}`, &body); rec.Code != http.StatusBadRequest {
		t.Fatalf("bool under a string key: status %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/settings/wiki.enabled", `{"scope":"host","value":"false"}`, &body); rec.Code != http.StatusBadRequest {
		t.Fatalf("string under a bool key: status %d", rec.Code)
	}
}

// TestProjectNamePutAnnouncesProjects pins F-0331 crit. 1 and 2: a rename from
// the local ui answers the stored name and announces a projects refresh, after
// which /api/projects (the selector) and /state (Kanban, Mindmap) carry it.
func TestProjectNamePutAnnouncesProjects(t *testing.T) {
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

	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	req, _ := http.NewRequestWithContext(sctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer resp.Body.Close()
	events := make(chan string, 8)
	go func() {
		br := bufio.NewReader(resp.Body)
		for {
			d, err := readSSEData(br)
			if err != nil {
				return
			}
			events <- d
		}
	}()
	if got := waitEvent(t, events); got != "" {
		t.Fatalf("connect frame data = %q, want empty (projects event)", got)
	}

	var got struct {
		Name string `json:"name"`
	}
	if rec := doJSON(t, ui, http.MethodPut, "/api/projects/proj-a/project/name", `{"name":"  Núcleo  "}`, &got); rec.Code != http.StatusOK || got.Name != "Núcleo" {
		t.Fatalf("PUT = %d %s, want 200 {name: Núcleo}", rec.Code, rec.Body.String())
	}
	// The commit advances HEAD, so a change frame (non-empty data) may come
	// first: skip to the projects event (empty data).
	for d := waitEvent(t, events); d != ""; d = waitEvent(t, events) {
	}

	var list projectsList
	getJSON(t, srv.URL+"/api/projects", &list)
	if list.Projects[0].Name != "Núcleo" {
		t.Errorf("/api/projects name = %q, want Núcleo", list.Projects[0].Name)
	}
	var state struct {
		Project struct {
			Name string `json:"name"`
		} `json:"project"`
	}
	getJSON(t, srv.URL+"/api/projects/proj-a/state", &state)
	if state.Project.Name != "Núcleo" {
		t.Errorf("/state project.name = %q, want Núcleo", state.Project.Name)
	}
}

// TestProjectNamePutRefusesInvalid pins F-0331 crit. 4 over HTTP: each broken
// rule is a 400 naming it, and nothing reaches the backend.
func TestProjectNamePutRefusesInvalid(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	h := New(repoDir, projDir, "")
	head0, _ := gitback.HeadSHA(origin)
	for _, tc := range []struct{ in, want string }{
		{"", "empty"},
		{"   ", "empty"},
		{strings.Repeat("é", 65), "64"},
		{"a\nb", "control"},
		{"a\tb", "control"},
	} {
		body, _ := json.Marshal(map[string]string{"name": tc.in})
		var got struct {
			Error string `json:"error"`
		}
		if rec := doJSON(t, h, http.MethodPut, "/api/project/name", string(body), &got); rec.Code != http.StatusBadRequest || !strings.Contains(got.Error, tc.want) {
			t.Errorf("PUT %q = %d %q, want 400 mentioning %q", tc.in, rec.Code, got.Error, tc.want)
		}
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("a refused rename reached origin: %s -> %s", head0, h)
	}
}

// TestProjectNamePutRefusesCrossOrigin pins F-0331 crit. 9: the same
// sameOriginJSON guard as the other UI JSON writes.
func TestProjectNamePutRefusesCrossOrigin(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	h := New(repoDir, projDir, "")
	head0, _ := gitback.HeadSHA(origin)
	for _, hdr := range []map[string]string{
		{"Origin": "https://evil.example", "Content-Type": "application/json"},
		{"Content-Type": "text/plain"},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/project/name", strings.NewReader(`{"name":"Plantado"}`))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("PUT %v = %d, want 403", hdr, rec.Code)
		}
	}
	if p, _ := store.LoadProject(repoDir); p.Name != "Projeto Demo" {
		t.Fatalf("a refused request renamed the project to %q", p.Name)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("a refused request reached origin: %s -> %s", head0, h)
	}
}

// TestProjectNameRouteLocalOnly pins F-0331 crit. 9: the rename lives on the
// local `cerne ui`, never on the remote `cerne serve`.
func TestProjectNameRouteLocalOnly(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	serve := NewMultiProject(mpSecret)
	doMP(t, serve, http.MethodPut, "/api/projects/proj-a", regBody(t, seedBareBackend(t, "Projeto A")), mpSecret, nil)
	if rec := doMP(t, serve, http.MethodPut, "/api/projects/proj-a/project/name", `{"name":"X"}`, mpSecret, nil); rec.Code != http.StatusNotFound {
		t.Errorf("serve PUT /project/name: status %d, want 404", rec.Code)
	}
}

// TestProjectNameStatus pins F-0331 crit. 7 (HTTP side): a busy lock or a
// concurrent change is a retryable 409, a broken rule a 400, the rest a 500.
// The lock timeout is a 150 s const, so the mapping is pinned on the errors.
func TestProjectNameStatus(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w: x", app.ErrInvalidProjectName), http.StatusBadRequest},
		{fmt.Errorf("%w: x", gitback.ErrLockTimeout), http.StatusConflict},
		{&app.ConflictError{Conflicts: []app.Conflict{{Path: "project.json"}}}, http.StatusConflict},
		{errors.New("push rejected"), http.StatusInternalServerError},
	} {
		if got := projectNameStatus(tc.err); got != tc.want {
			t.Errorf("projectNameStatus(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

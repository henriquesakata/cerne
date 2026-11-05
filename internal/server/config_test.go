package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitlab.com/rockt/cerne/internal/models"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// TestLLMConfigGetDefault: with a .cernerc that has no [llm] block, the mode
// reads back as the normalized default "delegation".
func TestLLMConfigGetDefault(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var got struct {
		Mode string `json:"mode"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/config/llm", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Mode != "delegation" {
		t.Fatalf("mode = %q, want delegation", got.Mode)
	}
}

// TestLLMConfigGetServesCatalog: F-0218 criterion 5 / F-0311 criterion 1 —
// GET carries the resolved model catalog (label, provider, per-model efforts
// and default, visibility) so the chat selector never hardcodes a model.
func TestLLMConfigGetServesCatalog(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var got struct {
		Models       []models.Entry `json:"models"`
		DefaultModel string         `json:"default_model"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/config/llm", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	base := models.Baseline()
	if len(got.Models) != len(base.Entries) || got.DefaultModel != base.DefaultModel {
		t.Fatalf("served %d models (default %q), want the baseline %d (%q)", len(got.Models), got.DefaultModel, len(base.Entries), base.DefaultModel)
	}
	for _, e := range got.Models {
		if e.Label == "" || e.Provider == "" || e.Visibility == "" {
			t.Fatalf("entry served without label/provider/visibility: %+v", e)
		}
	}
}

// TestLLMConfigMalformedCatalogServesBaseline: criterion 3 — a malformed
// global catalog never breaks the page: the baseline is served and the reason
// lands in the structured log.
func TestLLMConfigMalformedCatalogServesBaseline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, models.FileName), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := telemetry.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { telemetry.SetLogger(prev) })

	repoDir, projDir := newBackend(t)
	var got struct {
		Models []models.Entry `json:"models"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/config/llm", "", &got)
	if rec.Code != http.StatusOK || len(got.Models) != len(models.Baseline().Entries) {
		t.Fatalf("status %d, %d models; want 200 with the baseline", rec.Code, len(got.Models))
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "malformed") {
		t.Fatalf("log = %q, want a WARN naming the malformed file", logs.String())
	}
}

// TestLLMConfigPutSwitchesMode: PUT changes the mode in .cernerc and preserves
// the other config fields; a follow-up load reflects the new mode.
func TestLLMConfigPutSwitchesMode(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var got struct {
		Mode string `json:"mode"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/config/llm", `{"mode":"claude-cli"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Mode != "claude-cli" {
		t.Fatalf("response mode = %q, want claude-cli", got.Mode)
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.LLM.Mode != "claude-cli" {
		t.Fatalf("persisted mode = %q, want claude-cli", cfg.LLM.Mode)
	}
	if cfg.ProjectID != "p" || cfg.Backend != "b" {
		t.Fatalf("config fields not preserved: %+v", cfg)
	}
}

// TestLLMConfigPutPreservesCernerc (F-0314 criterion 4): switching the mode
// from the UI rewrites only the llm.mode line; comments and tables the Config
// struct does not model (a settings-registry [mindmap]) survive byte for byte.
func TestLLMConfigPutPreservesCernerc(t *testing.T) {
	repoDir, projDir := newBackend(t)
	before := "project_id = \"p\"\n# keep this comment\nbackend = \"b\"\n\n[mindmap]\nenabled = false\n\n[llm]\nmode = \"delegation\" # x\n"
	path := filepath.Join(projDir, ".cernerc")
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Mode string `json:"mode"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/config/llm", `{"mode":"claude-cli"}`, &got)
	if rec.Code != http.StatusOK || got.Mode != "claude-cli" {
		t.Fatalf("status %d mode %q, want 200 claude-cli; body: %s", rec.Code, got.Mode, rec.Body.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(before, `mode = "delegation" # x`, `mode = "claude-cli" # x`, 1); string(after) != want {
		t.Fatalf(".cernerc after PUT =\n%s\nwant only the mode line changed:\n%s", after, want)
	}
}

// TestLLMConfigPutMapsRefusals (critic C5): PUT /config/llm maps a write
// refusal like /settings does: a layout the editor will not touch and a
// malformed file are 422, another writer's lock is 409.
func TestLLMConfigPutMapsRefusals(t *testing.T) {
	cases := []struct {
		name, cernerc string
		held          bool
		want          int
	}{
		{"dotted llm.mode", "project_id = \"p\"\nllm.mode = \"delegation\"\n", false, http.StatusUnprocessableEntity},
		{"malformed", "project_id = \"p\"\n[llm\n", false, http.StatusUnprocessableEntity},
		{"held lock", "project_id = \"p\"\n", true, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir := newBackend(t)
			path := filepath.Join(projDir, ".cernerc")
			if err := os.WriteFile(path, []byte(tc.cernerc), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.held {
				if err := os.WriteFile(path+".lock", nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var body map[string]string
			rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/config/llm", `{"mode":"claude-cli"}`, &body)
			if rec.Code != tc.want || body["error"] == "" {
				t.Errorf("status %d body %v, want %d with an error", rec.Code, body, tc.want)
			}
			if after, _ := os.ReadFile(path); string(after) != tc.cernerc {
				t.Errorf(".cernerc changed on refusal: %q", after)
			}
		})
	}
}

// TestLLMConfigPutInvalid: an unknown mode is a 400 with a pt-BR message, and
// nothing is written.
func TestLLMConfigPutInvalid(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/config/llm", `{"mode":"nope"}`, &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR error message", got)
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.LLM.Mode != "" {
		t.Fatalf("invalid PUT wrote mode %q, want unchanged", cfg.LLM.Mode)
	}
}

// TestLLMConfigGetLiveQuery (F-0224 criterion 3): GET /api/config/llm reports
// whether the chat agent has live read access to cerne — true only in
// claude-cli's agentic mode, false for byo-key (static context) and the
// delegation default.
func TestLLMConfigGetLiveQuery(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"claude-cli", true},
		{"byo-key", false},
		{"", false},
	}
	for _, tt := range tests {
		repoDir, projDir := newBackend(t)
		if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b",
			LLM: project.LLMConfig{Mode: tt.mode}}); err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}
		var got struct {
			LiveQuery bool `json:"live_query"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/config/llm", "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("mode %q: status = %d; body: %s", tt.mode, rec.Code, rec.Body.String())
		}
		if got.LiveQuery != tt.want {
			t.Fatalf("mode %q: live_query = %v, want %v", tt.mode, got.LiveQuery, tt.want)
		}
	}
}

// TestLLMConfigListsProvidersAndPreferred: criterion 14 — GET lists the
// providers the project already reaches (none in delegation) and the global
// preferred one; it never offers to register a provider.
func TestLLMConfigListsProvidersAndPreferred(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, models.FileName), []byte(`{"preferred_provider":"anthropic","entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		mode string
		want []string
	}{
		{"", nil},
		{"claude-cli", []string{"anthropic"}},
		{"byo-key", []string{"anthropic"}},
	}
	for _, tt := range tests {
		repoDir, projDir := newBackend(t)
		if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b",
			LLM: project.LLMConfig{Mode: tt.mode}}); err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}
		var got struct {
			Providers         []string `json:"providers"`
			PreferredProvider string   `json:"preferred_provider"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/config/llm", "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("mode %q: status = %d; body: %s", tt.mode, rec.Code, rec.Body.String())
		}
		if strings.Join(got.Providers, ",") != strings.Join(tt.want, ",") {
			t.Fatalf("mode %q: providers = %v, want %v", tt.mode, got.Providers, tt.want)
		}
		if got.PreferredProvider != "anthropic" {
			t.Fatalf("mode %q: preferred = %q, want anthropic", tt.mode, got.PreferredProvider)
		}
	}
}

// TestPreferredProviderPersists: criterion 16 — the choice lands in the global
// file, so a fresh Load (a restarted process) still sees it; a provider the
// project does not reach is refused and leaves the file alone.
func TestPreferredProviderPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", dir)
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b",
		LLM: project.LLMConfig{Mode: "byo-key"}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	h := New(repoDir, projDir, "")

	rec := doJSON(t, h, http.MethodPut, "/api/config/models/provider", `{"provider":"openai"}`, new(map[string]any))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unconfigured provider: status = %d, want 400", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, models.FileName)); !os.IsNotExist(err) {
		t.Fatalf("refused PUT touched the catalog file: %v", err)
	}

	rec = doJSON(t, h, http.MethodPut, "/api/config/models/provider", `{"provider":"anthropic"}`, new(map[string]any))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	cat, warn := models.Load(dir)
	if warn != nil || cat.PreferredProvider != "anthropic" {
		t.Fatalf("after restart: preferred = %q (warn %v), want anthropic", cat.PreferredProvider, warn)
	}
}

// TestAbsentCatalogLoggedOncePerProcess: C10 — no global file is the
// permanent state for many users; it is logged once per process, not on
// every chat options request.
func TestAbsentCatalogLoggedOncePerProcess(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	absentCatalogLogged = sync.Once{}
	var logs bytes.Buffer
	prev := telemetry.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { telemetry.SetLogger(prev) })

	repoDir, projDir := newBackend(t)
	h := New(repoDir, projDir, "")
	for range 3 {
		if rec := doJSON(t, h, http.MethodGet, "/api/config/llm", "", new(map[string]any)); rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if n := strings.Count(logs.String(), "no global file"); n != 1 {
		t.Fatalf("absent-file line logged %d times, want once:\n%s", n, logs.String())
	}
}

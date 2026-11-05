package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

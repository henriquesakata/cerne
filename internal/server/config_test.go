package server

import (
	"net/http"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
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

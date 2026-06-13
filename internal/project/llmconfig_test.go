package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigLLMMode(t *testing.T) {
	dir := t.TempDir()
	body := "project_id = \"x\"\nbackend = \"/tmp/b\"\n\n[llm]\nmode = \"delegation\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(body), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	cfg, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.LLM.Mode != "delegation" {
		t.Fatalf("LLM.Mode = %q, want delegation", cfg.LLM.Mode)
	}
}

func TestLoadConfigNoLLMBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte("project_id = \"x\"\nbackend = \"/tmp/b\"\n"), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	cfg, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.LLM.Mode != "" {
		t.Fatalf("LLM.Mode = %q, want empty (ResolveMode defaults it)", cfg.LLM.Mode)
	}
}

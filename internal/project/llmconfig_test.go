package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/models"
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

// TestLoadConfigLLMByoKeyRoundTrip persists every byo-key [llm] field and reads
// it back unchanged, so the TOML tags map the whole block.
func TestLoadConfigLLMByoKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := "project_id = \"x\"\nbackend = \"/tmp/b\"\n\n" +
		"[llm]\n" +
		"mode = \"byo-key\"\n" +
		"provider = \"anthropic\"\n" +
		"model = \"claude-opus-4-8\"\n" +
		"key_env = \"MY_KEY\"\n" +
		"base_url = \"http://proxy.local\"\n" +
		"max_tokens = 1234\n" +
		"claude_bin = \"/opt/claude\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(body), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	cfg, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	want := LLMConfig{Mode: "byo-key", Provider: "anthropic", Model: "claude-opus-4-8",
		KeyEnv: "MY_KEY", BaseURL: "http://proxy.local", MaxTokens: 1234, ClaudeBin: "/opt/claude"}
	if cfg.LLM != want {
		t.Fatalf("LLM = %+v, want %+v", cfg.LLM, want)
	}
}

// TestLLMConfigWithDefaults checks the documented defaults fill only the empty
// fields and that explicit values survive.
func TestLLMConfigWithDefaults(t *testing.T) {
	got := LLMConfig{Mode: "byo-key"}.WithDefaults()
	want := LLMConfig{Mode: "byo-key", Provider: "anthropic", Model: models.Baseline().DefaultModel,
		KeyEnv: "CERNE_LLM_API_KEY", BaseURL: "https://api.anthropic.com", MaxTokens: 8000,
		ClaudeBin: "claude"}
	if got != want {
		t.Fatalf("withDefaults() on empty = %+v, want %+v", got, want)
	}

	custom := LLMConfig{Mode: "byo-key", Provider: "anthropic", Model: "claude-x",
		KeyEnv: "MY_KEY", BaseURL: "http://proxy.local", MaxTokens: 42, ClaudeBin: "/opt/claude"}
	if got := custom.WithDefaults(); got != custom {
		t.Fatalf("withDefaults() overrode explicit values: %+v, want %+v", got, custom)
	}
}

// TestLLMConfigClaudeBinDefault checks claude_bin defaults to "claude" for the
// claude-cli provider when the field is empty.
func TestLLMConfigClaudeBinDefault(t *testing.T) {
	got := LLMConfig{Mode: "claude-cli"}.WithDefaults()
	if got.ClaudeBin != "claude" {
		t.Fatalf("ClaudeBin default = %q, want \"claude\"", got.ClaudeBin)
	}
}

// TestLLMConfigNoSecretField asserts the struct carries only the env-var NAME
// (key_env), never the secret itself — the secret must never land in .cernerc.
// It bans fields whose name implies they hold the key value (apikey, secret,
// token, key, password); KeyEnv is allowed because it stores only the env name.
func TestLLMConfigNoSecretField(t *testing.T) {
	banned := map[string]bool{
		"apikey": true, "secret": true, "token": true,
		"key": true, "password": true, "apisecret": true,
	}
	typ := reflect.TypeOf(LLMConfig{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.ToLower(field.Name)
		if name == "keyenv" {
			continue // stores the env var name, not the secret
		}
		if banned[name] {
			t.Fatalf("LLMConfig has a secret-bearing field %q; secrets must live only in the env named by key_env", field.Name)
		}
	}
}

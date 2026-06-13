package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fresh `cerne init` must not write an empty [llm] block — otherwise a user
// who later adds the documented [llm] byo-key block to .cernerc ends up with a
// duplicate TOML table, which fails to parse.
func TestSaveConfigOmitsEmptyLLM(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "x", Backend: "b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[llm]") {
		t.Fatalf(".cernerc should omit the empty [llm] block:\n%s", data)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.LLM != (LLMConfig{}) {
		t.Fatalf("LLM should be zero, got %+v", got.LLM)
	}
}

// When the user configures byo-key, the [llm] block is written and round-trips.
func TestSaveConfigWritesLLMWhenSet(t *testing.T) {
	dir := t.TempDir()
	in := Config{ProjectID: "x", Backend: "b", LLM: LLMConfig{Mode: "byo-key", BaseURL: "http://h"}}
	if err := SaveConfig(dir, in); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[llm]") || !strings.Contains(string(data), "byo-key") {
		t.Fatalf("expected [llm] block with byo-key:\n%s", data)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.LLM.Mode != "byo-key" || got.LLM.BaseURL != "http://h" {
		t.Fatalf("round-trip mismatch: %+v", got.LLM)
	}
}

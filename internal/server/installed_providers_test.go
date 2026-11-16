package server

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// stubInstalledCLIs makes the PATH probe report exactly these subscription CLIs.
func stubInstalledCLIs(t *testing.T, found ...string) {
	t.Helper()
	orig := project.FoundSubscriptionCLIs
	t.Cleanup(func() { project.FoundSubscriptionCLIs = orig })
	project.FoundSubscriptionCLIs = func() []string { return found }
}

// TestLLMConfigListsInstalledCLIs (F-0440 criterion 1): the configured mode's
// vendor first, then one per installed subscription CLI, never repeated; the
// remote serve keeps only the configured one.
func TestLLMConfigListsInstalledCLIs(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex", "cursor-agent")
	cfg := project.LLMConfig{Mode: "claude-cli"}
	if got := strings.Join(llmConfig(cfg, false).Providers, ","); got != "anthropic,openai,cursor" {
		t.Fatalf("local providers = %q, want anthropic,openai,cursor", got)
	}
	if got := strings.Join(llmConfig(cfg, true).Providers, ","); got != "anthropic" {
		t.Fatalf("remote providers = %q, want anthropic", got)
	}
	// byo-key with an installed CLI of the same vendor: no duplicate.
	if got := strings.Join(llmConfig(project.LLMConfig{Mode: "byo-key"}, false).Providers, ","); got != "anthropic,openai,cursor" {
		t.Fatalf("byo-key providers = %q, want anthropic,openai,cursor", got)
	}
}

// TestPreferredProviderAcceptsInstalledCLI (criterion 5): the PUT takes an
// installed CLI's vendor and leaves .cernerc byte for byte as it was.
func TestPreferredProviderAcceptsInstalledCLI(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b", LLM: project.LLMConfig{Mode: "claude-cli"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	h := New(repoDir, projDir, "")
	if rec := doJSON(t, h, http.MethodPut, "/api/config/models/provider", `{"provider":"openai"}`, new(map[string]any)); rec.Code != http.StatusOK {
		t.Fatalf("installed codex: status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/config/models/provider", `{"provider":"gemini"}`, new(map[string]any)); rec.Code != http.StatusBadRequest {
		t.Fatalf("not installed gemini: status = %d, want 400", rec.Code)
	}
	after, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	if !bytes.Equal(before, after) {
		t.Fatalf(".cernerc changed:\n%s\n---\n%s", before, after)
	}
}

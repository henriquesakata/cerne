package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

func chatFixture(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newFeatureBackend(t)
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Conciliação bancária", State: domain.StateImplemented},
		{ID: "F-0002", Title: "Rejeitada", State: domain.StateRejected},
	} {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	if err := store.SaveCapability(repoDir, store.Capability{ID: "C-0001", Title: "Financeiro", State: domain.CapabilityActive}); err != nil {
		t.Fatalf("SaveCapability: %v", err)
	}
	commitBackend(t, repoDir)
	return repoDir, projDir
}

// TestBuildChatSystemPromptDefaultPersona: no [agent] block → the default
// neutral persona is used, and the roster + capability tree + canonical
// sections still show up (criterion 2).
func TestBuildChatSystemPromptDefaultPersona(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{})
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	if !strings.Contains(prompt, DefaultAgentPersona) {
		t.Fatalf("prompt missing the default persona:\n%s", prompt)
	}
	if !strings.Contains(prompt, "F-0001") || !strings.Contains(prompt, "Conciliação bancária") {
		t.Fatalf("prompt missing the live feature roster:\n%s", prompt)
	}
	if strings.Contains(prompt, "F-0002") {
		t.Fatalf("prompt must not roster a rejected feature:\n%s", prompt)
	}
	if !strings.Contains(prompt, "C-0001") || !strings.Contains(prompt, "Financeiro") {
		t.Fatalf("prompt missing the capability tree:\n%s", prompt)
	}
	for _, s := range spectmpl.Sections {
		if !strings.Contains(prompt, s) {
			t.Fatalf("prompt missing canonical section %q:\n%s", s, prompt)
		}
	}
	if !strings.Contains(prompt, "cerne-proposta") {
		t.Fatalf("prompt missing the proposal contract marker:\n%s", prompt)
	}
}

// TestBuildChatSystemPromptInlineProfile: [agent] profile inline text
// replaces the default persona.
func TestBuildChatSystemPromptInlineProfile(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	cfg := project.Config{Agent: project.AgentConfig{Profile: "Você é um analista financeiro sênior."}}

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cfg)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	if !strings.Contains(prompt, "Você é um analista financeiro sênior.") {
		t.Fatalf("prompt missing the inline persona:\n%s", prompt)
	}
	if strings.Contains(prompt, DefaultAgentPersona) {
		t.Fatalf("prompt must not also carry the default persona:\n%s", prompt)
	}
}

// TestBuildChatSystemPromptProfileFile: [agent] profile_file content is read
// relative to projDir and used as the persona.
func TestBuildChatSystemPromptProfileFile(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	if err := os.WriteFile(filepath.Join(projDir, "persona.md"), []byte("Persona do arquivo do projeto."), 0o644); err != nil {
		t.Fatalf("write persona.md: %v", err)
	}
	cfg := project.Config{Agent: project.AgentConfig{ProfileFile: "persona.md"}}

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cfg)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	if !strings.Contains(prompt, "Persona do arquivo do projeto.") {
		t.Fatalf("prompt missing the profile_file persona:\n%s", prompt)
	}
}

// TestBuildChatSystemPromptBadProfileFile: a configured profile_file that
// cannot be read is a real error, not a silent fallback.
func TestBuildChatSystemPromptBadProfileFile(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	cfg := project.Config{Agent: project.AgentConfig{ProfileFile: "nope.md"}}

	if _, err := BuildChatSystemPrompt(repoDir, projDir, cfg); err == nil {
		t.Fatalf("BuildChatSystemPrompt: want error for missing profile_file, got nil")
	}
}

// TestBuildChatPrompt: the transcript is flattened, in order, with roles
// labeled, ending on a cue for the agent to continue.
func TestBuildChatPrompt(t *testing.T) {
	system := "SISTEMA: você é um agente de domínio."
	msgs := []ChatMessage{
		{Role: "user", Content: "Precisamos de um jeito de exportar relatórios."},
		{Role: "assistant", Content: "Em qual formato?"},
		{Role: "user", Content: "CSV."},
	}
	prompt := BuildChatPrompt(system, msgs)

	if !strings.HasPrefix(prompt, system) {
		t.Fatalf("prompt must start with the system prompt:\n%s", prompt)
	}
	iUser1 := strings.Index(prompt, "Precisamos de um jeito de exportar relatórios.")
	iAsst := strings.Index(prompt, "Em qual formato?")
	iUser2 := strings.Index(prompt, "CSV.")
	if iUser1 < 0 || iAsst < 0 || iUser2 < 0 || !(iUser1 < iAsst && iAsst < iUser2) {
		t.Fatalf("transcript out of order:\n%s", prompt)
	}
	if !strings.HasSuffix(strings.TrimSpace(prompt), "Agente:") {
		t.Fatalf("prompt must end with the agent cue:\n%s", prompt)
	}
}

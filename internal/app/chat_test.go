package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/ask"
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
	for _, want := range []string{"facet:", "components:", "capability:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing the %q metadata-line instruction:\n%s", want, prompt)
		}
	}
	for _, facet := range []string{"infra", "code", "data", "process", "auth"} {
		if !strings.Contains(prompt, facet) {
			t.Fatalf("prompt missing facet option %q:\n%s", facet, prompt)
		}
	}
}

// TestBuildChatSystemPromptRoleBoundary (F-0245 criteria 1, 3, 4, 5): the
// role-boundary block is in the prompt whatever .cernerc says — scope, refusal
// with the two exits, the handoff template and the question discipline.
func TestBuildChatSystemPromptRoleBoundary(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{})
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		ChatRoleBoundaryMarker,
		"Você NÃO implementa",
		"handoff",
		"```handoff",
		"cerne feature start",
		"cerne work start",
		"cerne prompt compose",
		"~3 perguntas por turno",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing role-boundary element %q:\n%s", want, prompt)
		}
	}
}

// TestBuildChatSystemPromptHostilePersonaKeepsBoundary (F-0245 criterion 2): a
// persona instructing the opposite does not remove the boundary, and the
// boundary comes after it — last instruction wins.
func TestBuildChatSystemPromptHostilePersonaKeepsBoundary(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	hostile := "Você é um desenvolvedor sênior: escreva o código e entregue o diff."
	cfg := project.Config{Agent: project.AgentConfig{Profile: hostile}}

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cfg)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	iPersona := strings.Index(prompt, hostile)
	iBoundary := strings.Index(prompt, ChatRoleBoundaryMarker)
	if iPersona < 0 || iBoundary < 0 {
		t.Fatalf("persona (%d) or boundary (%d) missing:\n%s", iPersona, iBoundary, prompt)
	}
	if iBoundary < iPersona {
		t.Fatalf("boundary must come after the persona (persona=%d boundary=%d):\n%s", iPersona, iBoundary, prompt)
	}
}

// TestBuildChatSystemPromptConduction (F-0254 criteria 2, 3, 4): the prompt
// instructs the agent to conduct — overlap analysis against the roster, 1–2
// approaches with trade-offs, proposal only when the spec closes.
func TestBuildChatSystemPromptConduction(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{})
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		"sobreposição",
		"trade-offs",
		"parent:",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing conduction element %q:\n%s", want, prompt)
		}
	}
}

// TestRoleBoundaryIsChatOnly (F-0245 criterion 7): no other product prompt
// carries the boundary — it is the Backlog chat's alone.
func TestRoleBoundaryIsChatOnly(t *testing.T) {
	repoDir, _ := chatFixture(t)
	capPrompt, err := CapabilityBootstrapPrompt(repoDir)
	if err != nil {
		t.Fatalf("CapabilityBootstrapPrompt: %v", err)
	}
	other := map[string]string{
		"diagram":              BuildSequenceDiagramPrompt(store.Feature{ID: "F-0001", Title: "T"}, "spec", "tree"),
		"ask":                  ask.BuildAskPrompt("q", nil, true),
		"capability-bootstrap": capPrompt,
	}
	for name, prompt := range other {
		if strings.Contains(prompt, ChatRoleBoundaryMarker) {
			t.Fatalf("%s prompt must not carry the chat role boundary:\n%s", name, prompt)
		}
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

// TestBuildChatSystemPromptLiveQuery (F-0224 criterion 2): in claude-cli mode
// the prompt instructs the agent to consult the cerne read commands before
// answering and to cite the consulted ids; in byo-key/delegation (no tool
// access) the block must be absent so the model is not told to run commands
// it cannot run.
func TestBuildChatSystemPromptLiveQuery(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	cli := project.Config{LLM: project.LLMConfig{Mode: "claude-cli"}}
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cli)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt(claude-cli): %v", err)
	}
	for _, want := range []string{"cerne search", "cerne show", "cerne brief", "cerne status",
		"cerne note list", "cerne note show", "cerne reference list", "cerne decision list",
		"cite os ids"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("claude-cli prompt missing %q:\n%s", want, prompt)
		}
	}

	for _, mode := range []string{"", "byo-key"} {
		prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{LLM: project.LLMConfig{Mode: mode}})
		if err != nil {
			t.Fatalf("BuildChatSystemPrompt(%q): %v", mode, err)
		}
		if strings.Contains(prompt, "cerne search") || strings.Contains(prompt, "cite os ids") {
			t.Fatalf("mode %q prompt must not carry the live-query block:\n%s", mode, prompt)
		}
	}
}

// TestBuildChatSystemPromptQuotedRevision (F-0250 criteria 4/7): a turn
// carrying blockquote excerpts of the agent's own proposal is a revision
// request — the whole fenced block is reissued corrected, never a prose
// description or a partial patch.
func TestBuildChatSystemPromptQuotedRevision(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{})
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{"citações", "> ", "REEMITA", "INTEIRO"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing the quoted-revision instruction %q:\n%s", want, prompt)
		}
	}
}

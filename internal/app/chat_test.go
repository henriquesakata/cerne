package app

import (
	"errors"
	"github.com/henriquesakata/cerne/internal/clarify"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/ask"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
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

// TestBuildChatSystemPromptGauntlet pins F-0270: the analyst's prompt carries
// the Gauntlet protocol — demand a named/fetchable/comparable quality bar,
// refuse vague bars, materialize bar + blind comparison + exit condition in
// the proposal's Verificação, register external bars as references, and record
// an explicit human opt-out instead of silently omitting the bar.
func TestBuildChatSystemPromptGauntlet(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		ChatGauntletMarker,
		"NAMED",                             // bar must be a named real thing
		"vague bar",                         // vague bar is refused
		doclang.Examples(doclang.PT).Method, // blind side-by-side is the method, in the project's language
		doclang.Examples(doclang.PT).Exit,   // exit = win/tie, never a fixed round count
		"cerne reference new",
		"Barra: dispensada por decisão humana",             // the opt-out marker (F-0270 criterion 1)
		"never suggest the waiver",                         // opt-out is human-initiated, never agent-solicited
		doclang.Examples(doclang.PT).BarExample,            // one filled-in positive example anchors the format
		"Barra: não aplicável",                             // the confirmed-classification marker (criterion 4)
		"the human approve is its acceptance",              // the agent's bar is a proposal; approve accepts it (F-0308)
		"feature approve <id> --check",                     // readiness is checked before asking for the approve
		"padrão alternativo: <teste/benchmark/invariante>", // no comparable artifact → alternative standard, in the project's form
		"a proposal without them is incomplete",            // the binding line next to the proposal template
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing gauntlet instruction %q:\n%s", want, prompt)
		}
	}
	// F-0308 crit. 10: no acceptance round-trip before the bar enters the proposal.
	for _, gone := range []string{"exige aceite explícito do humano antes de entrar na proposta", "CONFIRME com o humano"} {
		if strings.Contains(prompt, gone) {
			t.Fatalf("prompt still carries the old protocol %q", gone)
		}
	}
	// The protocol governs elaboration: it must come after the conduction block
	// and before the role boundary, which stays last-word on role conflicts.
	gauntlet := strings.Index(prompt, ChatGauntletMarker)
	boundary := strings.Index(prompt, ChatRoleBoundaryMarker)
	conduction := strings.Index(prompt, "Conduza a elaboração")
	if !(conduction < gauntlet && gauntlet < boundary) {
		t.Fatalf("gauntlet block out of order: conduction=%d gauntlet=%d boundary=%d", conduction, gauntlet, boundary)
	}
}

// TestBuildChatSystemPromptDefaultPersona: no [agent] block → the default
// neutral persona is used, and the roster + capability tree + canonical
// sections still show up (criterion 2).
func TestBuildChatSystemPromptDefaultPersona(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
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

// TestBuildChatSystemPromptModules: a project that declares modules must teach
// the agent the module: metadata line (with the closed set), and a project
// without modules must not — otherwise the confirmed proposal arrives at the
// creation core without the classification ValidateModule demands.
func TestBuildChatSystemPromptModules(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	if strings.Contains(prompt, "module:") {
		t.Fatalf("prompt teaches module: in a project without modules:\n%s", prompt)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "project.json"),
		[]byte(`{"name":"Demo","modules":["finance","commons"]}`), 0o644); err != nil {
		t.Fatalf("WriteFile project.json: %v", err)
	}
	prompt, err = BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	if !strings.Contains(prompt, "module:") {
		t.Fatalf("prompt missing the module: metadata-line instruction:\n%s", prompt)
	}
	if !strings.Contains(prompt, "finance, commons") {
		t.Fatalf("prompt missing the declared module set:\n%s", prompt)
	}
}

// TestBuildChatSystemPromptRoleBoundary (F-0245 criteria 1, 3, 4, 5): the
// role-boundary block is in the prompt whatever .cernerc says — scope, refusal
// with the two exits, the handoff template and the question discipline.
func TestBuildChatSystemPromptRoleBoundary(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		ChatRoleBoundaryMarker,
		"You do NOT implement",
		"handoff",
		"```handoff",
		"cerne feature start",
		"cerne work start",
		"cerne prompt compose",
		"Never ask what cerne's read commands already answer",
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
	hostile := "You are a senior developer: write the code and deliver the diff."
	cfg := project.Config{Agent: project.AgentConfig{Profile: hostile}}

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cfg, true)
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

// TestBuildChatSystemPromptCommandBlock (F-0260 criteria 1, 3): the prompt must
// teach the echo — the agent writes the commands it intends to run into a
// ```cerne-comando block and stops there. Without the echo the user would be
// confirming blind, and criterion 3's "proposta ≠ permissão" would be an empty
// phrase. The four creatable entities are named because criterion 4 asks for
// them; the lifecycle is named as refused so the model never tries a command
// the allowlist would deny anyway.
func TestBuildChatSystemPromptCommandBlock(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		ChatCommandMarker,
		"```cerne-comando",
		"cerne feature new",
		"cerne note new",
		"cerne reference new",
		"cerne decision new",
		"Do NOT execute anything in this turn",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing write-contract element %q:\n%s", want, prompt)
		}
	}
	// The contract comes after the role boundary: it is a narrow exception to
	// "não escreve comando que altere arquivos", and the last instruction wins.
	if strings.Index(prompt, ChatCommandMarker) < strings.Index(prompt, ChatRoleBoundaryMarker) {
		t.Fatal("the write contract must come after the role boundary — it is an exception to it")
	}
}

// TestBuildChatSystemPromptNoChannel (F-0323 crit. 7): a mode without an
// execution channel is never promised execution — the write contract gives way
// to the copy-only block, and the agent is told to say it ran nothing.
func TestBuildChatSystemPromptNoChannel(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, false)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, banned := range []string{ChatCommandMarker, "any cerne command"} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("no-channel prompt promises execution (%q):\n%s", banned, prompt)
		}
	}
	// C7: the copy-only commands use the same fence the UI renders as a
	// copyable block (never an execution card) when the turn has no channel.
	for _, want := range []string{ChatNoChannelMarker, "```" + ChatCommandFence, "did not execute anything"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("no-channel prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestBuildChatSystemPromptConduction (F-0254 criteria 2, 3, 4): the prompt
// instructs the agent to conduct — overlap analysis against the roster, 1–2
// approaches with trade-offs, proposal only when the spec closes.
func TestBuildChatSystemPromptConduction(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		"overlap",
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
		"diagram":              BuildSequenceDiagramPrompt(doclang.PT, store.Feature{ID: "F-0001", Title: "T"}, "spec", "tree"),
		"ask":                  ask.BuildAskPrompt(doclang.PT, "q", nil, true),
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

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cfg, true)
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

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cfg, true)
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

	if _, err := BuildChatSystemPrompt(repoDir, projDir, cfg, true); err == nil {
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
	if !strings.HasSuffix(strings.TrimSpace(prompt), "Agent:") {
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
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cli, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt(claude-cli): %v", err)
	}
	for _, want := range []string{"cerne search", "cerne show", "cerne brief", "cerne status",
		"cerne note list", "cerne note show", "cerne reference list", "cerne decision list",
		"cite the ids"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("claude-cli prompt missing %q:\n%s", want, prompt)
		}
	}

	for _, mode := range []string{"", "byo-key"} {
		prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{LLM: project.LLMConfig{Mode: mode}}, true)
		if err != nil {
			t.Fatalf("BuildChatSystemPrompt(%q): %v", mode, err)
		}
		if strings.Contains(prompt, "cerne search") || strings.Contains(prompt, "cite the ids") {
			t.Fatalf("mode %q prompt must not carry the live-query block:\n%s", mode, prompt)
		}
	}
}

// TestChatSystemPromptDoesNotDenyWrite (F-0267): the read block used to close
// with "Comandos de escrita não estão disponíveis" — true when F-0224 wrote it,
// false since F-0260 gave the confirmed turn write tools and F-0265 added the
// update verbs. Two contradictory sentences in one prompt is a text bug, not a
// question of ordering: in real use the agent anchored on the denial and told
// the user it could not update a spec, offering a handoff instead of echoing
// `cerne feature spec`. The prompt must announce the write once, correctly.
func TestChatSystemPromptDoesNotDenyWrite(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir,
		project.Config{LLM: project.LLMConfig{Mode: "claude-cli"}}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, denial := range []string{
		"Comandos de escrita não estão disponíveis",
		"não estão disponíveis",
	} {
		if strings.Contains(prompt, denial) {
			t.Fatalf("prompt denies the write capability with %q — the agent has it under confirmation:\n%s", denial, prompt)
		}
	}
	// The read block points at the contract instead of denying (criterion 2),
	// and the update verbs are named so "atualiza a spec da F-NNNN" routes to a
	// verb rather than to a file edit + pull/push (criterion 3).
	for _, want := range []string{
		"only under user confirmation",
		"cerne feature spec",
		"cerne feature retag",
		// F-0268: module has its own verb, and the prompt says so — the agent
		// guessed `retag --module` (a flag that does not exist) and handed the
		// user a command the CLI would refuse.
		"cerne classify",
		"retag has no",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestChatSystemPromptStatesScopeAsRule (F-0269 criterion 8): the contract must
// describe the reach by RULE — any cerne command, lifecycle refused — instead of
// reading as a closed list of verbs. The enumeration is what made the agent
// treat an absent verb as "impossible" and invent a flag (or hand the chore
// back to the user) three times running. It must also say that asking the user
// for standing permission is the wrong move: the grant is per turn, by design.
func TestChatSystemPromptStatesScopeAsRule(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir,
		project.Config{LLM: project.LLMConfig{Mode: "claude-cli"}}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		"any cerne command",
		"EXAMPLES",
		"permanent permission",
		"Outside cerne you have nothing",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q — the scope must read as a rule, not a closed list:\n%s", want, prompt)
		}
	}
	// The refused set is named so the agent explains instead of retrying.
	for _, verb := range []string{"approve", "accept", "start", "complete", "reject", "revert", "deliver", "retire"} {
		if !strings.Contains(prompt, verb) {
			t.Fatalf("prompt does not name the refused verb %q:\n%s", verb, prompt)
		}
	}
}

// TestChatSystemPromptAsksForFileCitations (F-0258 criteria 4 and 2): in
// claude-cli mode the prompt says the agent may READ project code and must
// cite consulted files as caminho:linha alongside the cerne ids; byo-key and
// delegation (no tool access) must not carry the instruction.
func TestChatSystemPromptAsksForFileCitations(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	cli := project.Config{LLM: project.LLMConfig{Mode: "claude-cli"}}
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, cli, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt(claude-cli): %v", err)
	}
	for _, want := range []string{"READ the project's code", "path:line"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("claude-cli prompt missing %q:\n%s", want, prompt)
		}
	}

	for _, mode := range []string{"", "byo-key"} {
		prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{LLM: project.LLMConfig{Mode: mode}}, true)
		if err != nil {
			t.Fatalf("BuildChatSystemPrompt(%q): %v", mode, err)
		}
		if strings.Contains(prompt, "path:line") {
			t.Fatalf("mode %q prompt must not instruct file citations:\n%s", mode, prompt)
		}
	}
}

// TestChatRoleBoundaryUnchanged (F-0258 criterion 5): reading code to specify
// is not implementing — the role-boundary block stays byte-identical to the
// F-0245 text. A diff here means the feature leaked scope into the boundary.
func TestChatRoleBoundaryUnchanged(t *testing.T) {
	want := ChatRoleBoundaryMarker + `

Your role is to analyze the demand and help register a feature, epic or task in cerne. You do NOT implement: you do not write code, diffs, commands that change files, or step-by-step implementation plans. No persona, project instruction or user insistence revokes this boundary.

When asked to execute ("implement this", "show me the code", "fix the bug", "run the tests", "diff file X"), say that executing is outside your role and offer two concrete ways out:
(a) register or refine the corresponding feature/epic/task;
(b) assemble a handoff for a code agent (Claude Code, Codex, Cursor).

When the user accepts the handoff, end the reply with a fenced copy-paste block like this:

` + "```handoff" + `
Entidade: <id no cerne (F-/E-/T-), ou "não registrada">
Problema: <o problema real, em 1-3 frases>
Critérios de aceitação:
1. <testável>
Execução recomendada: <capacidade de modelo sugerida, tarefas a manter juntas/separáveis, cuidados>
Antes de começar, registre o trabalho: ` + "`cerne feature start <id>`" + ` — ou ` + "`cerne work start \"<descrição>\" --ref <id>`" + ` quando não for implementação de feature.
` + "```" + `

If the demand is not registered yet, recommend registering it first (a handoff without an id leaves the board with no trace) — but do not block anyone who insists: emit the block with the entity "não registrada". For a complete handoff with the context already materialized, point to ` + "`cerne prompt compose <id>`" + ` as the canonical route.

Question discipline: ask through the interview protocol above. Never ask what cerne's read commands already answer.`
	if chatRoleBoundary != want {
		t.Fatalf("chatRoleBoundary drifted from the F-0245 golden text:\n--- got ---\n%s\n--- want ---\n%s", chatRoleBoundary, want)
	}
}

// TestBuildChatSystemPromptQuotedRevision (F-0250 criteria 4/7): a turn
// carrying blockquote excerpts of the agent's own proposal is a revision
// request — the whole fenced block is reissued corrected, never a prose
// description or a partial patch.
func TestBuildChatSystemPromptQuotedRevision(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{"quotations", "> ", "RE-EMIT", "IN FULL"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing the quoted-revision instruction %q:\n%s", want, prompt)
		}
	}
}

// TestBuildRefineBlock (F-0256 criteria 1/2): refining an existing candidate
// hands the agent the id, the title and the spec as it is in the backend right
// now, plus the instruction to re-emit the proposal as a revision of that id.
func TestBuildRefineBlock(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Exportar relatórios", "", "", nil, nil,
		"## Problema\n\nFalta exportação em CSV.", false, "", "")
	if err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}

	block, err := BuildRefineBlock(repoDir, f.ID)
	if err != nil {
		t.Fatalf("BuildRefineBlock: %v", err)
	}
	for _, want := range []string{f.ID, "Exportar relatórios", "Falta exportação em CSV", "cerne-proposta"} {
		if !strings.Contains(block, want) {
			t.Fatalf("refine block missing %q:\n%s", want, block)
		}
	}

	// Criterion 6: the block is rebuilt from the backend, so a spec that moved
	// under the conversation is reflected on the next turn — no stale snapshot.
	if _, _, err := UpdateCandidateSpec(repoDir, projDir, f.ID, "## Problema\n\nExportar também em XLSX.\n"); err != nil {
		t.Fatalf("UpdateCandidateSpec: %v", err)
	}
	block2, err := BuildRefineBlock(repoDir, f.ID)
	if err != nil {
		t.Fatalf("BuildRefineBlock (2): %v", err)
	}
	if !strings.Contains(block2, "XLSX") || strings.Contains(block2, "Falta exportação em CSV") {
		t.Fatalf("refine block did not reload the current spec:\n%s", block2)
	}
}

// TestBuildRefineBlockNotCandidate (F-0256 criterion 5, moved past approval by
// F-0318): in development the chat does not refine — the refusal lives in the
// core, not only in the UI.
func TestBuildRefineBlockNotCandidate(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := BuildRefineBlock(repoDir, "F-0001"); !errors.Is(err, ErrNotCandidate) {
		t.Fatalf("err = %v, want ErrNotCandidate", err)
	}
	if _, err := BuildRefineBlock(repoDir, "F-9999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

// TestBuildChatSystemPromptNoteContract (F-0249 criteria 1/6/7): the prompt
// teaches the second fenced block — when to propose durable knowledge, the
// duplicate check against the live notes, and the informs backlink that keeps
// the conversa → nota → feature trail.
func TestBuildChatSystemPromptNoteContract(t *testing.T) {
	repoDir, projDir := chatFixture(t)

	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	for _, want := range []string{
		"cerne-nota",   // the fence itself
		"informs:",     // criterion 7: the backlink line
		"note list",    // criterion 6: check before proposing
		"vocabulary",   // criterion 1: what counts as durable knowledge
		"confirmation", // criterion 4: nothing is written without the click
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing the note-contract marker %q:\n%s", want, prompt)
		}
	}
}

// TestBuildChatSystemPromptIdeasOnDemand (F-0312 crit. 9, C8): no mode puts
// an idea in the prompt, and the on-demand read is taught only where the mode
// can run it — claude-cli directly, a byo-key tool channel through the
// confirmed card — never in delegation or without a channel (F-0224 crit. 3).
func TestBuildChatSystemPromptIdeasOnDemand(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	if err := store.SaveIdea(repoDir, store.Idea{ID: "I-0001", Title: "conciliação via planilha", State: domain.IdeaActive}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	for _, tc := range []struct {
		mode    string
		channel bool
		want    string // "" = no ideas read taught at all
	}{
		{"claude-cli", false, chatIdeasPolicy},
		{"claude-cli", true, chatIdeasPolicy},
		{"byo-key", true, chatIdeasCardPolicy},
		{"byo-key", false, ""},
		{"delegation", false, ""},
		{"", false, ""},
	} {
		prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{LLM: project.LLMConfig{Mode: tc.mode}}, tc.channel)
		if err != nil {
			t.Fatalf("%q: %v", tc.mode, err)
		}
		if strings.Contains(prompt, "I-0001") || strings.Contains(prompt, "via planilha") {
			t.Fatalf("%q channel=%v: an idea leaked into the prompt", tc.mode, tc.channel)
		}
		if tc.want == "" {
			if strings.Contains(prompt, "cerne idea list") {
				t.Fatalf("%q channel=%v: taught a read the mode cannot run:\n%s", tc.mode, tc.channel, prompt)
			}
			continue
		}
		if !strings.Contains(prompt, tc.want) {
			t.Fatalf("%q channel=%v: want the policy %q", tc.mode, tc.channel, tc.want)
		}
	}
	if !strings.Contains(chatIdeasCardPolicy, "cerne idea list --search") || !strings.Contains(chatIdeasCardPolicy, "card") {
		t.Fatalf("the card policy must name the command and the card: %q", chatIdeasCardPolicy)
	}
	cli, _ := BuildChatSystemPrompt(repoDir, projDir, project.Config{LLM: project.LLMConfig{Mode: "claude-cli"}}, false)
	_, rest, _ := strings.Cut(cli, "Allowed commands:")
	if allowed, _, _ := strings.Cut(rest, ". When"); !strings.Contains(allowed, "cerne idea list") {
		t.Fatalf("claude-cli allowed commands lack idea list: %q", allowed)
	}
}

// The analyst is told to write one of the prompt's Barra: lines; copied
// verbatim, each is still a template and the approve refuses it — the filled
// example sits on a line of its own, outside the Barra: value (F-0327).
func TestChatPromptBarTemplatesRefusedVerbatim(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	n := 0
	for _, line := range strings.Split(prompt, "\n") {
		if !strings.HasPrefix(line, "Barra:") {
			continue
		}
		n++
		if bar, err := doccheck.SpecBar([]byte("# t\n\n" + line + "\n")); err == nil {
			t.Errorf("template line copied verbatim is accepted as %q", bar)
		}
	}
	if n == 0 {
		t.Fatal("prompt has no Barra: template line")
	}
}

// TestBuildChatSystemPrompt_FourBacktickContract (F-0435): the proposal opens
// and closes with four backticks so code blocks inside the spec do not end it.
func TestBuildChatSystemPrompt_FourBacktickContract(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatalf("BuildChatSystemPrompt: %v", err)
	}
	if !strings.Contains(prompt, "\n````cerne-proposta\n") {
		t.Error("prompt does not open the proposal with four backticks")
	}
	if !strings.Contains(prompt, "\n````\n\nReally fill in") {
		t.Error("prompt does not close the proposal with four backticks")
	}
	if strings.Contains(prompt, "\n```cerne-proposta\n") {
		t.Error("prompt still shows a three-backtick proposal opening")
	}
}

// TestBuildChatSystemPrompt_NoteFourBackticks (F-0435): the note block uses the
// same four-backtick fence as the proposal, so its body may carry code blocks.
func TestBuildChatSystemPrompt_NoteFourBackticks(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "\n````cerne-nota\n") || !strings.Contains(prompt, "\n````\n\nWithout a facet") {
		t.Error("note block is not opened and closed with four backticks")
	}
	if strings.Contains(prompt, "\n```cerne-nota\n") {
		t.Error("prompt still shows a three-backtick note opening")
	}
}

// TestBuildChatSystemPrompt_PortugueseUnchanged (F-0433 crit. 3, F-0447): a
// backend without docs_language gets the pt-BR prompt byte for byte. Regenerate the
// golden only on a deliberate prompt change: CHAT_GOLDEN_UPDATE=1.
func TestBuildChatSystemPrompt_PortugueseUnchanged(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/chat_prompt_pt.golden"
	if os.Getenv("CHAT_GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(golden, []byte(prompt), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != string(want) {
		t.Error("the pt-BR chat prompt differs from the golden")
	}
}

func englishChatPrompt(t *testing.T) string { return chatPromptIn(t, doclang.EN) }

// chatPromptIn is the chat system prompt (with channel) of a project
// documented in l.
func chatPromptIn(t *testing.T, l doclang.Lang) string {
	t.Helper()
	repoDir, projDir := chatFixture(t)
	p, err := store.LoadProject(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	p.DocsLanguage = string(l)
	if err := store.SaveProject(repoDir, p); err != nil {
		t.Fatal(err)
	}
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, true)
	if err != nil {
		t.Fatal(err)
	}
	return prompt
}

// TestBuildChatSystemPrompt_EnglishSections (F-0433 crit. 1).
func TestBuildChatSystemPrompt_EnglishSections(t *testing.T) {
	prompt := englishChatPrompt(t)
	for _, s := range doclang.Sections(doclang.Spec, doclang.EN) {
		if !strings.Contains(prompt, "## "+s+"\n") {
			t.Errorf("prompt lacks the English section %q", s)
		}
	}
	// The handoff block is another artifact (a brief for a code agent), with
	// its own Portuguese labels: out of this feature's scope.
	rest := regexp.MustCompile("(?s)```handoff.*?\n```").ReplaceAllString(prompt, "")
	for _, s := range doclang.Sections(doclang.Spec, doclang.PT) {
		if strings.Contains(rest, s) {
			t.Errorf("prompt still names the Portuguese section %q", s)
		}
	}
}

// TestBuildChatSystemPrompt_EnglishProtocolForms (F-0433 crit. 2).
func TestBuildChatSystemPrompt_EnglishProtocolForms(t *testing.T) {
	prompt := englishChatPrompt(t)
	for _, want := range []string{"[CLARIFY:", "Bar:", "Method:", "Exit:", "Question for the judge:", "Bar: not applicable", "Bar: waived by human decision"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, bad := range []string{"[ESCLARECER:", "Barra:", "Método:", "Saída:", "não aplicável", "dispensada por decisão humana", "Pergunta ao juiz", "padrão alternativo", "Barra de elite", "O SISTEMA DEVE", "pergunta específica"} {
		if strings.Contains(prompt, bad) {
			t.Errorf("prompt still carries the Portuguese form %q", bad)
		}
	}
}

// TestBuildChatSystemPrompt_ClosesWithDirective (F-0433 crit. 4, F-0447 crit. 1):
// the prompt ends with the language directive of the project's language, so
// the last thing the model reads names the language it must write in.
func TestBuildChatSystemPrompt_ClosesWithDirective(t *testing.T) {
	for _, l := range doclang.Langs() {
		repoDir, projDir := chatFixture(t)
		p, err := store.LoadProject(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		p.DocsLanguage = string(l)
		if err := store.SaveProject(repoDir, p); err != nil {
			t.Fatal(err)
		}
		for _, channel := range []bool{true, false} {
			prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, channel)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(prompt, doclang.Directive(l)) {
				t.Errorf("%s (channel %v): the prompt does not end with the language directive", l, channel)
			}
		}
	}
}

// TestChatEnglishExamplesRoundTrip (F-0433 crit. 2): the English example lines
// the prompt teaches are read by the real readers as what they claim to be.
func TestChatEnglishExamplesRoundTrip(t *testing.T) {
	prompt := englishChatPrompt(t)
	fill := regexp.MustCompile(`<[^>]*>`)
	var bars []string
	for _, m := range regexp.MustCompile(`"(Bar: [^"\n]*)"`).FindAllStringSubmatch(prompt, -1) {
		if m[1] == "Bar: not applicable" { // the bare label named in prose, not a line to write
			continue
		}
		bars = append(bars, fill.ReplaceAllString(m[1], "something concrete"))
	}
	if len(bars) < 3 {
		t.Fatalf("want the example bar and both exemptions quoted in the prompt, got %q", bars)
	}
	for _, bar := range bars {
		body := []byte("# t\n\n## Verification\n\n" + bar + "\n")
		got, err := doccheck.SpecBarIn(body, doclang.EN)
		if err != nil || !strings.HasPrefix(got, "Bar:") {
			t.Errorf("%q is not read as an English bar: %q, %v", bar, got, err)
		}
	}
	if !strings.Contains(prompt, "Question for the judge: <") {
		t.Fatal("prompt does not show the judge-question line")
	}
	jq := "Question for the judge: Which proposal would you ship?"
	body := []byte("# t\n\n## Verification\n\nBar: Kiro spec mode — kiro.dev/docs/specs\n" + jq + "\n")
	if q, err := doccheck.SpecJudgeQuestion(body); err != nil || q == "" {
		t.Errorf("judge question not recognized: %q, %v", q, err)
	}
	marker := doclang.AmbiguityMarker(doclang.EN)
	if !strings.Contains(prompt, marker+" specific question]") {
		t.Errorf("prompt does not teach %q specific question]", marker)
	}
	if open := clarify.Open("x " + marker + " Which currency?] y"); len(open) != 1 {
		t.Errorf("clarification marker not read: %v", open)
	}
}

// TestBuildChatSystemPrompt_PortugueseNoChannelUnchanged (F-0433 crit. 3):
// the no-channel pt-BR prompt, byte for byte.
func TestBuildChatSystemPrompt_PortugueseNoChannelUnchanged(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, false)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/chat_prompt_pt_nochannel.golden"
	if os.Getenv("CHAT_GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(golden, []byte(prompt), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != string(want) {
		t.Error("the pt-BR no-channel chat prompt differs from the golden")
	}
}

// TestBuildChatSystemPrompt_DraftCheck (F-0438 crit. 2): only the claude-cli
// agent, which can run the command, is told to check its draft first.
func TestBuildChatSystemPrompt_DraftCheck(t *testing.T) {
	repoDir, projDir := chatFixture(t)
	for mode, want := range map[string]bool{"claude-cli": true, "byo-key": false, "": false} {
		prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{LLM: project.LLMConfig{Mode: mode}}, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(prompt, "cerne spec check"); got != want {
			t.Errorf("mode %q: prompt mentions spec check = %v, want %v", mode, got, want)
		}
		// The agent has no write tool: the body goes on stdin, never a temp file.
		if want && (!strings.Contains(prompt, "<<'SPEC'") || strings.Contains(prompt, "arquivo temporário")) {
			t.Errorf("claude-cli prompt must teach the stdin heredoc and no temp file")
		}
	}
}

// TestBuildChatSystemPrompt_QuestionContract (F-0437 crit. 1 and 4): the
// prompt teaches the cerne-pergunta block (format, 2-4 options, one
// recommended, exactly one per turn — B-0047) and the rule that answered questions
// become "Decisões do design" lines with their origin. The English prompt
// carries the English forms and no Portuguese ones.
func TestBuildChatSystemPrompt_QuestionContract(t *testing.T) {
	for _, l := range doclang.Langs() {
		prompt := chatPromptIn(t, l)
		d := func(t doclang.Term) string { return doclang.Display(t, l) }
		design := doclang.Section(doclang.Spec, "Design decisions", l)
		for _, want := range []string{
			"\n````" + chatQuestionFence + "\n",
			"# <question>",
			"- <option> — <consequence in one line>",
			"(" + d(doclang.Recommended) + ")",
			"2 to 4 options",
			"Emit exactly one block per turn", // B-0047: one question at a time
			"the next question waits for the answer to this one",
			d(doclang.OriginAccepted),
			d(doclang.OriginOther),
			d(doclang.OriginFree),
			// The rule must name the decisions section itself, not just any
			// section of the template (the template list already carries it).
			"line of the \"" + design + "\" section",
			"## " + design + "\n",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s prompt lacks %q", l, want)
			}
		}
		other := doclang.PT
		if l == doclang.PT {
			other = doclang.EN
		}
		for _, term := range []doclang.Term{doclang.Recommended, doclang.OriginAccepted, doclang.OriginOther, doclang.OriginFree} {
			w := doclang.Display(term, other)
			if term == doclang.Recommended {
				w = "(" + w + ")" // the bare word is also prose and a section name
			}
			if strings.Contains(prompt, w) {
				t.Errorf("%s prompt carries the %s form %q", l, other, w)
			}
		}
		if bad := doclang.Section(doclang.Spec, "Design decisions", other); bad != design && strings.Contains(prompt, bad) {
			t.Errorf("%s prompt carries the %s section %q", l, other, bad)
		}
	}
}

// TestBuildChatSystemPromptLiveQueryCodex (F-0439 criterion 3): the local codex
// chat block tells the agent to consult the project files and cerne before
// answering, names the tools it has, and asks to cite what it read. It names no
// shell command the turn cannot run.
func TestBuildChatSystemPromptLiveQueryCodex(t *testing.T) {
	for _, want := range []string{"find_files", "read_file", "search", "show", "brief", "status", "Cite"} {
		if !strings.Contains(ChatCodexLiveQuery(doclang.PT), want) {
			t.Errorf("codex live-query block lacks %q:\n%s", want, ChatCodexLiveQuery(doclang.PT))
		}
	}
	// The exploration rules and the fixed three-section answer (Gauntlet rounds 1 to 5).
	for _, want := range []string{"search_files", "list the tree", "**", "stage", "backdrop",
		"Never ask the user for a path or a keyword before", "relative to the project root", "never absolute paths", "temporary working directory",
		"Read every file you point to", "never present a file as the definition", "exactly as read_file or search_files numbered them",
		"cite the file without a line rather than guess", "in this language", "exactly three sections, in this order, every time",
		`"Onde está", "Para ajustar", "Fontes consultadas"`,
		"decisive file first", "that line's text quoted", "one bullet per file", "do NOT define", "mark the decisive one",
		"no entry that was not read", "no generic source without an id"} {
		if !strings.Contains(ChatCodexLiveQuery(doclang.PT), want) {
			t.Errorf("codex live-query block lacks %q:\n%s", want, ChatCodexLiveQuery(doclang.PT))
		}
	}
	t.Logf("block size: %d bytes", len(ChatCodexLiveQuery(doclang.PT)))
	if strings.Contains(ChatCodexLiveQuery(doclang.PT), "cerne search") || strings.Contains(ChatCodexLiveQuery(doclang.PT), "Read, Grep") {
		t.Errorf("block names a claude tool or shell command:\n%s", ChatCodexLiveQuery(doclang.PT))
	}
}

// TestBuildChatSystemPrompt_InterviewProtocol (F-0443 crit. 1-6): the
// conduction block interviews over the spec's decision tree, in frontier
// rounds, and sits between the conduction header and the Gauntlet block.
func TestBuildChatSystemPrompt_InterviewProtocol(t *testing.T) {
	for _, l := range doclang.Langs() {
		prompt := chatPromptIn(t, l)
		assumed := strings.TrimSuffix(doclang.Display(doclang.Assumed, l), ":")
		for _, want := range []string{
			ChatInterviewMarker,
			"Map the decisions the spec needs", // (b) the decision map
			"(a) who is affected or receives, and who is left out",
			"(b) the trigger",
			"missing data, a finished or cancelled item, repetition or recurrence, data that changes later",
			"(d) how the person controls or turns it off",
			"(e) what is OUT of scope, asked of the human, never assumed",
			"(f) the bar",
			"is NEVER an assumption, even if it costs more rounds",
			"Examples that are questions",
			"Examples that may be \"" + assumed + ":\"",
			"\"0 open\" only holds when",
			"only the frontier",                                     // (c) frontier only
			"depends on the answer to another still open",           // (c) no dependent question in a round
			"changes an acceptance criterion, the scope or the bar", // (d) relevance filter
			doclang.Display(doclang.Assumed, l),                     // (d) low-impact list
			"Ask ONE question per turn",                             // (e) B-0047: one at a time
			"only after the human answered the previous one",        // (e) B-0047: next waits for the answer
			"aim for at most 5 questions in the whole interview",    // (e) B-0047: budget
			"only a scenario whose decisions really change",         // (e) B-0047: more only when needed
			"through the " + chatQuestionFence + " block",           // (e) cites the F-0437 contract
			"worded so that \"yes\" accepts the recommendation",     // (e) recommendation phrased for "yes"
			doclang.Display(doclang.TreeProgress, l),                // (f) progress line
			"do NOT emit " + chatProposalFence,                      // (g) no proposal while open
			doclang.Display(doclang.ProposeNow, l),                  // (g) human exit
			"as the ambiguity marker",                               // (g) open ones become markers
			"confirmation in one line",                              // (g) confirm before emitting
			"Only propose when problem, goal and testable acceptance criteria are settled and no decision is open",
			"the turn ENDS on the summary and the one-line confirmation question, without " + chatProposalFence,
			"the block only comes in the next turn, after the human confirms or corrects",
			"emit at once, with the open ones as the ambiguity marker",
			"No \"" + assumed + ":\" item contradicts or replaces what the human answered",
			"every human answer appears in the spec draft",
			"an answer with no place in the spec becomes a question, not a silent discard",
			"(" + doclang.AmbiguityMarker(l) + " question])",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s: prompt lacks interview rule %q", l, want)
			}
		}
		conduction := strings.Index(prompt, "Conduct the elaboration")
		interview := strings.Index(prompt, ChatInterviewMarker)
		gauntlet := strings.Index(prompt, ChatGauntletMarker)
		if !(conduction < interview && interview < gauntlet) {
			t.Errorf("%s: interview block out of order: conduction=%d interview=%d gauntlet=%d", l, conduction, interview, gauntlet)
		}
	}
}

// TestBuildChatSystemPrompt_NoFixedQuestionTarget (F-0443 crit. 7): neither
// language carries the fixed question target or the "never interrogation" wording.
func TestBuildChatSystemPrompt_NoFixedQuestionTarget(t *testing.T) {
	for _, l := range doclang.Langs() {
		prompt := chatPromptIn(t, l)
		for _, bad := range []string{"~3", "interrogatório", "interrogation"} {
			if strings.Contains(prompt, bad) {
				t.Errorf("%s prompt still carries %q", l, bad)
			}
		}
	}
}

// TestBuildChatSystemPrompt_InterviewFormsFollowLanguage (F-0443 crit. 7,
// F-0447 crit. 2): the interview protocol carries the forms of the project's
// language and none of the other's.
func TestBuildChatSystemPrompt_InterviewFormsFollowLanguage(t *testing.T) {
	for _, l := range doclang.Langs() {
		other := doclang.PT
		if l == doclang.PT {
			other = doclang.EN
		}
		prompt := chatPromptIn(t, l)
		for _, term := range []doclang.Term{doclang.Assumed, doclang.TreeProgress, doclang.ProposeNow} {
			if w := doclang.Display(term, l); !strings.Contains(prompt, w) {
				t.Errorf("%s: prompt lacks %q", l, w)
			}
			if w := doclang.Display(term, other); strings.Contains(prompt, w) {
				t.Errorf("%s: prompt carries the %s form %q", l, other, w)
			}
		}
	}
}

// TestGauntletContractFormsRoundTrip (F-0447 crit. 2): the example bar lines the
// contract teaches, in either language, are read by the real bar reader as
// what they claim to be — and carry no form of the other language.
func TestGauntletContractFormsRoundTrip(t *testing.T) {
	fill := regexp.MustCompile(`<[^>]*>`)
	for _, l := range doclang.Langs() {
		contract := GauntletContract(l)
		label := doclang.Display(doclang.BarLabel, l)
		var bars []string
		for _, m := range regexp.MustCompile(`"(`+label+`: [^"\n]*)"`).FindAllStringSubmatch(contract, -1) {
			if m[1] == label+": "+doclang.Display(doclang.NotApplicable, l) { // the bare label named in prose
				continue
			}
			bars = append(bars, fill.ReplaceAllString(m[1], "something concrete"))
		}
		if len(bars) < 3 {
			t.Fatalf("%s: want the example bar and both exemptions quoted, got %q", l, bars)
		}
		verification := doclang.Section(doclang.Spec, "Verification", l)
		for _, bar := range bars {
			body := []byte("# t\n\n## " + verification + "\n\n" + bar + "\n")
			if got, err := doccheck.SpecBarIn(body, l); err != nil || !strings.HasPrefix(got, label+":") {
				t.Errorf("%s: %q is not read as a bar: %q, %v", l, bar, got, err)
			}
		}
		other := doclang.PT
		if l == doclang.PT {
			other = doclang.EN
		}
		for _, term := range []doclang.Term{doclang.BarLabel, doclang.MethodLabel, doclang.ExitLabel, doclang.NotApplicable, doclang.Waived} {
			if w := doclang.Display(term, other); strings.Contains(contract, w+" ") || strings.Contains(contract, w+":") || strings.Contains(contract, " "+w) {
				t.Errorf("%s: the contract carries the %s form %q", l, other, w)
			}
		}
	}
}

// TestBuildChatSystemPrompt_EnglishGolden (F-0447 crit. 1, 2): the English
// project's chat prompt, with and without an execution channel, byte for byte.
// Regenerate only on a deliberate prompt change: CHAT_GOLDEN_UPDATE=1.
func TestBuildChatSystemPrompt_EnglishGolden(t *testing.T) {
	for name, channel := range map[string]bool{"chat_prompt_en.golden": true, "chat_prompt_en_nochannel.golden": false} {
		repoDir, projDir := chatFixture(t)
		p, err := store.LoadProject(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		p.DocsLanguage = "en"
		if err := store.SaveProject(repoDir, p); err != nil {
			t.Fatal(err)
		}
		prompt, err := BuildChatSystemPrompt(repoDir, projDir, project.Config{}, channel)
		if err != nil {
			t.Fatal(err)
		}
		golden := filepath.Join("testdata", name)
		if os.Getenv("CHAT_GOLDEN_UPDATE") == "1" {
			if err := os.WriteFile(golden, []byte(prompt), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if prompt != string(want) {
			t.Errorf("the English chat prompt (channel %v) differs from %s", channel, golden)
		}
	}
}

// F-0520 crit. 5: each attached image is named by its path right under the
// message it belongs to; a transcript with no image keeps today's prompt.
func TestBuildChatPromptNamesAttachedImages(t *testing.T) {
	plain := []ChatMessage{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	if got, want := BuildChatPrompt("SYS", plain), "SYS\n\n--- Conversation so far ---\nUser: hi\nAgent: hello\nAgent:"; got != want {
		t.Fatalf("prompt without images changed:\n%q\nwant\n%q", got, want)
	}

	withImg := []ChatMessage{
		{Role: "user", Content: "see this", Images: []string{"/c/a.png", "/c/b.jpg"}},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "and now?"},
	}
	got := BuildChatPrompt("SYS", withImg)
	if !strings.Contains(got, "User: see this\n[attached image: /c/a.png]\n[attached image: /c/b.jpg]\nAgent: ok\nUser: and now?\n") {
		t.Fatalf("images are not listed under their message:\n%s", got)
	}
	if !strings.Contains(got, chatImagesNote) || strings.Index(got, chatImagesNote) > strings.Index(got, "--- Conversation so far ---") {
		t.Fatalf("the images note must come before the transcript:\n%s", got)
	}
}

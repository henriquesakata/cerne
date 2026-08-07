package app

import (
	"fmt"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// DefaultAgentPersona is the neutral persona the Backlog chat (F-0210) uses
// when the project sets no [agent] block in .cernerc.
const DefaultAgentPersona = "Você é um analista de requisitos experiente, ajudando a transformar um pedido em uma feature bem especificada."

// chatProposalFence is the fenced-block info string the system prompt tells
// the model to use when it is ready to propose a candidate — the UI (and any
// future Go-side consumer) detects it the same way SanitizeMermaid/
// extractFencedBlock detect a ```mermaid block in diagram.go.
const chatProposalFence = "cerne-proposta"

// ChatMessage is one turn of the Backlog chat transcript (F-0210). The
// server is stateless: the whole transcript is resent on every call and
// flattened into a single prompt via BuildChatPrompt (Provider.Complete takes
// one prompt, not a chat API).
type ChatMessage struct {
	Role    string // "user" | "assistant"
	Content string
}

// BuildChatSystemPrompt assembles the Backlog chat's system prompt (F-0210):
// the project's domain persona (cfg.Agent, falling back to
// DefaultAgentPersona) plus enough project context — the live feature roster
// and the capability tree — for the agent to ground a proposal in what
// already exists, and instructions for how to format that proposal so the UI
// can offer it for confirmation.
func BuildChatSystemPrompt(repoDir, projDir string, cfg project.Config) (string, error) {
	persona, err := cfg.Agent.Resolve(projDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(persona) == "" {
		persona = DefaultAgentPersona
	}

	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return "", err
	}
	capabilities, err := store.LoadAllCapabilities(repoDir)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(persona)
	b.WriteString("\n\n")
	b.WriteString("Você conversa com um usuário do projeto no Backlog do cerne, a ferramenta de documentação viva deste produto. ")
	b.WriteString("Seu papel é entender o que a pessoa precisa e ajudar a transformar isso numa feature bem especificada, usando seu conhecimento do domínio do produto.\n\n")

	b.WriteString("Roster do projeto (features vivas/entregues; id, estado, título):\n")
	for _, f := range features {
		if f.State == domain.StateRejected || f.State == domain.StateSuperseded {
			continue
		}
		fmt.Fprintf(&b, "- %s [%s] %s\n", f.ID, f.State, f.Title)
	}
	b.WriteString("\n")

	b.WriteString("Árvore de capacidades do produto (id, pai quando houver, título):\n")
	if len(capabilities) == 0 {
		b.WriteString("(nenhuma capacidade registrada ainda)\n")
	}
	for _, c := range capabilities {
		if c.Parent == "" {
			fmt.Fprintf(&b, "- %s %s\n", c.ID, c.Title)
		} else {
			fmt.Fprintf(&b, "- %s (sob %s) %s\n", c.ID, c.Parent, c.Title)
		}
	}
	b.WriteString("\n")

	b.WriteString("Conduza a conversa fazendo as perguntas necessárias para entender o problema real, não apenas a solução pedida. ")
	b.WriteString("Quando tiver contexto suficiente — problema, objetivo, critérios de aceitação testáveis — e SOMENTE então, proponha a candidata.\n\n")
	b.WriteString("Para propor, termine sua resposta com um bloco cercado assim:\n\n")
	fmt.Fprintf(&b, "```%s\n", chatProposalFence)
	b.WriteString("# <título curto da feature>\n")
	b.WriteString("facet: <uma opção do conjunto fechado: " + strings.Join(domain.ValidFacets, ", ") + ">\n")
	b.WriteString("components: <lista curta separada por vírgula>\n")
	b.WriteString("capability: <um id C-NNNN da árvore acima, ou um título novo para propor um nó quando nenhuma servir>\n\n")
	b.WriteString("Escreva a spec COMPLETA em Markdown, com exatamente estas seções nesta ordem:\n")
	for _, s := range spectmpl.Sections {
		b.WriteString("## " + s + "\n")
		if s == "Verificação" {
			// The chat always fills this section, unlike a plain spec draft
			// (F-0207 keeps it out of spectmpl.Sections so stub-detection
			// stays lenient) — the chat proposal has no later editing pass.
			b.WriteString("## Execução recomendada (agente)\n")
		}
	}
	b.WriteString("```\n\n")
	b.WriteString("Preencha de verdade a seção \"Execução recomendada (agente)\" (capacidade de modelo sugerida + tarefas a manter juntas/separáveis) — não deixe placeholder. ")
	b.WriteString("Critérios de aceitação numerados e testáveis; para comportamento, use EARS: \"QUANDO <condição> O SISTEMA DEVE <comportamento>\". ")
	b.WriteString("Marque qualquer incerteza com \"" + spectmpl.AmbiguityMarker + " pergunta específica]\" em vez de supor. ")
	b.WriteString("Fora desse bloco, converse normalmente — só o produza quando estiver pronto para o usuário confirmar o registro.\n")
	return b.String(), nil
}

// BuildChatPrompt flattens the system prompt and the transcript so far into
// the single prompt Provider.Complete expects, closing with a cue for the
// model to continue as the agent.
func BuildChatPrompt(system string, messages []ChatMessage) string {
	var b strings.Builder
	b.WriteString(system)
	b.WriteString("\n\n--- Conversa até agora ---\n")
	for _, m := range messages {
		label := "Usuário"
		if m.Role == "assistant" {
			label = "Agente"
		}
		fmt.Fprintf(&b, "%s: %s\n", label, m.Content)
	}
	b.WriteString("Agente:")
	return b.String()
}

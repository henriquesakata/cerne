package app

import (
	"fmt"
	"slices"
	"strings"

	"gitlab.com/rockt/cerne/internal/ask"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// retrievalTopN is how many retrieved sources the turn context carries: enough
// to cover the typical question without drowning the prompt (F-0253).
const retrievalTopN = 6

// conventionComponent marks the notes that state how this project authors and
// works — always injected, search or no search (F-0253 criterion 2).
const conventionComponent = "convention"

// BuildTurnContext assembles the project context injected into the Backlog
// chat prompt for ONE turn (F-0253): the FTS hits relevant to the user's
// message (the same retrieval `cerne ask` runs, state-tagged so the agent
// never reads old prose as current) plus the always-on baseline — active
// decisions and convention notes. It is server-side and provider-agnostic:
// byo-key/http get the same context claude-cli's tool-use would fetch.
//
// budget caps the retrieved prose in characters, split evenly across sources;
// a non-positive budget disables the injection entirely. Retrieval failure
// (missing/corrupt index) yields no snippets rather than a failed turn —
// priming is an enhancement, the chat still answers without it. Empty
// everything returns "" so no empty section reaches the prompt.
func BuildTurnContext(repoDir, dbPath, userMsg string, budget int) (string, error) {
	if budget <= 0 {
		return "", nil
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return "", err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return "", err
	}
	snips, _ := ask.Retrieve(repoDir, dbPath, userMsg, false, retrievalTopN)
	snips = ask.Trim(snips, retrievalTopN, perSourceBudget(budget, len(snips)))

	var body strings.Builder
	if lines := activeDecisionLines(decisions); len(lines) > 0 {
		body.WriteString("Decisões ativas do projeto:\n")
		body.WriteString(strings.Join(lines, "\n") + "\n\n")
	}
	if lines := conventionNoteLines(notes); len(lines) > 0 {
		body.WriteString("Convenções registradas (notas; consulte com cerne note show <id> quando precisar do texto):\n")
		body.WriteString(strings.Join(lines, "\n") + "\n\n")
	}
	if len(snips) > 0 {
		body.WriteString("Trechos recuperados do backend, relevantes à mensagem do usuário:\n")
		for _, s := range snips {
			fmt.Fprintf(&body, "- %s [%s]: %s\n", s.Source, s.State, strings.TrimSpace(s.Text))
		}
		body.WriteString("\n")
	}
	if body.Len() == 0 {
		return "", nil
	}
	return "Contexto recuperado do projeto (injetado automaticamente pelo cerne; o estado ao lado de cada id é a verdade atual):\n\n" + body.String(), nil
}

// perSourceBudget splits the character budget evenly across the sources, so a
// long wiki page cannot eat the whole allowance before the entity hits.
func perSourceBudget(budget, sources int) int {
	if budget <= 0 || sources <= 0 {
		return 0
	}
	if per := budget / sources; per > 0 {
		return per
	}
	return 1
}

func activeDecisionLines(decisions []store.Decision) []string {
	var lines []string
	for _, d := range decisions {
		if d.State != domain.DecisionActive {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s %s", d.ID, d.Title))
	}
	return lines
}

func conventionNoteLines(notes []store.Note) []string {
	var lines []string
	for _, n := range notes {
		if n.State != domain.NoteActive || !slices.Contains(n.Components, conventionComponent) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s %s", n.ID, n.Title))
	}
	return lines
}

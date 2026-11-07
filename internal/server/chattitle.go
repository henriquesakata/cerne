package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/project"
)

// chatTitleTimeout bounds the one-shot title call (F-0310). A var, not a
// const, so tests can shrink it; the claude-cli one-shot measured ~8 s (F-0287).
var chatTitleTimeout = 60 * time.Second

// titleInputRunes caps each message fed to the title prompt.
// ponytail: fixed per-message cap; tie it to max_tokens if long first turns get poor titles.
const titleInputRunes = 2000

// chatTitleResponse: Generated=false with no NeedsProvider is "the model gave
// nothing usable" — the client keeps its truncated title either way.
type chatTitleResponse struct {
	Generated     bool   `json:"generated"`
	NeedsProvider bool   `json:"needs_provider,omitempty"`
	Title         string `json:"title,omitempty"`
}

// handleChatTitle (F-0310) summarizes a conversation's first exchange into a
// short title through the project's one-shot provider — the roast path, not the
// tool-using chat agent. Delegation degrades silently (criterion 12).
func handleChatTitle(w http.ResponseWriter, r *http.Request, projDir string) {
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req struct {
		Messages []chatMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("messages must not be empty"))
		return
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil && !errors.Is(err, project.ErrNoProject) {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeJSON(w, http.StatusOK, chatTitleResponse{NeedsProvider: true})
		return
	}
	prov, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), chatTitleTimeout)
	defer cancel()
	reply, err := prov.Complete(ctx, chatTitlePrompt(req.Messages))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorBody("chat title: "+err.Error()))
		return
	}
	title := cleanTitle(reply)
	writeJSON(w, http.StatusOK, chatTitleResponse{Generated: title != "", Title: title})
}

// chatTitlePrompt is pt-BR on purpose: an authoring prompt sets the language of
// the generated title. Only the first exchange is read (the client sends just
// that); each message is capped at titleInputRunes.
func chatTitlePrompt(msgs []chatMessage) string {
	var b strings.Builder
	b.WriteString("Dê um título curto, de no máximo 40 caracteres, para a tarefa desta conversa, " +
		"como numa lista de conversas. Responda só com o título, em português, sem aspas, " +
		"sem ponto final e sem explicação.\n\n")
	for _, m := range msgs[:min(2, len(msgs))] {
		who := "USUÁRIO"
		if m.Role == "assistant" {
			who = "AGENTE"
		}
		content := []rune(m.Content)
		fmt.Fprintf(&b, "%s: %s\n\n", who, string(content[:min(titleInputRunes, len(content))]))
	}
	return b.String()
}

var (
	titleHeadingRe = regexp.MustCompile(`^#{1,6}\s+`)             // "# X", never "#123"
	titleListRe    = regexp.MustCompile(`^(?:[-*+•]|\d+[.)])\s+`) // "- X", "* X", "1. X"
	titleLabelRe   = regexp.MustCompile(`(?i)^(?:t[íi]tulo|title)\s*:\s*`)
	titleMarkdown  = strings.NewReplacer("*", "", "`", "")
)

// titleQuotes are trimmed around the title (and the "_" of _emphasis_).
const titleQuotes = "\"'“”‘’«»_ "

// cleanTitle normalizes a model reply to one title line (F-0310 C5): the
// first line that is neither empty nor a preamble ending in ":" ("Claro! Aqui
// está o título:"), without heading marks, list markers, bold/code markdown,
// a "Título:"/"Title:" label, surrounding quotes or a final period. The
// 40-char cap is the client's (truncateTitle), not repeated here.
func cleanTitle(reply string) string {
	for _, line := range strings.Split(reply, "\n") {
		line = titleListRe.ReplaceAllString(titleHeadingRe.ReplaceAllString(strings.TrimSpace(line), ""), "")
		line = strings.Trim(titleMarkdown.Replace(line), titleQuotes)
		line = strings.Trim(titleLabelRe.ReplaceAllString(line, ""), titleQuotes)
		line = strings.TrimSpace(strings.TrimRight(line, "."))
		if line != "" && !strings.HasSuffix(line, ":") {
			return line
		}
	}
	return ""
}

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// capabilityIDPattern distinguishes an existing C- id from a free-text title
// proposing a new capability node in chatRegisterRequest.Capability.
var capabilityIDPattern = regexp.MustCompile(`^C-\d+$`)

// chatMessage is one turn of the Backlog chat transcript (F-0210), as sent by
// the browser — the server is stateless, so the whole transcript travels on
// every call.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the POST /api/chat body. Model/Effort (F-0218) are optional
// per-call overrides from the chat's model/effort selector: Model replaces
// cfg.LLM.Model for the byo-key provider; Effort travels here for forward
// compatibility but no provider applies it yet (silently ignored).
type chatRequest struct {
	Messages []chatMessage `json:"messages"`
	Model    string        `json:"model,omitempty"`
	Effort   string        `json:"effort,omitempty"`
}

// chatResponse mirrors askResponse/generateDiagramResponse: Answered is false
// in delegation mode (no provider to answer for the human in the browser) —
// the endpoint never fabricates a reply.
type chatResponse struct {
	Answered      bool   `json:"answered"`
	NeedsProvider bool   `json:"needs_provider,omitempty"`
	Reply         string `json:"reply,omitempty"`
}

// handleChat answers one turn of the Backlog domain-agent chat (F-0210): the
// system prompt (persona + project context) is assembled fresh from the
// current .cernerc/backend on every call, then flattened with the transcript
// into a single Provider.Complete call — nothing is written to the backend
// here (criterion 6); only handleChatRegister does.
func handleChat(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("messages must not be empty"))
		return
	}

	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeJSON(w, http.StatusOK, chatResponse{Answered: false, NeedsProvider: true})
		return
	}
	if req.Model != "" {
		cfg.LLM.Model = req.Model
	}

	sysPrompt, err := app.BuildChatSystemPrompt(repoDir, projDir, cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	msgs := make([]app.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = app.ChatMessage{Role: m.Role, Content: m.Content}
	}

	prov, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	reply, err := prov.Complete(r.Context(), app.BuildChatPrompt(sysPrompt, msgs))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, chatResponse{Answered: true, Reply: reply})
}

// chatRegisterRequest is the POST /api/chat/register body: the proposal the
// user confirmed in the chat, already parsed into title + spec body by the UI.
type chatRegisterRequest struct {
	Title      string   `json:"title"`
	SpecBody   string   `json:"spec_body"`
	Facets     []string `json:"facets"`
	Components []string `json:"components"`
	// Capability (F-0223) is either an existing C-NNNN id (validated before
	// the candidate is written — a bad id must not spend the write) or, when
	// it doesn't match that shape, a title proposing a new node — created
	// and linked after the candidate exists.
	Capability string `json:"capability"`
}

// handleChatRegister registers the confirmed proposal as a candidate — the
// same core as POST /api/features --file (app.CreateFeatureWithSpec), so the
// same duplicate-registration guard (F-0147) and module/facet validation
// apply. This is the only handler in the chat surface that touches the
// backend, and only after the user's explicit confirmation click.
func handleChatRegister(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	var req chatRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if strings.TrimSpace(req.SpecBody) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("spec_body must not be empty"))
		return
	}
	capability := strings.TrimSpace(req.Capability)
	isNewCapability := capability != "" && !capabilityIDPattern.MatchString(capability)
	if capability != "" && !isNewCapability {
		if _, err := store.LoadCapability(repoDir, capability); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, errorBody(fmt.Sprintf("unknown capability: %s", capability)))
			return
		}
	}
	f, err := app.CreateFeatureWithSpec(repoDir, projDir, req.Title, "", "", req.Facets, req.Components, req.SpecBody, false, "", "")
	if errors.Is(err, app.ErrSimilarExists) {
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
		return
	}
	if capability != "" {
		capID := capability
		if isNewCapability {
			c, err := app.CreateCapability(repoDir, projDir, capability, "")
			if err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
				return
			}
			capID = c.ID
		}
		f, err = app.LinkFeatureCapabilities(repoDir, projDir, f.ID, []string{capID}, false)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
			return
		}
	}
	writeJSON(w, http.StatusCreated, createFeatureResponse{Feature: f})
}

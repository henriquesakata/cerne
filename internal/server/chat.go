package server

import (
	"context"
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

// newAgentProvider is a test seam over llm.NewAgentProvider: tests swap it to
// inject a provider without the Streamer capability (the F-0240 fallback path,
// unreachable otherwise without shelling out to a claude binary).
var newAgentProvider = llm.NewAgentProvider

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
func handleChat(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
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
		writeChatLine(w, http.StatusOK, chatResponse{Answered: false, NeedsProvider: true})
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
	// F-0253: prime the turn server-side with the backend context relevant to
	// the last user message — every provider gets it, tool-use or not. A
	// broken index must not cost the turn, so retrieval failure only means no
	// injected block (the error is the index's problem, surfaced by
	// search/ask).
	if dbPath, err := ensureIndex(repoDir, projectID); err == nil {
		turnCtx, err := app.BuildTurnContext(repoDir, dbPath, lastUserMessage(msgs), cfg.Agent.RetrievalBudgetOrDefault())
		if err == nil && turnCtx != "" {
			sysPrompt += "\n" + turnCtx
		}
	}

	// F-0224: the chat provider runs claude-cli as a read-only agent in the
	// project dir; the budget covers chained cerne queries, not just one shot.
	prov, err := newAgentProvider(cfg.LLM, projDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cfg.Agent.ChatTimeoutOrDefault())
	defer cancel()
	prompt := app.BuildChatPrompt(sysPrompt, msgs)

	// F-0240: a provider with the Streamer capability transmits the reply as
	// NDJSON deltas; anyone else keeps the single-shot path below — whose body
	// is byte-for-byte the streaming path's terminal event (F-0244: a single
	// compact JSON line, same as streamChatReply's terminal Encode), so the
	// client never needs to know which path ran.
	if streamer, ok := prov.(llm.Streamer); ok {
		if flusher, ok := w.(http.Flusher); ok {
			streamChatReply(ctx, w, flusher, streamer, prompt)
			return
		}
	}
	reply, err := prov.Complete(ctx, prompt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeChatLine(w, http.StatusOK, chatResponse{Answered: true, Reply: reply})
}

// lastUserMessage is the retrieval query for the turn (F-0253): the message
// just sent, not the whole transcript — earlier turns dilute the FTS signal.
func lastUserMessage(msgs []app.ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "assistant" {
			return msgs[i].Content
		}
	}
	return ""
}

// writeChatLine writes v as a single compact JSON line ("{...}\n") — the
// NDJSON terminal-event shape the client's streamChat parser requires
// (ui/src/api.ts) — for the non-streaming success paths of handleChat.
// writeJSON's indented multi-line output broke JSON.parse on the first line
// alone (F-0244); this is the fix, factored out so both success paths (and
// the contract test) share one place.
func writeChatLine(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// streamChatReply writes the chat reply as line-delimited JSON: one
// {"delta":...} per chunk — each explicitly flushed, or net/http's buffering
// would deliver everything at once and the streaming would be staged all over
// again — then a terminal line: the plain chatResponse on success, or
// {"error":...} when the stream broke after output already went out (the
// partial the client holds stays valid; F-0240 criterion 8). An error before
// any delta still becomes the plain 500 JSON of the single-shot path.
func streamChatReply(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, streamer llm.Streamer, prompt string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	wrote := false
	reply, err := streamer.Stream(ctx, prompt, func(delta string) error {
		wrote = true
		if err := enc.Encode(map[string]string{"delta": delta}); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		if !wrote {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		_ = enc.Encode(errorBody(err.Error()))
		flusher.Flush()
		return
	}
	_ = enc.Encode(chatResponse{Answered: true, Reply: reply})
	flusher.Flush()
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

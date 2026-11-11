package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// capabilityIDPattern distinguishes an existing C- id from a free-text title
// proposing a new capability node in chatRegisterRequest.Capability.
var capabilityIDPattern = regexp.MustCompile(`^C-\d+$`)

// newAgentProvider is a test seam over llm.NewAgentProvider: tests swap it to
// inject a provider without the Streamer capability (the F-0240 fallback path,
// unreachable otherwise without shelling out to a claude binary).
var newAgentProvider = llm.NewAgentProvider

// CommandTree is the cerne command tree the chat's tool channel derives its
// verb catalog from (F-0323). The server cannot import internal/cli (cli
// imports server), so cli injects it at init; nil means no tool channel — the
// byo-key chat then degrades honestly instead of promising execution.
var CommandTree func() *cobra.Command

// execChannel reports whether the active mode can execute cerne commands
// (F-0323 crit. 7): claude-cli runs them through its own Bash tool; any other
// mode needs a function-calling provider, not declared without tools in [llm]
// (C7), and the injected command tree.
func execChannel(cfg project.LLMConfig, prov llm.Provider) bool {
	if llm.ResolveMode(cfg.Mode) == llm.ModeClaudeCLI {
		return true
	}
	_, ok := prov.(llm.ToolCaller)
	return ok && !cfg.NoFunctionCalling && CommandTree != nil
}

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
	// FeatureID (F-0256) turns the turn into a refine of that candidate: its
	// current spec joins the system prompt. Read from the backend on every
	// call, so a resumed conversation never answers from a stale snapshot.
	FeatureID string `json:"feature_id,omitempty"`
	// Confirm (F-0260) is the user's explicit go-ahead for the commands the
	// agent echoed in the previous turn: only then does the provider get the
	// write allowlist. It is per request on purpose — never remembered from an
	// earlier turn, never inferred from the message text — so the zero value
	// keeps the chat read-only (criterion 3: a proposal is not a permission).
	Confirm bool `json:"confirm,omitempty"`
	// ConversationID and ConfirmTurn (F-0323) bind Confirm to one card: the
	// browser's conversation id (provenance of every write the turn makes) and
	// the transcript index of the agent message that carried the card. A
	// confirmation is valid only for the turn right after that card.
	ConversationID string `json:"conversation_id,omitempty"`
	ConfirmTurn    int    `json:"confirm_turn,omitempty"`
}

var conversationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// usedConfirmations holds the (backend, conversation, card turn) confirmations
// already spent in this process (F-0323 C9).
// ponytail: in memory and unbounded (a few bytes per confirmed turn), reset on restart; persist if replays across restarts matter.
var usedConfirmations sync.Map

// refuseConfirmation is the 422 of a confirmation that does not hold, with the
// stable code of the tool contract (F-0323 C8) next to the explanation.
func refuseConfirmation(w http.ResponseWriter, msg string) {
	code := verbs.Fail(verbs.ErrConfirmationRequired).Error.Code
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": msg, "code": code})
}

// confirmedTurn validates the request's confirmation against its own
// transcript (F-0323 crit. 5) and returns the card's turn index. It holds only
// when the user confirms the card the agent JUST showed: confirm set, a valid
// conversation id, confirm_turn == len(messages)-2 naming an assistant message
// with a cerne-comando fence, and the user's message last. A later turn has no
// card at len-2, so a grant never outlives its turn.
func confirmedTurn(req chatRequest) (int, error) {
	n, t := len(req.Messages), req.ConfirmTurn
	switch {
	case !req.Confirm:
		return 0, errors.New("confirm is not set")
	case !conversationIDPattern.MatchString(req.ConversationID):
		return 0, fmt.Errorf("conversation_id must match %s", conversationIDPattern)
	case t != n-2 || t < 0:
		return 0, fmt.Errorf("confirm_turn %d is not the card turn right before the last message (%d)", t, n-2)
	case req.Messages[t].Role != "assistant" || !strings.Contains(req.Messages[t].Content, "```"+app.ChatCommandFence):
		return 0, fmt.Errorf("messages[%d] is not an agent message carrying a %s card", t, app.ChatCommandFence)
	case req.Messages[n-1].Role != "user":
		return 0, errors.New("the last message must be the user's")
	}
	return t, nil
}

// chatResponse mirrors askResponse/generateDiagramResponse: Answered is false
// in delegation mode (no provider to answer for the human in the browser) —
// the endpoint never fabricates a reply.
type chatResponse struct {
	Answered      bool   `json:"answered"`
	NeedsProvider bool   `json:"needs_provider,omitempty"`
	Reply         string `json:"reply,omitempty"`
	// NoExec (F-0323 crit. 7) marks a turn whose mode has no execution
	// channel: the UI draws no execution card and offers the commands to copy.
	NoExec bool `json:"no_exec,omitempty"`
	// Runs (F-0323) are the confirmed turn's tool executions, each with its
	// structured result — what the UI shows as ✓ id or ✗ code.
	Runs []chatRun `json:"runs,omitempty"`
	// NoRuns marks a confirmed tool-channel turn that executed nothing, and
	// Truncated one whose model output was cut by the token limit (C5).
	NoRuns    bool `json:"no_runs,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// handleChat answers one turn of the Backlog domain-agent chat (F-0210): the
// system prompt (persona + project context) is assembled fresh from the
// current .cernerc/backend on every call, then flattened with the transcript
// into a single Provider.Complete call — nothing is written to the backend
// here (criterion 6); only handleChatRegister does.
func handleChat(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
	// F-0323 C2: the chat executes writes — refuse the cross-site simple
	// request a page of another origin can fire without a preflight.
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("messages must not be empty"))
		return
	}
	// F-0323: a confirmation that does not bind to the card's turn is refused
	// loudly — never silently downgraded to a read-only turn.
	turn, confirmErr := confirmedTurn(req)
	confirmed := confirmErr == nil
	if req.Confirm && !confirmed {
		refuseConfirmation(w, "invalid confirmation: "+confirmErr.Error()+" (send conversation_id and confirm_turn with confirm)")
		return
	}
	// C9: a confirmation is single-use — a replayed body runs nothing.
	if confirmed {
		if _, used := usedConfirmations.LoadOrStore(fmt.Sprintf("%s\x00%s\x00%d", repoDir, req.ConversationID, turn), true); used {
			refuseConfirmation(w, "invalid confirmation: this card's confirmation was already used")
			return
		}
	}

	// F-0256: the refine context is resolved before anything else — a feature
	// past candidacy is refused here, whatever the provider situation, so the
	// gate holds for a direct API call and not only for the UI's hidden button.
	refine := ""
	if fid := strings.TrimSpace(req.FeatureID); fid != "" {
		block, err := app.BuildRefineBlock(repoDir, fid)
		if !writeRefineError(w, err) {
			return
		}
		refine = block
	}

	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeChatLine(w, http.StatusOK, chatResponse{Answered: false, NeedsProvider: true})
		return
	}
	if req.Model != "" {
		cfg.LLM.Model = req.Model
	}

	// F-0224: the chat provider runs claude-cli as an agent in the project dir;
	// the budget covers chained cerne queries, not just one shot. F-0260: the
	// confirmed turn — and only it — also gets the cerne creation commands.
	// F-0323: built before the prompt, which depends on its execution channel.
	// F-0323 C2: the execution channel exists only under the local ui — the
	// remote `cerne serve` never runs the catalog, in any mode.
	remote := servedRemotely(r)
	prov, err := newAgentProvider(cfg.LLM, projDir, confirmed && !remote)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	channel := execChannel(cfg.LLM, prov) && !remote

	sysPrompt, err := app.BuildChatSystemPrompt(repoDir, projDir, cfg, channel)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	sysPrompt += refine
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

	ctx, cancel := context.WithTimeout(r.Context(), cfg.Agent.ChatTimeoutOrDefault())
	defer cancel()
	prompt := app.BuildChatPrompt(sysPrompt, msgs)

	// F-0323: the confirmed turn of a function-calling provider executes
	// through the verb catalog — and only it is offered the tool. Single-shot
	// (the turn is short, and its terminal line is a valid one-event stream).
	if tc, ok := prov.(llm.ToolCaller); ok && confirmed && channel {
		var runs []chatRun
		// C3: the confirmation covers the card's commands, nothing else.
		env := verbs.Env{Bin: cerneBin(), ProjDir: projDir, RepoDir: repoDir, Confirmed: true,
			Card: cardKeys(req.Messages[turn].Content), Conversation: req.ConversationID, Turn: turn}
		reply, err := tc.CompleteTools(ctx, prompt, chatTools(CommandTree), toolRun(env, &runs))
		truncated := errors.Is(err, llm.ErrTruncated)
		if err != nil && !truncated {
			// Whatever already ran is reported with the failure: a write must
			// never go unreported because a later round broke.
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "runs": runs})
			return
		}
		// C5: a confirmed turn that executed nothing says so — never a silent
		// success, whatever the reply claims.
		writeChatLine(w, http.StatusOK, chatResponse{Answered: true, Reply: reply, Runs: runs, NoRuns: len(runs) == 0, Truncated: truncated})
		return
	}

	// F-0240: a provider with the Streamer capability transmits the reply as
	// NDJSON deltas; anyone else keeps the single-shot path below — whose body
	// is byte-for-byte the streaming path's terminal event (F-0244: a single
	// compact JSON line, same as streamChatReply's terminal Encode), so the
	// client never needs to know which path ran.
	if streamer, ok := prov.(llm.Streamer); ok {
		if flusher, ok := w.(http.Flusher); ok {
			streamChatReply(ctx, w, flusher, streamer, prompt, !channel)
			return
		}
	}
	reply, err := prov.Complete(ctx, prompt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeChatLine(w, http.StatusOK, chatResponse{Answered: true, Reply: reply, NoExec: !channel})
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
func streamChatReply(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, streamer llm.Streamer, prompt string, noExec bool) {
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
	_ = enc.Encode(chatResponse{Answered: true, Reply: reply, NoExec: noExec})
	flusher.Flush()
}

// chatRegisterRequest is the POST /api/chat/register body: the proposal the
// user confirmed in the chat, already parsed into title + spec body by the UI.
type chatRegisterRequest struct {
	Title      string   `json:"title"`
	SpecBody   string   `json:"spec_body"`
	Facets     []string `json:"facets"`
	Components []string `json:"components"`
	// Module classifies the candidate in projects that declare modules —
	// there ValidateModule makes it mandatory (2+ modules), so the chat
	// register must carry it or every confirmation click dies on the core.
	Module string `json:"module,omitempty"`
	// Capability (F-0223) is either an existing C-NNNN id (validated before
	// the candidate is written — a bad id must not spend the write) or, when
	// it doesn't match that shape, a title proposing a new node — created
	// and linked after the candidate exists.
	Capability string `json:"capability"`
	// Parent (F-0254) is the optional epic the proposal decomposes. A parent
	// that doesn't exist or is closed must not cost the registration: the
	// candidate is created without it and the response carries a warning.
	Parent string `json:"parent,omitempty"`
	// FeatureID (F-0256) switches the handler from create to update: the
	// confirmed revision rewrites that candidate's spec instead of minting a
	// new one. Tags/capability/parent are not re-applied — refining edits the
	// document, nothing else.
	FeatureID string `json:"feature_id,omitempty"`
}

// writeRefineError maps the refine path's refusals (F-0256) to a status and
// reports whether the caller may continue: unknown id → 404, past candidacy →
// 422 (the spec is a committed contract), anything else → 500. Shared by the
// turn (handleChat) and the write (handleChatRegister) so both gate alike.
func writeRefineError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
	case errors.Is(err, app.ErrNotCandidate), errors.Is(err, app.ErrBodyFrontmatter), errors.Is(err, app.ErrEmptyBody):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	}
	return false
}

// chatRegisterResponse is the 201 payload: the created feature, plus a warning
// when something non-fatal was dropped on the way in (F-0254: an invalid
// parent). Unchanged flags an update whose spec was already the stored one —
// a success that wrote nothing, not a warning (F-0328).
type chatRegisterResponse struct {
	Feature   store.Feature `json:"feature"`
	Warning   string        `json:"warning,omitempty"`
	Unchanged bool          `json:"unchanged,omitempty"`
}

// dupMatch is one collision the duplicate guard refused on, as the UI needs
// it (F-0264): the id is what the "update F-XXXX" action writes to, the state
// is what decides whether the action is offered at all.
type dupMatch struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Title string `json:"title"`
}

// dupConflictBody turns the guard's refusal into the 409 payload: the message
// without the --force tail (a CLI flag has no meaning in the UI, crit. 8) plus
// the structured collisions.
func dupConflictBody(err error) map[string]any {
	body := map[string]any{"error": "a similar registration already exists"}
	var dup *app.SimilarExistsError
	if !errors.As(err, &dup) {
		return body
	}
	matches := make([]dupMatch, 0, len(dup.Live))
	var lines []string
	for _, m := range dup.Live {
		matches = append(matches, dupMatch{ID: m.ID, State: m.State, Title: m.Title})
		lines = append(lines, fmt.Sprintf("%s [%s] %s", m.ID, m.State, m.Title))
	}
	body["matches"] = matches
	if len(lines) > 0 {
		body["error"] = fmt.Sprintf("a similar registration already exists: %s", strings.Join(lines, "; "))
	}
	return body
}

// handleChatRegister registers the confirmed proposal as a candidate — the
// same core as POST /api/features --file (app.CreateFeatureWithSpec), so the
// same duplicate-registration guard (F-0147) and module/facet validation
// apply. This is the only handler in the chat surface that touches the
// backend, and only after the user's explicit confirmation click.
func handleChatRegister(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !sameOriginJSON(r) { // F-0323 C2: a write route, same guard as the chat
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req chatRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if strings.TrimSpace(req.SpecBody) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("spec_body must not be empty"))
		return
	}
	// F-0256: update mode — the confirmed revision of an already-registered
	// candidate. Same gate as the creation (an explicit click got here), same
	// backend write path, but nothing is minted and no state moves.
	if fid := strings.TrimSpace(req.FeatureID); fid != "" {
		f, changed, err := app.UpdateCandidateSpec(repoDir, projDir, fid, req.SpecBody)
		if !writeRefineError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, chatRegisterResponse{Feature: f, Unchanged: !changed})
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
	parent := strings.TrimSpace(req.Parent)
	module := strings.TrimSpace(req.Module)
	warning := ""
	f, err := app.CreateFeatureWithSpec(repoDir, projDir, req.Title, module, parent, req.Facets, req.Components, req.SpecBody, false, "", "")
	// F-0254: a bad parent degrades to no parent instead of failing the
	// registration — the refusal happens before anything is written, so the
	// retry is clean. Only the parent errors qualify; everything else is fatal.
	if parent != "" && (errors.Is(err, app.ErrParentNotFound) || errors.Is(err, app.ErrParentTerminal)) {
		warning = fmt.Sprintf("registered without parent %s: %v", parent, err)
		f, err = app.CreateFeatureWithSpec(repoDir, projDir, req.Title, module, "", req.Facets, req.Components, req.SpecBody, false, "", "")
	}
	if errors.Is(err, app.ErrSimilarExists) {
		writeJSON(w, http.StatusConflict, dupConflictBody(err))
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
	writeJSON(w, http.StatusCreated, chatRegisterResponse{Feature: f, Warning: warning})
}

// chatNoteRequest is the POST /api/chat/note body (F-0249): the note the agent
// proposed in a ```cerne-nota block and the user confirmed. Informs carries the
// candidate the same conversation produced, when there was one — the trail
// conversa → nota → feature.
type chatNoteRequest struct {
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Facets     []string `json:"facets"`
	Components []string `json:"components"`
	Informs    []string `json:"informs,omitempty"`
}

// handleChatNote registers the confirmed note proposal — the chat surface's
// second write, with the same gate as handleChatRegister: the agent proposes,
// the user clicks, the server writes (F-0224/F-0245 stay intact).
//
// Tags are required here and not only in the UI: a note born without facet or
// component is lint debt on arrival (untagged-live, F-0097), and a direct API
// call must not be able to mint one. The note is born durable — domain
// vocabulary and rejected scope do not expire with a feature (F-0052).
func handleChatNote(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !sameOriginJSON(r) { // F-0323 C2: a write route, same guard as the chat
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req chatNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("title must not be empty"))
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("body must not be empty"))
		return
	}
	if len(req.Facets) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("facet is required: an untagged note is born as lint debt (untagged-live)"))
		return
	}
	if len(req.Components) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("at least one component is required: an untagged note is born as lint debt (untagged-live)"))
		return
	}
	n, err := app.CreateNote(repoDir, projDir, req.Title, req.Body, req.Informs, req.Facets, req.Components, true, false)
	if errors.Is(err, app.ErrSimilarExists) {
		writeJSON(w, http.StatusConflict, dupConflictBody(err)) // F-0324: same 409 shape as the feature routes
		return
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]store.Note{"note": n})
}

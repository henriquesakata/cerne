package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
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
	// Attachments (F-0520) are the ids POST /api/chat/attachments answered for
	// the images of this user message, under the request's conversation_id.
	Attachments []string `json:"attachments,omitempty"`
}

// chatRequest is the POST /api/chat body. Model/Effort (F-0218) are optional
// per-call overrides from the chat's model/effort selector: Model replaces
// cfg.LLM.Model; Effort fills cfg.LLM.Effort, which codex-cli applies
// (B-0006) and the other providers ignore.
type chatRequest struct {
	Messages []chatMessage `json:"messages"`
	Model    string        `json:"model,omitempty"`
	Effort   string        `json:"effort,omitempty"`
	// Provider (F-0440) is the vendor the human picked in the selector: empty
	// or the configured one runs the turn as configured, an installed
	// subscription CLI's runs it on that CLI; anything else is refused.
	Provider string `json:"provider,omitempty"`
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
	// Context (F-0436) lists the sources the server injected into this turn,
	// without their text — what the UI shows under the reply.
	Context []app.TurnSource `json:"context,omitempty"`
	// ProposalIssues (F-0438) is what the cerne-proposta block of this reply
	// still lacks, by the one spec-body check; the UI lists it on the card.
	ProposalIssues []doccheck.Issue `json:"proposal_issues,omitempty"`
	// AnsweredBy (F-0440) is what the turn really ran on, read from the turn's
	// config, never echoed from the request.
	AnsweredBy *answeredBy `json:"answered_by,omitempty"`
}

// answeredBy names the provider, model and effort that ran a turn. Model and
// effort are empty where the vendor is never given one (a subscription CLI
// whose model is the CLI's own default).
type answeredBy struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

func turnAnsweredBy(cfg project.LLMConfig, prov llm.Provider) *answeredBy {
	vendors := llm.ConfiguredProviders(cfg)
	if len(vendors) == 0 {
		return nil
	}
	by := &answeredBy{Provider: vendors[0]}
	switch mode := llm.ResolveMode(cfg.Mode); mode {
	case llm.ModeClaudeCLI:
		// The model is what the CLI itself reported it ran; the effort is the
		// one the turn passed via --effort (B-0007).
		if r, ok := prov.(interface{ AnsweredModel() string }); ok {
			by.Model = r.AnsweredModel()
		}
		if r, ok := prov.(interface{ AnsweredEffort() string }); ok {
			by.Effort = r.AnsweredEffort()
		}
	case llm.ModeCodexCLI, llm.ModeGeminiCLI, llm.ModeCursorCLI, llm.ModeCopilotCLI:
		if cfg.Provider == "" {
			by.Model = cfg.Model
		}
		if mode == llm.ModeCodexCLI {
			by.Effort = cfg.Effort
		}
		// The Copilot CLI may pick its own model (auto): name what it reported.
		if r, ok := prov.(interface{ AnsweredModel() string }); ok && mode == llm.ModeCopilotCLI && r.AnsweredModel() != "" {
			by.Model = r.AnsweredModel()
		}
	default:
		by.Model = cfg.WithDefaults().Model
	}
	return by
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
	// F-0520: the attachment ids are checked before anything runs.
	images, status, err := turnImages(req, projectID)
	if err != nil {
		writeJSON(w, status, errorBody(err.Error()))
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
	// F-0440: the turn's copy of the config only — ask, compile and drafts
	// keep the project's mode, and nothing here writes .cernerc.
	turnCfg, err := turnLLM(cfg.LLM, req.Provider, servedRemotely(r))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	}
	cfg.LLM = turnCfg
	if req.Model != "" {
		cfg.LLM.Model = req.Model
	}
	cfg.LLM.Effort = req.Effort
	// F-0520: a turn that cannot show the images to the model is refused, not
	// answered as if they were not there.
	// Only the message being sent is refused: images of older messages (the
	// provider was switched since) are left out, so the conversation goes on.
	if servedRemotely(r) || !llm.SupportsImages(llm.ResolveMode(cfg.LLM.Mode)) {
		if len(images[len(images)-1]) > 0 {
			writeJSON(w, http.StatusUnprocessableEntity, errorBody("this provider does not take images in the chat: remove the attachments or pick a provider that does"))
			return
		}
		clear(images)
	}

	// F-0224: the chat provider runs claude-cli as an agent in the project dir;
	// the budget covers chained cerne queries, not just one shot. F-0260: the
	// confirmed turn — and only it — also gets the cerne creation commands.
	// F-0323: built before the prompt, which depends on its execution channel.
	// F-0323 C2: the execution channel exists only under the local ui — the
	// remote `cerne serve` never runs the catalog, in any mode.
	remote := servedRemotely(r)
	agentDir := projDir
	codexFiles := llm.ResolveMode(cfg.LLM.Mode) == llm.ModeCodexCLI
	liveCodex := codexLiveQuery(projDir, llm.ResolveMode(cfg.LLM.Mode), remote)
	if codexFiles && !liveCodex {
		agentDir = ""
	}
	prov, err := newAgentProvider(cfg.LLM, agentDir, confirmed && !remote)
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
	if liveCodex {
		sysPrompt += app.ChatCodexLiveQuery(store.DocsLanguage(repoDir))
	}

	sysPrompt += refine
	msgs := make([]app.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = app.ChatMessage{Role: m.Role, Content: m.Content, Images: images[i]}
	}
	// F-0253: prime the turn server-side with the backend context relevant to
	// the last user message — every provider gets it, tool-use or not. A
	// broken index must not cost the turn, so retrieval failure only means no
	// injected block (the error is the index's problem, surfaced by
	// search/ask).
	var injected []app.TurnSource
	if dbPath, err := ensureIndex(repoDir, projectID); err == nil {
		turnCtx, err := app.BuildTurnContext(repoDir, dbPath, lastUserMessage(msgs), cfg.Agent.RetrievalBudgetOrDefault())
		if err == nil && turnCtx.Prompt != "" {
			sysPrompt += "\n" + turnCtx.Prompt
			injected = turnCtx.Sources
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), cfg.Agent.ChatTimeoutOrDefault())
	defer cancel()
	ctx = llm.WithImages(ctx, slices.Concat(images...))
	prompt := app.BuildChatPrompt(sysPrompt, msgs)

	// F-0438: the proposal block, if any, is checked once the reply is whole —
	// by the same check the approve and the lint run. Never corrected here.
	check := func(reply string) []doccheck.Issue {
		return proposalIssues(reply, store.DocsLanguage(repoDir), !cfg.Gates.GauntletEnabled())
	}

	// F-0323: the confirmed turn of a function-calling provider executes
	// through the verb catalog — and only it is offered the tool. Single-shot
	// (the turn is short, and its terminal line is a valid one-event stream).
	if tc, ok := prov.(llm.ToolCaller); ok && confirmed && channel {
		var runs []chatRun
		// C3: the confirmation covers the card's commands, nothing else.
		env := verbs.Env{Bin: cerneBin(), ProjDir: projDir, RepoDir: repoDir, Confirmed: true,
			Card: cardKeys(req.Messages[turn].Content), Conversation: req.ConversationID, Turn: turn, Provider: req.Provider, Model: cfg.LLM.Model}
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
		writeChatLine(w, http.StatusOK, chatResponse{Answered: true, Reply: reply, Runs: runs, NoRuns: len(runs) == 0, Truncated: truncated, Context: injected, ProposalIssues: check(reply), AnsweredBy: turnAnsweredBy(cfg.LLM, prov)})
		return
	}

	// F-0240: a provider with the Streamer capability transmits the reply as
	// NDJSON deltas; anyone else keeps the single-shot path below — whose body
	// is byte-for-byte the streaming path's terminal event (F-0244: a single
	// compact JSON line, same as streamChatReply's terminal Encode), so the
	// client never needs to know which path ran.
	if streamer, ok := prov.(llm.Streamer); ok {
		if flusher, ok := w.(http.Flusher); ok {
			streamChatReply(ctx, w, flusher, streamer, prompt, !channel, injected, check, turnAnsweredBy(cfg.LLM, prov))
			return
		}
	}
	reply, err := prov.Complete(ctx, prompt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeChatLine(w, http.StatusOK, chatResponse{Answered: true, Reply: reply, NoExec: !channel, Context: injected, ProposalIssues: check(reply), AnsweredBy: turnAnsweredBy(cfg.LLM, prov)})
}

// turnImages resolves the attachment ids of every message to the files still
// in the project cache (F-0520), one slice per message. An id outside the
// server's pattern, attachments on an agent message or without a valid
// conversation are a 400; more than the per-message cap is a 422. An id whose
// file is gone (expired, conversation deleted) is dropped: the turn runs
// without that image. Past maxChatImagesPerTurn the oldest are left out.
func turnImages(req chatRequest, projectID string) ([][]string, int, error) {
	images := make([][]string, len(req.Messages))
	for i, m := range req.Messages {
		if len(m.Attachments) == 0 {
			continue
		}
		if m.Role != "user" {
			return nil, http.StatusBadRequest, errors.New("only a user message carries attachments")
		}
		if len(m.Attachments) > maxChatImagesPerMessage {
			return nil, http.StatusUnprocessableEntity, fmt.Errorf("a message takes at most %d images", maxChatImagesPerMessage)
		}
		for _, id := range m.Attachments {
			path, err := chatAttachmentPath(projectID, req.ConversationID, id)
			if errors.Is(err, errBadAttachmentRef) {
				return nil, http.StatusBadRequest, err
			}
			if err != nil {
				return nil, http.StatusInternalServerError, err
			}
			if _, err := os.Stat(path); err == nil {
				images[i] = append(images[i], path)
			}
		}
	}
	// Every image is handed to the CLI again on each turn: keep the most
	// recent maxChatImagesPerTurn and leave the older ones out.
	for i, kept := len(images)-1, 0; i >= 0; i-- {
		if room := maxChatImagesPerTurn - kept; len(images[i]) > room {
			images[i] = images[i][len(images[i])-room:]
		}
		kept += len(images[i])
	}
	return images, 0, nil
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
func streamChatReply(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, streamer llm.Streamer, prompt string, noExec bool, injected []app.TurnSource, check func(string) []doccheck.Issue, by *answeredBy) {
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
	_ = enc.Encode(chatResponse{Answered: true, Reply: reply, NoExec: noExec, Context: injected, ProposalIssues: check(reply), AnsweredBy: by})
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
	// Model (F-0375) is the conversation's model selection, the same override
	// chatRequest carries: the spec's writer is recorded with it.
	Model string `json:"model,omitempty"`
	// Provider (F-0440) is the vendor that answered, as the chat request had it.
	Provider string `json:"provider,omitempty"`
}

// turnLLM is the [llm] config of one chat turn (F-0440): cfg as configured
// when provider is empty or the configured vendor's, otherwise the mode of the
// installed subscription CLI of that vendor, with the project's model dropped
// (it names another vendor's model). The caller's copy only: nothing is saved.
func turnLLM(cfg project.LLMConfig, provider string, remote bool) (project.LLMConfig, error) {
	if provider == "" || slices.Contains(llm.ConfiguredProviders(cfg), provider) {
		return cfg, nil
	}
	if !slices.Contains(reachableProviders(cfg, remote), provider) {
		return cfg, fmt.Errorf("provider %q is not available on this machine", provider)
	}
	return llm.ForProvider(cfg, provider), nil
}

// chatAuthor is who wrote a spec the chat proposed (F-0375): the project's
// [llm] config with the conversation's model; unknown when the config is
// unreadable — never a default it did not call.
func chatAuthor(projDir, model, provider string, remote bool) app.DocAuthor {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		return app.DocAuthor{Source: "unknown"}
	}
	// An unreachable provider keeps the project's own config (best effort: the
	// registration was already confirmed).
	if turn, err := turnLLM(cfg.LLM, provider, remote); err == nil {
		cfg.LLM = turn
	}
	if model != "" {
		cfg.LLM.Model = model
	}
	return app.LLMAuthor(cfg.LLM)
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
	// F-0435: a proposal cut short (a code fence closed it early) must neither land
	// as a candidate with a half spec nor overwrite a complete one (refine) — refuse before anything is written.
	if missing := doccheck.MissingSections([]byte(req.SpecBody), doclang.Spec, store.DocsLanguage(repoDir)); len(missing) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   "the proposal is missing template sections: " + strings.Join(missing, ", "),
			"missing": missing,
		})
		return
	}
	// F-0256: update mode — the confirmed revision of an already-registered
	// candidate. Same gate as the creation (an explicit click got here), same
	// backend write path, but nothing is minted and no state moves.
	if fid := strings.TrimSpace(req.FeatureID); fid != "" {
		f, changed, err := app.UpdateCandidateSpecBy(repoDir, projDir, fid, req.SpecBody, chatAuthor(projDir, req.Model, req.Provider, servedRemotely(r)))
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
	author := chatAuthor(projDir, req.Model, req.Provider, servedRemotely(r))
	f, err := app.CreateFeatureWithSpecBy(repoDir, projDir, req.Title, module, parent, req.Facets, req.Components, req.SpecBody, author)
	// F-0254: a bad parent degrades to no parent instead of failing the
	// registration — the refusal happens before anything is written, so the
	// retry is clean. Only the parent errors qualify; everything else is fatal.
	if parent != "" && (errors.Is(err, app.ErrParentNotFound) || errors.Is(err, app.ErrParentTerminal)) {
		warning = fmt.Sprintf("registered without parent %s: %v", parent, err)
		f, err = app.CreateFeatureWithSpecBy(repoDir, projDir, req.Title, module, "", req.Facets, req.Components, req.SpecBody, author)
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
	n, err := app.CreateNote(repoDir, projDir, req.Title, req.Body, req.Informs, req.Facets, req.Components, nil, "", true, false)
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

var proposalOpenRe = regexp.MustCompile("(`{3,})cerne-proposta")

// proposalIssues runs doccheck.CheckSpecBody on the spec body of the reply's
// LAST cerne-proposta block — the UI shows the latest revision (F-0438); nil
// when there is none. An unclosed block is an issue, not a clean pass. The
// fence rule is F-0435's: the block closes on the first bare line of at least
// as many backticks as opened it, so a three-backtick code block inside a
// four-backtick proposal does not cut it short.
func proposalIssues(reply string, lang doclang.Lang, light bool) []doccheck.Issue {
	reply = strings.ReplaceAll(reply, "\r\n", "\n")
	all := proposalOpenRe.FindAllStringSubmatchIndex(reply, -1)
	if len(all) == 0 {
		return nil
	}
	m := all[len(all)-1]
	unclosed := []doccheck.Issue{{Code: doccheck.IssueUnclosed, Severity: doccheck.SeverityError, Detail: "the proposal block was never closed"}}
	nl := strings.IndexByte(reply[m[0]:], '\n')
	if nl < 0 {
		return unclosed
	}
	rest := reply[m[0]+nl+1:]
	closer := regexp.MustCompile("(?m)^ {0,3}`{" + strconv.Itoa(m[3]-m[2]) + ",}[ \\t]*$")
	c := closer.FindStringIndex(rest)
	if c == nil {
		return unclosed
	}
	return doccheck.CheckSpecBody([]byte(doccheck.ProposalSpecBody(rest[:c[0]])), lang, light)
}

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// fakeChatProvider stands in for the LLM HTTP API. Since F-0240 the chat
// provider streams, so the fake speaks SSE: the reply travels as two
// content_block_delta events (forcing the incremental path in every test).
func fakeChatProvider(t *testing.T, gotPrompt *string, replyText string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if gotPrompt != nil {
			*gotPrompt = string(raw)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		runes := []rune(replyText)
		for _, chunk := range []string{string(runes[:len(runes)/2]), string(runes[len(runes)/2:])} {
			if chunk == "" {
				continue
			}
			data, _ := json.Marshal(map[string]any{
				"type":  "content_block_delta",
				"delta": map[string]string{"type": "text_delta", "text": chunk},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// doChatStream posts to /api/chat and parses the NDJSON response (F-0240):
// every line but the last must be a {"delta":...} event, returned in order;
// the last line is decoded into terminal.
func doChatStream(t *testing.T, h http.Handler, body string, terminal any) (deltas []string, rec *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/chat status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("empty chat response body")
	}
	for _, line := range lines[:len(lines)-1] {
		var ev struct {
			Delta *string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Delta == nil {
			t.Fatalf("non-terminal line is not a delta event: %q (err=%v)", line, err)
		}
		deltas = append(deltas, *ev.Delta)
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), terminal); err != nil {
		t.Fatalf("terminal line %q: %v", lines[len(lines)-1], err)
	}
	return deltas, rec
}

func configureChatByoKey(t *testing.T, projDir, baseURL string) {
	t.Helper()
	t.Setenv("TEST_CHAT_KEY", "sk-x")
	if err := project.SaveConfig(projDir, project.Config{
		ProjectID: "proj-chat",
		Backend:   "unused",
		LLM:       project.LLMConfig{Mode: "byo-key", BaseURL: baseURL, KeyEnv: "TEST_CHAT_KEY"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestChatByoKey: criterion 3 — with a provider configured, POST /api/chat
// calls it with the assembled system prompt + transcript and returns the
// reply. Since F-0240 the reply streams: delta events first, then the same
// terminal payload as before.
func TestChatByoKey(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001

	var gotPrompt string
	srv := fakeChatProvider(t, &gotPrompt, "Poderia detalhar o formato do relatório?")
	configureChatByoKey(t, projDir, srv.URL)

	var got struct {
		Answered      bool   `json:"answered"`
		NeedsProvider bool   `json:"needs_provider"`
		Reply         string `json:"reply"`
	}
	deltas, _ := doChatStream(t, New(repoDir, projDir, "proj-chat"),
		`{"messages":[{"role":"user","content":"Preciso exportar relatórios."}]}`, &got)
	if !got.Answered || got.NeedsProvider {
		t.Fatalf("answered=%v needs_provider=%v, want answered=true needs_provider=false", got.Answered, got.NeedsProvider)
	}
	if got.Reply != "Poderia detalhar o formato do relatório?" {
		t.Fatalf("reply = %q", got.Reply)
	}
	// F-0240 criterion 1: the text traveled incrementally, and the deltas
	// reassemble into the terminal reply.
	if len(deltas) < 2 {
		t.Fatalf("deltas = %d, want at least 2 (incremental delivery)", len(deltas))
	}
	if strings.Join(deltas, "") != got.Reply {
		t.Fatalf("deltas joined = %q, want the terminal reply", strings.Join(deltas, ""))
	}
	if !strings.Contains(gotPrompt, "F-0001") {
		t.Fatalf("prompt missing the project roster:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "Preciso exportar relatórios.") {
		t.Fatalf("prompt missing the user's message:\n%s", gotPrompt)
	}
}

// TestChatModelOverride: F-0218 criterion 5 — an optional "model" on the
// request body overrides cfg.LLM.Model for the byo-key provider's API call;
// "effort" travels on the request too but no provider applies it yet (no-op,
// per the spec's "sem suporte, omite silenciosamente").
func TestChatModelOverride(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var gotPrompt string
	srv := fakeChatProvider(t, &gotPrompt, "ok")
	configureChatByoKey(t, projDir, srv.URL)

	var got struct {
		Answered bool `json:"answered"`
	}
	_, _ = doChatStream(t, New(repoDir, projDir, "proj-chat"),
		`{"messages":[{"role":"user","content":"Oi"}],"model":"claude-opus-5","effort":"high"}`, &got)
	if !got.Answered {
		t.Fatalf("answered = %v, want true", got.Answered)
	}
	var sentToProvider struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal([]byte(gotPrompt), &sentToProvider); err != nil {
		t.Fatalf("unmarshal provider request: %v", err)
	}
	if sentToProvider.Model != "claude-opus-5" {
		t.Fatalf("provider request model = %q, want claude-opus-5", sentToProvider.Model)
	}
	if !sentToProvider.Stream {
		t.Fatalf("provider request stream = false, want true (F-0240)")
	}
}

// TestChatNoModelOverrideUsesConfigDefault: without "model" on the request,
// the provider gets the .cernerc default (regression guard for TestChatByoKey).
func TestChatNoModelOverrideUsesConfigDefault(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var gotPrompt string
	srv := fakeChatProvider(t, &gotPrompt, "ok")
	configureChatByoKey(t, projDir, srv.URL)

	var got struct {
		Answered bool `json:"answered"`
	}
	_, _ = doChatStream(t, New(repoDir, projDir, "proj-chat"),
		`{"messages":[{"role":"user","content":"Oi"}]}`, &got)
	var sentToProvider struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(gotPrompt), &sentToProvider); err != nil {
		t.Fatalf("unmarshal provider request: %v", err)
	}
	if sentToProvider.Model == "" || sentToProvider.Model == "claude-opus-5" {
		t.Fatalf("provider request model = %q, want the .cernerc default (unset here, so the byo-key WithDefaults value), not the override", sentToProvider.Model)
	}
}

// TestChatDelegation: criterion 5 — with no provider (delegation), the chat
// can't answer for a human in the browser: needs_provider, no fabrication.
func TestChatDelegation(t *testing.T) {
	repoDir, projDir := newBackend(t) // no .cernerc → delegation

	var got struct {
		Answered      bool   `json:"answered"`
		NeedsProvider bool   `json:"needs_provider"`
		Reply         string `json:"reply"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-del"), http.MethodPost, "/api/chat",
		`{"messages":[{"role":"user","content":"Oi"}]}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Answered || !got.NeedsProvider {
		t.Fatalf("answered=%v needs_provider=%v, want answered=false needs_provider=true", got.Answered, got.NeedsProvider)
	}
	if got.Reply != "" {
		t.Fatalf("reply = %q, want empty in delegation (no fabrication)", got.Reply)
	}
}

// TestChatNonStreamerFallback (F-0240 criterion 2): a provider without the
// Streamer capability keeps the current single-shot path — one plain JSON
// write, no delta events, same terminal payload.
func TestChatNonStreamerFallback(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	configureChatByoKey(t, projDir, "http://unused.invalid")

	orig := newAgentProvider
	newAgentProvider = func(project.LLMConfig, string) (llm.Provider, error) {
		return &llm.Fake{Reply: "resposta inteira de uma vez"}, nil
	}
	t.Cleanup(func() { newAgentProvider = orig })

	var got struct {
		Answered bool   `json:"answered"`
		Reply    string `json:"reply"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat"), http.MethodPost, "/api/chat",
		`{"messages":[{"role":"user","content":"Oi"}]}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !got.Answered || got.Reply != "resposta inteira de uma vez" {
		t.Fatalf("answered=%v reply=%q, want the full reply in one shot", got.Answered, got.Reply)
	}
	if strings.Contains(rec.Body.String(), `"delta"`) {
		t.Fatalf("fallback body must have no delta events: %s", rec.Body.String())
	}
}

// TestChatStreamMidError (F-0240 criterion 8): the stream breaking after the
// first delta keeps what was already sent and closes with an error terminal —
// the partial is not discarded, the failure not silent.
func TestChatStreamMidError(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"começo da resposta"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"error","error":{"message":"overloaded"}}`+"\n\n")
	}))
	t.Cleanup(srv.Close)
	configureChatByoKey(t, projDir, srv.URL)

	var terminal struct {
		Error string `json:"error"`
	}
	deltas, _ := doChatStream(t, New(repoDir, projDir, "proj-chat"),
		`{"messages":[{"role":"user","content":"Oi"}]}`, &terminal)
	if len(deltas) != 1 || deltas[0] != "começo da resposta" {
		t.Fatalf("deltas = %#v, want the partial delivered before the error", deltas)
	}
	if !strings.Contains(terminal.Error, "overloaded") {
		t.Fatalf("terminal error = %q, want the provider's message", terminal.Error)
	}
}

// TestChatEmptyMessages: an empty transcript is a 422, not a call to the model.
func TestChatEmptyMessages(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/chat", `{"messages":[]}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

// TestChatNoConversationPersisted: criterion 6 — a chat call never writes to
// the backend (no new entity, origin HEAD unchanged).
func TestChatNoConversationPersisted(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	srv := fakeChatProvider(t, nil, "Entendi, me conte mais.")
	configureChatByoKey(t, projDir, srv.URL)

	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}
	featuresBefore, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures before: %v", err)
	}

	var got map[string]any
	_, _ = doChatStream(t, New(repoDir, projDir, "proj-chat"),
		`{"messages":[{"role":"user","content":"Preciso de algo."}]}`, &got)

	head1, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if head1 != head0 {
		t.Fatalf("origin HEAD advanced after a plain chat call — conversation must be ephemeral")
	}
	featuresAfter, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures after: %v", err)
	}
	if len(featuresAfter) != len(featuresBefore) {
		t.Fatalf("features changed after a plain chat call: before %d, after %d", len(featuresBefore), len(featuresAfter))
	}
}

// TestChatRegister: criterion 4 — confirming a proposal registers the
// candidate through the same core as POST /api/features --file.
func TestChatRegister(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)

	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-reg"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar relatórios em CSV\n\n## Problema\nFalta exportação.\n"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.Title != "Exportar relatórios em CSV" || got.Feature.State != domain.StateCandidate {
		t.Fatalf("feature = %+v, want candidate 'Exportar relatórios em CSV'", got.Feature)
	}

	head1, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if head1 == head0 {
		t.Fatalf("origin HEAD did not advance after registration")
	}
}

// TestChatRegisterExistingCapability: F-0223 criterion 2 — a proposal that
// names an existing C- id links it to the new candidate in the same act.
func TestChatRegisterExistingCapability(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	if err := store.SaveCapability(repoDir, store.Capability{ID: "C-0001", Title: "Financeiro", State: domain.CapabilityActive}); err != nil {
		t.Fatalf("SaveCapability: %v", err)
	}
	commitBackend(t, repoDir)

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-cap"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar relatórios em CSV\n\n## Problema\nFalta exportação.\n","capability":"C-0001"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Feature.Capabilities) != 1 || got.Feature.Capabilities[0] != "C-0001" {
		t.Fatalf("Capabilities = %v, want [C-0001]", got.Feature.Capabilities)
	}
}

// TestChatRegisterUnknownCapability: an unknown C- id refuses with nothing
// written — the candidate is never created (F-0223 criterion 2).
func TestChatRegisterUnknownCapability(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-cap-bad"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar relatórios em CSV\n\n## Problema\nFalta exportação.\n","capability":"C-9999"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	for _, f := range features {
		if f.Title == "Exportar relatórios em CSV" {
			t.Fatalf("candidate written despite unknown capability: %+v", f)
		}
	}
	head1, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if head1 != head0 {
		t.Fatalf("origin HEAD advanced on a refused register")
	}
}

// TestChatRegisterNewCapability: a non-id capability string is treated as a
// new node title, created and linked in the same act.
func TestChatRegisterNewCapability(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-cap-new"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar relatórios em CSV\n\n## Problema\nFalta exportação.\n","capability":"Exportação de dados"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Feature.Capabilities) != 1 {
		t.Fatalf("Capabilities = %v, want one new node linked", got.Feature.Capabilities)
	}
	caps, err := store.LoadAllCapabilities(repoDir)
	if err != nil {
		t.Fatalf("LoadAllCapabilities: %v", err)
	}
	if len(caps) != 1 || caps[0].Title != "Exportação de dados" {
		t.Fatalf("capabilities = %+v, want one new node titled 'Exportação de dados'", caps)
	}
}

// TestChatRegisterThenLintClean: F-0223 criterion 4 — a candidate registered
// via chat with facet+capability passes lint without capability-orphan or
// untagged-live for the new feature.
func TestChatRegisterThenLintClean(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-lint"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar relatórios em CSV\n\n## Problema\nFalta exportação.\n","facets":["code"],"capability":"Exportação de dados"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	findings, err := lint.Run(repoDir)
	if err != nil {
		t.Fatalf("lint.Run: %v", err)
	}
	for _, f := range findings {
		if f.Entity != got.Feature.ID {
			continue
		}
		if f.Rule == lint.RuleCapabilityOrphan || f.Rule == lint.RuleUntaggedLive {
			t.Fatalf("finding %+v on freshly-registered chat candidate", f)
		}
	}
}

// TestChatRegisterEmptySpec: an empty spec_body is refused (nothing to write).
func TestChatRegisterEmptySpec(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/chat/register",
		`{"title":"Sem corpo","spec_body":""}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

// TestChatRegisterDuplicate: the same exact-title guard (F-0121) that POST
// /api/features enforces applies here — same status as handleCreateFeature.
func TestChatRegisterDuplicate(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeature(t, repoDir, "F-0002", "Exportar relatórios em CSV", domain.StateCandidate)
	commitBackend(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-dup"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar relatórios em CSV\n\nCorpo.\n"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got["error"], "already exists") {
		t.Fatalf("error = %q, want a duplicate-title message", got["error"])
	}
}

// TestChatClaudeCLIRoutesAgentProvider (F-0224): with mode=claude-cli the
// handler builds the agentic provider — a claude_bin that cannot be started
// surfaces as a 500 naming the binary, proving the request routed to the CLI
// path (and not to byo-key/delegation).
func TestChatClaudeCLIRoutesAgentProvider(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b",
		LLM: project.LLMConfig{Mode: "claude-cli", ClaudeBin: filepath.Join(t.TempDir(), "missing-claude")}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var got struct {
		Error string `json:"error"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/chat",
		`{"messages":[{"role":"user","content":"oi"}]}`, &got)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got.Error, "missing-claude") {
		t.Fatalf("error %q does not name the claude binary (did the request route to the CLI provider?)", got.Error)
	}
}

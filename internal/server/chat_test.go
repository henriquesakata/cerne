package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	newAgentProvider = func(project.LLMConfig, string, bool) (llm.Provider, error) {
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

	// F-0244: the client's streamChat (ui/src/api.ts) treats every
	// non-streaming response as a one-line NDJSON stream — indented
	// multi-line JSON here breaks JSON.parse on the first line alone.
	body := strings.TrimRight(rec.Body.String(), "\n")
	if strings.Contains(body, "\n") {
		t.Fatalf("fallback body must be a single JSON line, got:\n%s", rec.Body.String())
	}
	if err := json.Unmarshal([]byte(body), &struct{}{}); err != nil {
		t.Fatalf("fallback body line must parse standalone: %v; body: %s", err, body)
	}
}

// TestChatConfirmArmsWriteTools (F-0260 criterion 3) is the whole gate: without
// an explicit confirmation on the turn the server does not even hand the agent
// the write tools, so "no write happened" is not a matter of the agent behaving
// — the capability was never granted. The confirmed turn is the only one that
// widens the allowlist, and it widens it for that call alone.
func TestChatConfirmArmsWriteTools(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	configureChatByoKey(t, projDir, "http://unused.invalid")

	var gotWrite []bool
	orig := newAgentProvider
	newAgentProvider = func(_ project.LLMConfig, _ string, write bool) (llm.Provider, error) {
		gotWrite = append(gotWrite, write)
		return &llm.Fake{Reply: "ok"}, nil
	}
	t.Cleanup(func() { newAgentProvider = orig })

	h := New(repoDir, projDir, "proj-chat-confirm")
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"absent", `{"messages":[{"role":"user","content":"registra a candidata"}]}`, false},
		{"explicitly false", `{"messages":[{"role":"user","content":"oi"}],"confirm":false}`, false},
		{"confirmed", `{"messages":[{"role":"user","content":"confirmo"}],"confirm":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotWrite = nil
			var got struct{}
			if rec := doJSON(t, h, http.MethodPost, "/api/chat", tc.body, &got); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			if len(gotWrite) != 1 || gotWrite[0] != tc.want {
				t.Fatalf("provider write = %v, want [%v]", gotWrite, tc.want)
			}
		})
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

// TestChatRegisterModule: in a project that declares 2+ modules, the register
// must carry the proposal's module through to the creation core — without it
// ValidateModule refuses and the confirmation click dies with "--module is
// required" (the reported bug).
func TestChatRegisterModule(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	if err := os.WriteFile(filepath.Join(repoDir, "project.json"),
		[]byte(`{"name":"Projeto Demo","modules":["finance","commons"]}`), 0o644); err != nil {
		t.Fatalf("WriteFile project.json: %v", err)
	}
	commitBackend(t, repoDir)

	// Without module the core refuses — the handler must surface that, not 500.
	var refuse map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-mod"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# X\n\n## Problema\nY.\n"}`, &refuse)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status without module = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}

	// With module the candidate is born classified.
	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec = doJSON(t, New(repoDir, projDir, "proj-chat-mod"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# X\n\n## Problema\nY.\n","module":"commons"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status with module = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.Module != "commons" {
		t.Fatalf("Module = %q, want %q", got.Feature.Module, "commons")
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

// TestChatRegisterSimilarReturnsMatches (F-0264, crit. 1/5/8): the similarity
// guard's 409 carries the collisions structured, so an orphan conversation can
// offer "update F-XXXX" instead of dead-ending; the message drops the --force
// tail (CLI chrome).
func TestChatRegisterSimilarReturnsMatches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state domain.FeatureState
	}{
		{"candidate", domain.StateCandidate},
		{"approved", domain.StateApproved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newGitBackend(t)
			seedFeature(t, repoDir, "F-0002", "Exportar relatórios em CSV", tc.state)
			commitBackend(t, repoDir)

			var got struct {
				Error   string `json:"error"`
				Matches []struct {
					ID    string `json:"id"`
					State string `json:"state"`
					Title string `json:"title"`
				} `json:"matches"`
			}
			rec := doJSON(t, New(repoDir, projDir, "proj-chat-sim"), http.MethodPost, "/api/chat/register",
				`{"title":"Exportar relatórios em CSV agora","spec_body":"# X\n\nCorpo.\n"}`, &got)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body: %s", rec.Code, rec.Body.String())
			}
			if len(got.Matches) != 1 || got.Matches[0].ID != "F-0002" || got.Matches[0].State != string(tc.state) {
				t.Fatalf("matches = %+v, want F-0002 [%s]", got.Matches, tc.state)
			}
			if strings.Contains(got.Error, "--force") {
				t.Fatalf("error = %q, want no --force chrome", got.Error)
			}
		})
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

// TestChatInjectsProjectContext (F-0253, criteria 2 and 3): the turn's prompt
// carries the backend context the server retrieved — here with a byo-key
// provider, which has no tool access at all, proving the injection is
// server-side and not the claude-cli consultation of F-0224.
func TestChatInjectsProjectContext(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	configureChatByoKey(t, projDir, "http://unused.invalid")
	if err := store.SaveDecision(repoDir, store.Decision{
		ID: "D-0001", Title: "Backend git é a fonte da verdade", State: domain.DecisionActive,
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	fake := &llm.Fake{Reply: "ok"}
	orig := newAgentProvider
	newAgentProvider = func(project.LLMConfig, string, bool) (llm.Provider, error) { return fake, nil }
	t.Cleanup(func() { newAgentProvider = orig })

	var got struct {
		Answered bool `json:"answered"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat"), http.MethodPost, "/api/chat",
		`{"messages":[{"role":"user","content":"qual a fonte da verdade?"}]}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(fake.Prompts) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(fake.Prompts))
	}
	for _, want := range []string{"Contexto recuperado do projeto", "D-0001"} {
		if !strings.Contains(fake.Prompts[0], want) {
			t.Fatalf("prompt missing %q:\n%s", want, fake.Prompts[0])
		}
	}
}

// TestChatRegisterParent: F-0254 criterion 5 — a valid parent in the proposal
// registers the candidate under that epic.
func TestChatRegisterParent(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got struct {
		Feature store.Feature `json:"feature"`
		Warning string        `json:"warning"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-parent"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar\n\n## Problema\nFalta exportação.\n","parent":"F-0001"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.Parent != "F-0001" {
		t.Fatalf("parent = %q, want F-0001", got.Feature.Parent)
	}
	if got.Warning != "" {
		t.Fatalf("unexpected warning: %q", got.Warning)
	}
}

// TestChatRegisterBadParent: F-0254 criterion 5 — an unknown parent must not
// cost the registration: the candidate lands without parent, plus a warning.
func TestChatRegisterBadParent(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got struct {
		Feature store.Feature `json:"feature"`
		Warning string        `json:"warning"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-parent-bad"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar relatórios em CSV","spec_body":"# Exportar\n\n## Problema\nFalta exportação.\n","parent":"F-9999"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.Parent != "" {
		t.Fatalf("parent = %q, want empty", got.Feature.Parent)
	}
	if !strings.Contains(got.Warning, "F-9999") {
		t.Fatalf("warning = %q, want it to name the dropped parent", got.Warning)
	}
}

// seedSpec writes a feature's spec.md straight into the backend clone and
// commits it — the starting point of a refine conversation (F-0256).
func seedSpec(t *testing.T, repoDir, id, body string) {
	t.Helper()
	path := filepath.Join(repoDir, "content", "features", id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("---\ncerne_id: "+id+"\n---\n\n"+body), 0o644); err != nil {
		t.Fatalf("write spec %s: %v", id, err)
	}
	commitBackend(t, repoDir)
}

// TestChatRefineLoadsCurrentSpec (F-0256 criteria 1/6): a turn carrying
// feature_id hands the agent the candidate's spec as it stands in the backend
// — and rereads it on the next turn, so a spec that moved under a resumed
// conversation is never answered from a stale snapshot.
func TestChatRefineLoadsCurrentSpec(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001 as candidate
	seedSpec(t, repoDir, "F-0001", "## Problema\n\nFalta exportação em CSV.\n")

	var gotPrompt string
	srv := fakeChatProvider(t, &gotPrompt, "ok")
	configureChatByoKey(t, projDir, srv.URL)
	h := New(repoDir, projDir, "proj-chat-refine")

	var got struct {
		Answered bool `json:"answered"`
	}
	_, _ = doChatStream(t, h, `{"messages":[{"role":"user","content":"Ajusta o escopo."}],"feature_id":"F-0001"}`, &got)
	if !got.Answered {
		t.Fatalf("answered = %v, want true", got.Answered)
	}
	for _, want := range []string{"F-0001", "Falta exportação em CSV"} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, gotPrompt)
		}
	}

	// The spec advances between turns (another branch, the CLI, whatever).
	seedSpec(t, repoDir, "F-0001", "## Problema\n\nExportar também em XLSX.\n")
	_, _ = doChatStream(t, h, `{"messages":[{"role":"user","content":"E agora?"}],"feature_id":"F-0001"}`, &got)
	if !strings.Contains(gotPrompt, "XLSX") || strings.Contains(gotPrompt, "Falta exportação em CSV") {
		t.Fatalf("second turn did not reload the current spec:\n%s", gotPrompt)
	}
}

// TestChatRefineRefusesNonCandidate (F-0256 criterion 5): past candidacy the
// refusal is the server's, not the UI's — a direct API call gets 422 and the
// provider is never even asked.
func TestChatRefineRefusesNonCandidate(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeature(t, repoDir, "F-0002", "Aprovada", domain.StateApproved)
	seedSpec(t, repoDir, "F-0002", "## Problema\n\nContrato aprovado.\n")

	var gotPrompt string
	srv := fakeChatProvider(t, &gotPrompt, "ok")
	configureChatByoKey(t, projDir, srv.URL)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-refine-bad"), http.MethodPost, "/api/chat",
		`{"messages":[{"role":"user","content":"Muda a spec."}],"feature_id":"F-0002"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if gotPrompt != "" {
		t.Fatalf("provider was called for a refused refine:\n%s", gotPrompt)
	}
}

// TestChatRegisterUpdate (F-0256 criteria 3/4): confirming a revision with
// feature_id rewrites that candidate's spec — no new candidate, no state move.
func TestChatRegisterUpdate(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	seedSpec(t, repoDir, "F-0001", "## Problema\n\nVersão antiga.\n")
	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-upd"), http.MethodPost, "/api/chat/register",
		`{"feature_id":"F-0001","title":"Feature A","spec_body":"## Problema\n\nVersão nova.\n"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.ID != "F-0001" || got.Feature.State != domain.StateCandidate {
		t.Fatalf("feature = %+v, want F-0001 still candidate", got.Feature)
	}
	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if !strings.Contains(string(spec), "Versão nova.") || !strings.Contains(string(spec), "cerne_id: F-0001") {
		t.Fatalf("spec.md = %q, want the new body with the cerne_id preserved", string(spec))
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("features = %d, want 1 (an update must not mint a candidate)", len(features))
	}
	if head1, _ := gitback.HeadSHA(origin); head1 == head0 {
		t.Fatalf("origin HEAD did not advance after the spec update")
	}
}

// TestChatRegisterUpdateRefusals (F-0256 criterion 5): an unknown id is 404, a
// feature past candidacy is 422, and neither touches the backend.
func TestChatRegisterUpdateRefusals(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeature(t, repoDir, "F-0002", "Aprovada", domain.StateApproved)
	seedSpec(t, repoDir, "F-0002", "## Problema\n\nContrato aprovado.\n")
	h := New(repoDir, projDir, "proj-chat-upd-bad")

	var got map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/chat/register",
		`{"feature_id":"F-9999","title":"X","spec_body":"## Problema\n\nX.\n"}`, &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodPost, "/api/chat/register",
		`{"feature_id":"F-0002","title":"Aprovada","spec_body":"## Problema\n\nBurlado.\n"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("approved status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0002", "spec.md"))
	if err != nil || !strings.Contains(string(spec), "Contrato aprovado.") {
		t.Fatalf("spec.md changed on a refused update: %q (%v)", string(spec), err)
	}
}

// TestChatNote (F-0249 criteria 3/7): confirming a proposed note writes it to
// the backend as an active, durable note carrying the block's tags and the
// informs backlink to the candidate the conversation produced.
func TestChatNote(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	seedFeature(t, repoDir, "F-0001", "Candidata da conversa", domain.StateCandidate)
	commitBackend(t, repoDir)
	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	var got struct {
		Note store.Note `json:"note"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-note"), http.MethodPost, "/api/chat/note",
		`{"title":"Candidata é a proposta antes do aprove","body":"No vocabulário deste produto, candidata é a feature proposta e ainda não aprovada.","facets":["code"],"components":["backlog"],"informs":["F-0001"]}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.Note.State != domain.NoteActive || !got.Note.Durable {
		t.Fatalf("note = %+v, want active + durable", got.Note)
	}
	if len(got.Note.Facets) != 1 || got.Note.Facets[0] != "code" {
		t.Fatalf("facets = %v, want [code]", got.Note.Facets)
	}
	if len(got.Note.Components) != 1 || got.Note.Components[0] != "backlog" {
		t.Fatalf("components = %v, want [backlog]", got.Note.Components)
	}
	if len(got.Note.Informs) != 1 || got.Note.Informs[0] != "F-0001" {
		t.Fatalf("informs = %v, want [F-0001]", got.Note.Informs)
	}
	body, err := os.ReadFile(filepath.Join(repoDir, "content", "notes", got.Note.ID+".md"))
	if err != nil || !strings.Contains(string(body), "candidata é a feature proposta") {
		t.Fatalf("note body not written: %q (%v)", string(body), err)
	}
	head1, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if head1 == head0 {
		t.Fatalf("origin HEAD did not advance after the note was created")
	}
}

// TestChatNoteRequiresTags (F-0249 criterion 5): a block without facet or
// without at least one component is refused with an explicit message and
// writes nothing — an untagged note is born as lint debt (untagged-live).
func TestChatNoteRequiresTags(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "proj-chat-note-tags")

	for _, tc := range []struct{ name, payload, want string }{
		{"no facet", `{"title":"T","body":"corpo real da nota","components":["backlog"]}`, "facet"},
		{"no component", `{"title":"T","body":"corpo real da nota","facets":["code"]}`, "component"},
		{"empty body", `{"title":"T","body":"  ","facets":["code"],"components":["backlog"]}`, "body"},
		{"empty title", `{"title":" ","body":"corpo real da nota","facets":["code"],"components":["backlog"]}`, "title"},
	} {
		var got map[string]string
		rec := doJSON(t, h, http.MethodPost, "/api/chat/note", tc.payload, &got)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422; body: %s", tc.name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(got["error"], tc.want) {
			t.Fatalf("%s: error = %q, want mention of %q", tc.name, got["error"], tc.want)
		}
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		t.Fatalf("LoadAllNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("refused proposals wrote %d note(s), want 0", len(notes))
	}
}

// TestChatNoteUnknownInforms (F-0249): an informs target that does not exist
// refuses instead of minting a note with a dangling backlink.
func TestChatNoteUnknownInforms(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-note-informs"), http.MethodPost, "/api/chat/note",
		`{"title":"T","body":"corpo real da nota","facets":["code"],"components":["backlog"],"informs":["F-9999"]}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if notes, _ := store.LoadAllNotes(repoDir); len(notes) != 0 {
		t.Fatalf("dangling informs wrote %d note(s), want 0", len(notes))
	}
}

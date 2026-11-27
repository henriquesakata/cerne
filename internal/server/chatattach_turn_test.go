package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
)

// imageSpy is a provider that records the turn's prompt and the images its
// context carried.
type imageSpy struct {
	prompt string
	images []string
	calls  int
}

func (s *imageSpy) Complete(ctx context.Context, prompt string) (string, error) {
	s.calls++
	s.prompt, s.images = prompt, llm.ImagesFrom(ctx)
	return "ok", nil
}

// attachTurn is a claude-cli project over a temp cache, with the provider
// swapped for a spy and one image already uploaded to conv-1.
func attachTurn(t *testing.T, mode string) (h http.Handler, spy *imageSpy, id, cache, repoDir, projDir string) {
	t.Helper()
	cache = t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	repoDir, projDir, _ = newGitBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "proj-attach", Backend: "unused", LLM: project.LLMConfig{Mode: mode, KeyEnv: "TEST_CHAT_KEY"}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	t.Setenv("TEST_CHAT_KEY", "sk-x")
	spy = &imageSpy{}
	orig := newAgentProvider
	newAgentProvider = func(project.LLMConfig, string, bool) (llm.Provider, error) { return spy, nil }
	t.Cleanup(func() { newAgentProvider = orig })
	h = New(repoDir, projDir, "proj-attach")
	id = uploadedID(t, uploadAttachment(t, h, "conv-1", "image/png", pngBytes))
	return h, spy, id, cache, repoDir, projDir
}

func chatBody(conv string, attachments ...string) string {
	ids, _ := json.Marshal(attachments)
	return fmt.Sprintf(`{"conversation_id":%q,"messages":[{"role":"user","content":"veja a tela","attachments":%s}]}`, conv, ids)
}

func postChat(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodPost, "/api/chat", strings.NewReader(body)))
	return rec
}

// F-0520 crit. 5: the turn resolves each id to its file in the cache, hands
// the paths to the provider and names them under the message.
func TestChatTurnCarriesAttachedImages(t *testing.T) {
	h, spy, id, cache, _, _ := attachTurn(t, llm.ModeClaudeCLI)
	rec := postChat(h, chatBody("conv-1", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	path := filepath.Join(cache, "proj-attach", "chat-attachments", "conv-1", id)
	if !slices.Equal(spy.images, []string{path}) {
		t.Fatalf("provider images = %v, want [%s]", spy.images, path)
	}
	if !strings.Contains(spy.prompt, "User: veja a tela\n[attached image: "+path+"]\n") {
		t.Fatalf("prompt does not name the image under its message:\n%s", spy.prompt)
	}
}

// F-0520 crit. 9: an attachment that expired is dropped and the turn runs.
func TestChatTurnDropsExpiredAttachment(t *testing.T) {
	h, spy, id, _, _, _ := attachTurn(t, llm.ModeClaudeCLI)
	rec := postChat(h, chatBody("conv-1", "0123456789abcdef.png", id))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(spy.images) != 1 || !strings.HasSuffix(spy.images[0], id) || strings.Contains(spy.prompt, "0123456789abcdef.png") {
		t.Fatalf("images = %v, want only the one still on disk; prompt:\n%s", spy.images, spy.prompt)
	}
}

// F-0520 crit. 8; F-0524 crit. 6: a turn with attachments on a mode that takes
// no image is refused instead of answered blind.
func TestChatTurnRefusesImagesOnModeWithoutThem(t *testing.T) {
	for _, mode := range []string{llm.ModeGeminiCLI} { // F-0522 crit. 7; F-0524 crit. 6
		h, spy, id, _, _, _ := attachTurn(t, mode)
		rec := postChat(h, chatBody("conv-1", id))
		if rec.Code != http.StatusUnprocessableEntity || spy.calls != 0 {
			t.Errorf("%s: status = %d, provider calls = %d, want 422 and no call; body: %s", mode, rec.Code, spy.calls, rec.Body.String())
		}
		// The same mode still answers a turn without images.
		if rec := postChat(h, chatBody("conv-1")); rec.Code != http.StatusOK {
			t.Errorf("%s: plain turn status = %d, want 200", mode, rec.Code)
		}
	}
}

// F-0520 crit. 8; F-0524 crit. 6: the remote serve never takes an attachment,
// even when the local mode supports images.
func TestChatTurnRefusesImagesRemotely(t *testing.T) {
	for _, mode := range []string{llm.ModeClaudeCLI, llm.ModeCursorCLI} {
		_, spy, id, _, repoDir, projDir := attachTurn(t, mode)
		remote := markRemote(func(w http.ResponseWriter, r *http.Request) { handleChat(w, r, repoDir, projDir, "proj-attach") })
		rec := httptest.NewRecorder()
		remote(rec, localRequest(http.MethodPost, "/api/chat", strings.NewReader(chatBody("conv-1", id))))
		if rec.Code != http.StatusUnprocessableEntity || spy.calls != 0 {
			t.Fatalf("%s: status = %d, provider calls = %d, want 422 and no call; body: %s", mode, rec.Code, spy.calls, rec.Body.String())
		}
	}
}

// F-0520 crit. 4, 11: more than five attachments on a message, an id outside
// the server's pattern, or attachments with no valid conversation are refused.
func TestChatTurnRefusesBadAttachments(t *testing.T) {
	h, spy, id, _, _, _ := attachTurn(t, llm.ModeClaudeCLI)
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"six on one message":     {chatBody("conv-1", id, id, id, id, id, id), http.StatusUnprocessableEntity},
		"path as id":             {chatBody("conv-1", "../../../etc/passwd"), http.StatusBadRequest},
		"absolute path as id":    {chatBody("conv-1", "/etc/passwd"), http.StatusBadRequest},
		"no conversation id":     {chatBody("", id), http.StatusBadRequest},
		"conversation traversal": {chatBody("..", id), http.StatusBadRequest},
		"on an agent message":    {`{"conversation_id":"conv-1","messages":[{"role":"assistant","content":"x","attachments":["` + id + `"]},{"role":"user","content":"y"}]}`, http.StatusBadRequest},
	} {
		if rec := postChat(h, tc.body); rec.Code != tc.want || spy.calls != 0 {
			t.Errorf("%s: status = %d, provider calls = %d, want %d and no call; body: %s", name, rec.Code, spy.calls, tc.want, rec.Body.String())
		}
	}
}

// F-0520 crit. 8; F-0524 crit. 5, 6: the UI learns which reachable providers
// take images.
func TestLLMConfigListsImageProviders(t *testing.T) {
	for mode, want := range map[string][]string{
		llm.ModeClaudeCLI: {"anthropic"},
		llm.ModeCursorCLI: {"cursor"},
		llm.ModeGeminiCLI: nil,
	} {
		cfg := project.LLMConfig{Mode: mode}
		got := imageProviders(cfg, llm.ConfiguredProviders(cfg), false)
		if !slices.Equal(got, want) {
			t.Errorf("mode %s: image providers = %v, want %v", mode, got, want)
		}
	}
	// F-0522 crit. 4; F-0524 crit. 5, 6: the project's own vendor, reached by
	// API key, takes images, and so does a CLI with an image input found on
	// PATH (cursor included); gemini does not.
	if got := imageProviders(project.LLMConfig{Mode: "byo-key", Provider: "anthropic"}, []string{"anthropic", "openai", "cursor", "gemini"}, false); !slices.Equal(got, []string{"anthropic", "openai", "cursor"}) {
		t.Errorf("image providers = %v, want [anthropic openai cursor]", got)
	}
	for _, mode := range []string{llm.ModeClaudeCLI, llm.ModeCursorCLI} {
		if got := imageProviders(project.LLMConfig{Mode: mode}, llm.ConfiguredProviders(project.LLMConfig{Mode: mode}), true); len(got) != 0 {
			t.Errorf("remote %s image providers = %v, want none", mode, got)
		}
	}
}

// F-0520 crit. 12: what the chat writes to the backend (a feature, a note)
// has no field an image could travel in.
func TestChatWritesCarryNoAttachment(t *testing.T) {
	for _, v := range []any{chatRegisterRequest{}, chatNoteRequest{}} {
		typ := reflect.TypeOf(v)
		for i := range typ.NumField() {
			if tag := typ.Field(i).Tag.Get("json"); strings.Contains(tag, "attach") || strings.Contains(tag, "image") {
				t.Errorf("%s.%s carries attachments to the backend (json %q)", typ.Name(), typ.Field(i).Name, tag)
			}
		}
	}
}

// F-0520 crit. 8: only the message being sent is refused for its images. After
// a switch to a provider with no image input, an older message's images are
// left out and the conversation goes on.
func TestChatTurnAfterProviderSwitchDropsOlderImages(t *testing.T) {
	h, spy, id, _, _, _ := attachTurn(t, llm.ModeGeminiCLI)
	body := `{"conversation_id":"conv-1","messages":[{"role":"user","content":"veja a tela","attachments":["` + id + `"]},{"role":"assistant","content":"vi"},{"role":"user","content":"e agora?"}]}`
	rec := postChat(h, body)
	if rec.Code != http.StatusOK || spy.calls != 1 {
		t.Fatalf("status = %d, provider calls = %d, want 200 and one call; body: %s", rec.Code, spy.calls, rec.Body.String())
	}
	if len(spy.images) != 0 || strings.Contains(spy.prompt, "[attached image") {
		t.Fatalf("a provider with no image input got images %v; prompt:\n%s", spy.images, spy.prompt)
	}
}

// F-0520 crit. 5: a long conversation hands the provider its most recent
// images only, and a message may be images with no text.
func TestChatTurnCapsImagesAcrossTheConversation(t *testing.T) {
	h, spy, first, _, _, _ := attachTurn(t, llm.ModeClaudeCLI)
	var msgs []string
	var last string
	for m := range maxChatImagesPerTurn/maxChatImagesPerMessage + 1 {
		ids := []string{first}
		if m > 0 {
			ids = ids[:0]
			for range maxChatImagesPerMessage {
				last = uploadedID(t, uploadAttachment(t, h, "conv-1", "image/png", pngBytes))
				ids = append(ids, last)
			}
		}
		raw, _ := json.Marshal(ids)
		msgs = append(msgs, `{"role":"user","content":"","attachments":`+string(raw)+`}`)
	}
	rec := postChat(h, `{"conversation_id":"conv-1","messages":[`+strings.Join(msgs, ",")+`]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(spy.images) != maxChatImagesPerTurn || !strings.HasSuffix(spy.images[len(spy.images)-1], last) {
		t.Fatalf("provider got %d images ending in %v, want the %d most recent", len(spy.images), spy.images[len(spy.images)-1:], maxChatImagesPerTurn)
	}
	if strings.Contains(strings.Join(spy.images, " ")+spy.prompt, first) {
		t.Fatalf("the oldest image must be left out of the turn")
	}
}

// F-0522 crit. 4: a byo-key turn takes the message with its attachment and
// hands the image to the provider.
func TestChatTurnByoKeyCarriesAttachedImages(t *testing.T) {
	h, spy, id, _, _, _ := attachTurn(t, "byo-key")
	rec := postChat(h, chatBody("conv-1", id))
	if rec.Code != http.StatusOK || spy.calls != 1 {
		t.Fatalf("status = %d, provider calls = %d, want 200 and one call; body: %s", rec.Code, spy.calls, rec.Body.String())
	}
	if len(spy.images) != 1 || !strings.HasSuffix(spy.images[0], id) {
		t.Fatalf("provider images = %v, want the attached one", spy.images)
	}
}

// F-0524 crit. 5: a cursor-cli turn accepts a message with an attachment and
// hands the cached path to the provider.
func TestChatTurnCursorCLICarriesAttachedImages(t *testing.T) {
	h, spy, id, cache, _, _ := attachTurn(t, llm.ModeCursorCLI)
	rec := postChat(h, chatBody("conv-1", id))
	if rec.Code != http.StatusOK || spy.calls != 1 {
		t.Fatalf("status = %d, provider calls = %d, want 200 and one call; body: %s", rec.Code, spy.calls, rec.Body.String())
	}
	path := filepath.Join(cache, "proj-attach", "chat-attachments", "conv-1", id)
	if !slices.Equal(spy.images, []string{path}) {
		t.Fatalf("provider images = %v, want [%s]", spy.images, path)
	}
}

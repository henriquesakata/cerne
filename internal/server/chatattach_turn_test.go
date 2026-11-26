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

// F-0520 crit. 8: a turn with attachments on a mode that takes no image is
// refused instead of answered blind.
func TestChatTurnRefusesImagesOnModeWithoutThem(t *testing.T) {
	for _, mode := range []string{"byo-key", llm.ModeGeminiCLI, llm.ModeCursorCLI} {
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

// F-0520 crit. 8: the remote serve never takes an attachment.
func TestChatTurnRefusesImagesRemotely(t *testing.T) {
	_, spy, id, _, repoDir, projDir := attachTurn(t, llm.ModeClaudeCLI)
	remote := markRemote(func(w http.ResponseWriter, r *http.Request) { handleChat(w, r, repoDir, projDir, "proj-attach") })
	rec := httptest.NewRecorder()
	remote(rec, localRequest(http.MethodPost, "/api/chat", strings.NewReader(chatBody("conv-1", id))))
	if rec.Code != http.StatusUnprocessableEntity || spy.calls != 0 {
		t.Fatalf("status = %d, provider calls = %d, want 422 and no call; body: %s", rec.Code, spy.calls, rec.Body.String())
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

// F-0520 crit. 8: the UI learns which reachable providers take images.
func TestLLMConfigListsImageProviders(t *testing.T) {
	for mode, want := range map[string][]string{llm.ModeClaudeCLI: {"anthropic"}, "byo-key": nil} {
		cfg := project.LLMConfig{Mode: mode}
		got := imageProviders(cfg, llm.ConfiguredProviders(cfg), false)
		if !slices.Equal(got, want) {
			t.Errorf("mode %s: image providers = %v, want %v", mode, got, want)
		}
	}
	// A CLI found on PATH beside a byo-key project takes images; the project's
	// own vendor, reached by API key, does not.
	if got := imageProviders(project.LLMConfig{Mode: "byo-key", Provider: "anthropic"}, []string{"anthropic", "openai", "gemini"}, false); !slices.Equal(got, []string{"openai"}) {
		t.Errorf("image providers = %v, want [openai]", got)
	}
	if got := imageProviders(project.LLMConfig{Mode: llm.ModeClaudeCLI}, []string{"anthropic"}, true); len(got) != 0 {
		t.Errorf("remote image providers = %v, want none", got)
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

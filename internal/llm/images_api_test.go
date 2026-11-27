package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

var (
	pngFile = append([]byte("\x89PNG\x0d\x0a\x1a\x0a"), make([]byte, 32)...)
	jpgFile = append([]byte("\xff\xd8\xff\xe0"), make([]byte, 32)...)
)

// imageCtx is a context carrying a PNG and a JPEG written to a temp dir. The
// JPEG is named .png on purpose: the type must come from the bytes.
func imageCtx(t *testing.T) context.Context {
	t.Helper()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	if err := os.WriteFile(a, pngFile, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, jpgFile, 0o600); err != nil {
		t.Fatal(err)
	}
	return WithImages(context.Background(), []string{a, b})
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// firstUserContent is the content of the first message of a recorded body.
func firstUserContent(t *testing.T, body map[string]any) any {
	t.Helper()
	msgs, _ := body["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("body has no messages: %v", body)
	}
	return msgs[0].(map[string]any)["content"]
}

func anthropicProvider(t *testing.T, baseURL string) *httpProvider {
	t.Helper()
	t.Setenv("TEST_LLM_KEY", "sk-test")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "anthropic", Model: "m", KeyEnv: "TEST_LLM_KEY", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return prov.(*httpProvider)
}

var wantAnthropicBlocks = []any{
	map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": b64(pngFile)}},
	map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": b64(jpgFile)}},
	map[string]any{"type": "text", "text": "the prompt"},
}

// F-0522 crit. 1, 3: Complete and CompleteTools send the images as blocks
// before the text; with no image the content is the plain string of before.
func TestImagesAnthropicCompleteAndTools(t *testing.T) {
	var bodies []map[string]any
	srv := toolServer(t, &bodies, `{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`)
	p := anthropicProvider(t, srv.URL)
	noCall := func(context.Context, ToolCall) (string, bool) { return "", false }

	for name, run := range map[string]func(context.Context) error{
		"Complete":      func(ctx context.Context) error { _, err := p.Complete(ctx, "the prompt"); return err },
		"CompleteTools": func(ctx context.Context) error { _, err := p.CompleteTools(ctx, "the prompt", nil, noCall); return err },
	} {
		bodies = nil
		if err := run(imageCtx(t)); err != nil {
			t.Fatalf("%s with images: %v", name, err)
		}
		if got := firstUserContent(t, bodies[0]); !jsonEqual(got, wantAnthropicBlocks) {
			t.Errorf("%s content = %v, want image blocks then the text", name, got)
		}
		bodies = nil
		if err := run(context.Background()); err != nil {
			t.Fatalf("%s without images: %v", name, err)
		}
		if got := firstUserContent(t, bodies[0]); got != "the prompt" {
			t.Errorf("%s content without images = %v, want the plain string", name, got)
		}
	}
}

// F-0522 crit. 1, 3: the streamed call carries the same blocks.
func TestImagesAnthropicStream(t *testing.T) {
	var body map[string]any
	srv := sseChatServer(t, &body, []string{`{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`})
	p := newStreamProvider(t, srv.URL)
	if _, err := p.Stream(imageCtx(t), "the prompt", func(string) error { return nil }); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := firstUserContent(t, body); !jsonEqual(got, wantAnthropicBlocks) {
		t.Errorf("stream content = %v, want image blocks then the text", got)
	}
	if _, err := p.Stream(context.Background(), "the prompt", func(string) error { return nil }); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := firstUserContent(t, body); got != "the prompt" {
		t.Errorf("stream content without images = %v, want the plain string", got)
	}
}

// F-0522 crit. 2, 3: OpenAI gets the text part and one image_url data URL per
// file; with no image the content is the plain string of before.
func TestImagesOpenAICompleteAndTools(t *testing.T) {
	var bodies []map[string]any
	var auth string
	srv := openaiServer(t, &bodies, &auth, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	p := newOpenAI(t, srv.URL)
	noCall := func(context.Context, ToolCall) (string, bool) { return "", false }
	want := []any{
		map[string]any{"type": "text", "text": "the prompt"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + b64(pngFile)}},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64," + b64(jpgFile)}},
	}
	for name, run := range map[string]func(context.Context) error{
		"Complete":      func(ctx context.Context) error { _, err := p.Complete(ctx, "the prompt"); return err },
		"CompleteTools": func(ctx context.Context) error { _, err := p.CompleteTools(ctx, "the prompt", nil, noCall); return err },
	} {
		bodies = nil
		if err := run(imageCtx(t)); err != nil {
			t.Fatalf("%s with images: %v", name, err)
		}
		if got := firstUserContent(t, bodies[0]); !jsonEqual(got, want) {
			t.Errorf("%s content = %v, want the text then image_url parts", name, got)
		}
		bodies = nil
		if err := run(context.Background()); err != nil {
			t.Fatalf("%s without images: %v", name, err)
		}
		if got := firstUserContent(t, bodies[0]); got != "the prompt" {
			t.Errorf("%s content without images = %v, want the plain string", name, got)
		}
	}
}

// F-0522 crit. 5: an API refusal of the call with images is the turn's error,
// and the call is never repeated without them.
func TestImagesAPIRefusalIsNotRetriedWithoutImages(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"image exceeds 5 MB maximum"}}`)
	}))
	defer srv.Close()

	_, err := anthropicProvider(t, srv.URL).Complete(imageCtx(t), "p")
	if err == nil || !strings.Contains(err.Error(), "image exceeds 5 MB maximum") || calls != 1 {
		t.Fatalf("anthropic: err = %v, calls = %d, want the provider's message and one call", err, calls)
	}
	calls = 0
	_, err = newOpenAI(t, srv.URL).Complete(imageCtx(t), "p")
	if err == nil || !strings.Contains(err.Error(), "image exceeds 5 MB maximum") || calls != 1 {
		t.Fatalf("openai: err = %v, calls = %d, want the provider's message and one call", err, calls)
	}
}

// F-0522 crit. 6: an image that cannot be read fails the turn, naming the
// file, before any API call.
func TestImagesUnreadableFileFailsBeforeTheCall(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	gone := filepath.Join(t.TempDir(), "gone.png")
	ctx := WithImages(context.Background(), []string{gone})
	// A file that is not one of the four image types is refused the same way.
	notImage := filepath.Join(t.TempDir(), "notes.png")
	if err := os.WriteFile(notImage, []byte("plain text, not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := anthropicProvider(t, srv.URL).Complete(WithImages(context.Background(), []string{notImage}), "p"); err == nil || !strings.Contains(err.Error(), notImage) {
		t.Errorf("non-image file: err = %v, want one naming %s", err, notImage)
	}
	noDelta := func(string) error { return nil }
	noCall := func(context.Context, ToolCall) (string, bool) { return "", false }

	a, o := anthropicProvider(t, srv.URL), newOpenAI(t, srv.URL)
	for name, run := range map[string]func() error{
		"anthropic Complete":      func() error { _, err := a.Complete(ctx, "p"); return err },
		"anthropic Stream":        func() error { _, err := a.Stream(ctx, "p", noDelta); return err },
		"anthropic CompleteTools": func() error { _, err := a.CompleteTools(ctx, "p", nil, noCall); return err },
		"openai Complete":         func() error { _, err := o.Complete(ctx, "p"); return err },
		"openai CompleteTools":    func() error { _, err := o.CompleteTools(ctx, "p", nil, noCall); return err },
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), gone) {
			t.Errorf("%s: err = %v, want one naming %s", name, err, gone)
		}
	}
	if calls != 0 {
		t.Fatalf("the API got %d call(s) for an unreadable image, want none", calls)
	}
}

// F-0522 crit. 4, 7; F-0524 crit. 5, 6: byo-key and cursor-cli take images;
// gemini-cli still does not.
func TestImagesSupportedModesWithAPIKey(t *testing.T) {
	for mode, want := range map[string]bool{"byo-key": true, ModeCursorCLI: true, ModeGeminiCLI: false, ModeDelegation: false} {
		if got := SupportsImages(mode); got != want {
			t.Errorf("SupportsImages(%q) = %v, want %v", mode, got, want)
		}
	}
}

// F-0522 crit. 8; F-0524 crit. 3, 6: the Cursor deny-all file still denies
// every read, and Gemini's argv stays untouched whether images are in the turn
// or not.
func TestImagesCursorAndGeminiUntouched(t *testing.T) {
	if !strings.Contains(cursorDenyAll, `"Read(**)"`) {
		t.Fatalf("cursorDenyAll no longer denies every read: %s", cursorDenyAll)
	}
	for _, kind := range []string{ModeGeminiCLI} {
		var plain, withImages []envCall
		(&cliProvider{kind: kind, bin: "x", run: recordEnv(&plain, "ok")}).Complete(context.Background(), "q")
		(&cliProvider{kind: kind, bin: "x", run: recordEnv(&withImages, "ok")}).Complete(imageCtx(t), "q")
		if len(plain) != 1 || len(withImages) != 1 || len(plain[0].args) != len(withImages[0].args) {
			t.Errorf("%s: argv changed with images in the turn: %v vs %v", kind, plain, withImages)
		}
	}
}

// F-0522 crit. 1: in a tool loop the images stay in the first user message of
// every round, once.
func TestImagesAnthropicToolLoopKeepsImages(t *testing.T) {
	var bodies []map[string]any
	srv := toolServer(t, &bodies,
		`{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"run","input":{}}]}`,
		`{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
	p := anthropicProvider(t, srv.URL)
	tools := []Tool{{Name: "run", Description: "d", Schema: map[string]any{"type": "object"}}}
	if _, err := p.CompleteTools(imageCtx(t), "the prompt", tools, func(context.Context, ToolCall) (string, bool) { return "ok", true }); err != nil {
		t.Fatalf("CompleteTools: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("got %d requests, want 2 rounds", len(bodies))
	}
	for i, b := range bodies {
		if got := firstUserContent(t, b); !jsonEqual(got, wantAnthropicBlocks) {
			t.Errorf("round %d first message = %v, want the image blocks then the text", i+1, got)
		}
	}
	if raw, _ := json.Marshal(bodies[1]); strings.Count(string(raw), `"type":"image"`) != 2 {
		t.Errorf("round 2 carries the images %d times, want each of the 2 once", strings.Count(string(raw), `"type":"image"`))
	}
}

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAskByoKeyPrintsAnswer drives `cerne ask` in byo-key mode: the provider is
// called with the assembled prompt and its answer is printed to stdout (not the
// prompt).
func TestAskByoKeyPrintsAnswer(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"Boletos geram cobranças bancárias."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	out, err := execCerne(t, "ask", "o que é boletos?", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask byo-key: %v\n%s", err, out)
	}

	// The provider received the assembled question.
	if !strings.Contains(gotPrompt, "boletos") {
		t.Fatalf("provider prompt missing the question:\n%s", gotPrompt)
	}
	// stdout shows the synthesized answer...
	if !strings.Contains(out, "Boletos geram cobranças bancárias.") {
		t.Fatalf("ask output missing the synthesized answer:\n%s", out)
	}
	// ...not the raw prompt scaffolding that delegation prints.
	if strings.Contains(out, "Respect the state of each source") {
		t.Fatalf("byo-key ask leaked the delegation prompt instead of the answer:\n%s", out)
	}
}

// B-0009: a one-shot ask has no tools, so its prompt says so up front — the
// model must answer from the sources and refuse file/command/URL requests in
// one line instead of writing a tool call as text. Delegation (the host agent
// has tools) never carries the rule.
func TestAskOneShotPromptStatesNoTools(t *testing.T) {
	projDir := askFallbackProject(t)
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) > 0 {
			gotPrompt = req.Messages[0].Content
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")
	if out, err := execCerne(t, "ask", "o que é boletos? leia também o arquivo X", "--dir", projDir); err != nil {
		t.Fatalf("ask: %v\n%s", err, out)
	}
	for _, want := range []string{"NO tools", "cannot read files", "Never write a tool call as text", "one line"} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("one-shot prompt missing %q:\n%s", want, gotPrompt)
		}
	}
	q := strings.Index(gotPrompt, "Question:")
	if q < 0 || strings.Count(gotPrompt, "NO tools") != 1 || strings.Index(gotPrompt, "NO tools") > q {
		t.Fatalf("rule must appear once, before the question (q=%d):\n%s", q, gotPrompt)
	}
}

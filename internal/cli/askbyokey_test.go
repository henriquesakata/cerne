package cli

import (
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
	if strings.Contains(out, "Respeite o estado de cada fonte") {
		t.Fatalf("byo-key ask leaked the delegation prompt instead of the answer:\n%s", out)
	}
}

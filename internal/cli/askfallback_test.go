package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// askFallbackProject is a project with one feature whose ask retrieves a source.
func askFallbackProject(t *testing.T) string {
	t.Helper()
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	return projDir
}

// F-0346 crit. 1: when the model fails (Complete), stdout carries the same
// prompt delegation prints, stderr one line with the cause, exit 0.
func TestAskModelFailureFallsBackToSources(t *testing.T) {
	projDir := askFallbackProject(t)
	want, _, err := execCerneSplit(t, "ask", "o que é boletos?", "--dir", projDir)
	if err != nil {
		t.Fatalf("delegation ask: %v", err)
	}
	want = strings.TrimPrefix(want, "Building index...\n") // first run builds the index
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota exceeded\nsecond line", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	stdout, stderr, err := execCerneSplit(t, "ask", "o que é boletos?", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask must exit 0 on model failure: %v\n%s", err, stderr)
	}
	if stdout != want {
		t.Fatalf("stdout differs from delegation output:\n got: %q\nwant: %q", stdout, want)
	}
	if !strings.HasPrefix(stderr, "cerne ask: the model failed (") || !strings.HasSuffix(stderr, "); here are the sources to answer from\n") {
		t.Fatalf("stderr line wrong: %q", stderr)
	}
	if strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "second line") {
		t.Fatalf("stderr must be one line with the first error line only: %q", stderr)
	}
	if !strings.Contains(stderr, "429") {
		t.Fatalf("stderr lost the cause: %q", stderr)
	}
}

// F-0346 crit. 1: NewProvider failing (key env unset) is the same fallback,
// and the 200-char cut counts runes, not bytes.
func TestAskProviderUnavailableFallsBackAndCutsByRunes(t *testing.T) {
	projDir := askFallbackProject(t)
	enableByoKey(t, projDir, "http://127.0.0.1:1", strings.Repeat("é", 300))

	stdout, stderr, err := execCerneSplit(t, "ask", "o que é boletos?", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask must exit 0 when the provider is unavailable: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "Pergunta: o que é boletos?") {
		t.Fatalf("stdout missing the prompt:\n%s", stdout)
	}
	const pre, suf = "cerne ask: the model failed (", "); here are the sources to answer from\n"
	if !strings.HasPrefix(stderr, pre) || !strings.HasSuffix(stderr, suf) {
		t.Fatalf("stderr line wrong: %q", stderr)
	}
	cause := strings.TrimSuffix(strings.TrimPrefix(stderr, pre), suf)
	if n := utf8.RuneCountInString(cause); n != 200 || !utf8.ValidString(cause) {
		t.Fatalf("cause = %d runes (valid=%v), want 200 valid runes", n, utf8.ValidString(cause))
	}
}

// F-0346 crit. 5: --no-preamble is honored in the fallback text.
func TestAskModelFailureNoPreamble(t *testing.T) {
	projDir := askFallbackProject(t)
	enableByoKey(t, projDir, "http://127.0.0.1:1", "TEST_UNSET_KEY_F0346")
	stdout, _, err := execCerneSplit(t, "ask", "o que é boletos?", "--no-preamble", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if !strings.Contains(stdout, "Pergunta: o que é boletos?") || strings.Contains(stdout, "Respeite o estado") {
		t.Fatalf("fallback ignored --no-preamble:\n%s", stdout)
	}
}

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const titlePairBody = `{"messages":[{"role":"user","content":"Oi"},{"role":"assistant","content":"Olá"}]}`

type chatTitleGot struct {
	Generated     bool   `json:"generated"`
	NeedsProvider bool   `json:"needs_provider"`
	Title         string `json:"title"`
	Error         string `json:"error"`
}

// TestChatTitleRouteDeclaredLocalOnly (F-0310 criterion 15): the title route
// lives in the single route table, and only on the local ui — it spends the
// host's LLM, like /wiki/compile.
func TestChatTitleRouteDeclaredLocalOnly(t *testing.T) {
	for _, r := range dataRoutes() {
		if r.method == http.MethodPost && r.pattern == "/chat/title" {
			if r.remote {
				t.Fatal("/chat/title is remote, want local-only")
			}
			return
		}
	}
	t.Fatal("POST /chat/title is not declared in dataRoutes()")
}

// TestChatTitleDelegation (F-0310 criterion 12): with no provider the route
// degrades silently — 200, nothing generated, no title, no error.
func TestChatTitleDelegation(t *testing.T) {
	repoDir, projDir := newBackend(t) // no .cernerc → delegation
	var got chatTitleGot
	rec := doJSON(t, New(repoDir, projDir, "proj-title"), http.MethodPost, "/api/chat/title", titlePairBody, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Generated || !got.NeedsProvider || got.Title != "" {
		t.Fatalf("got %+v, want generated=false needs_provider=true and no title", got)
	}
}

// TestChatTitleByoKey (F-0310 criterion 9, server side): the one-shot answer is
// cleaned into a title; the prompt carries the exchange, the 40-char rule, and
// each message capped at titleInputRunes.
func TestChatTitleByoKey(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var gotPrompt string
	srv := fakeDiagramProvider(t, &gotPrompt, `\"Exportar relatórios em PDF.\"`)
	configureByoKey(t, projDir, srv.URL)

	long := strings.Repeat("z", 5000)
	body := `{"messages":[{"role":"user","content":"Quero exportar relatórios ` + long + `"},{"role":"assistant","content":"Certo."}]}`
	var got chatTitleGot
	rec := doJSON(t, New(repoDir, projDir, "proj-title"), http.MethodPost, "/api/chat/title", body, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !got.Generated || got.Title != "Exportar relatórios em PDF" {
		t.Fatalf("got %+v, want generated title %q", got, "Exportar relatórios em PDF")
	}
	if !strings.Contains(gotPrompt, "Quero exportar relatórios") {
		t.Fatalf("prompt lacks the user text: %s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "no máximo 40 caracteres") {
		t.Fatalf("prompt lacks the 40-char rule: %s", gotPrompt)
	}
	if strings.Contains(gotPrompt, strings.Repeat("z", titleInputRunes)) {
		t.Fatalf("prompt kept %d z's of the user message, want it cut to %d runes total", titleInputRunes, titleInputRunes)
	}
	if !strings.Contains(gotPrompt, strings.Repeat("z", titleInputRunes-len([]rune("Quero exportar relatórios ")))) {
		t.Fatal("prompt lost the head of the long message")
	}
}

// TestChatTitleEmptyReply (F-0310 criterion 13): a blank answer is "not
// generated", never an empty title.
func TestChatTitleEmptyReply(t *testing.T) {
	repoDir, projDir := newBackend(t)
	srv := fakeDiagramProvider(t, nil, `  \n `)
	configureByoKey(t, projDir, srv.URL)
	var got chatTitleGot
	rec := doJSON(t, New(repoDir, projDir, "proj-title"), http.MethodPost, "/api/chat/title", titlePairBody, &got)
	if rec.Code != http.StatusOK || got.Generated || got.Title != "" {
		t.Fatalf("status=%d got %+v, want 200 and generated=false", rec.Code, got)
	}
}

// TestChatTitleProviderFailure (F-0310 criterion 13): a dead provider is a 502
// the client swallows, keeping the truncated title.
func TestChatTitleProviderFailure(t *testing.T) {
	repoDir, projDir := newBackend(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	configureByoKey(t, projDir, srv.URL)
	var got chatTitleGot
	rec := doJSON(t, New(repoDir, projDir, "proj-title"), http.MethodPost, "/api/chat/title", titlePairBody, &got)
	if rec.Code != http.StatusBadGateway || got.Error == "" {
		t.Fatalf("status=%d got %+v, want 502 with an error", rec.Code, got)
	}
}

// TestChatTitleTimeout (F-0310 criterion 13): a hung provider is cut by
// chatTitleTimeout, not by the chat's long agent budget.
func TestChatTitleTimeout(t *testing.T) {
	old := chatTitleTimeout
	chatTitleTimeout = 50 * time.Millisecond
	t.Cleanup(func() { chatTitleTimeout = old })

	repoDir, projDir := newBackend(t)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Draining the body lets the server notice the client hanging up;
		// the bounded wait keeps a regression from wedging srv.Close.
		_, _ = io.ReadAll(r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	configureByoKey(t, projDir, srv.URL)
	start := time.Now()
	var got chatTitleGot
	rec := doJSON(t, New(repoDir, projDir, "proj-title"), http.MethodPost, "/api/chat/title", titlePairBody, &got)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body: %s", rec.Code, rec.Body.String())
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v, want well under 1s", d)
	}
}

// TestChatTitleRefusesCrossOrigin: a cross-site POST cannot burn the host's
// tokens — refused before the provider is reached.
func TestChatTitleRefusesCrossOrigin(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"X"}]}`)
	}))
	t.Cleanup(srv.Close)
	configureByoKey(t, projDir, srv.URL)
	h := New(repoDir, projDir, "proj-title")
	for _, hdr := range []map[string]string{
		{"Origin": "https://evil.example", "Content-Type": "application/json"},
		{"Content-Type": "text/plain;charset=UTF-8"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/chat/title", strings.NewReader(titlePairBody))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%v = %d, want 403", hdr, rec.Code)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("provider called %d times, want 0", n)
	}
}

// TestChatTitleBadInput: no exchange is 422, a malformed body 400.
func TestChatTitleBadInput(t *testing.T) {
	repoDir, projDir := newBackend(t)
	h := New(repoDir, projDir, "proj-title")
	var got chatTitleGot
	if rec := doJSON(t, h, http.MethodPost, "/api/chat/title", `{"messages":[]}`, &got); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty messages = %d, want 422", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/chat/title", `{not json`, &got); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", rec.Code)
	}
}

func TestCleanTitle(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"X"`, "X"},
		{"# X.\n\nexplicação", "X"},
		{"`X`", "X"},
		{"\n\n  X  \n", "X"},
		{" \n ", ""},
		{"**Exportar PDF**", "Exportar PDF"},
		// C5: model preamble, label prefixes, list markers, markdown residue and
		// guillemets are stripped; a meaningful leading "#" (an issue ref) stays.
		{"Claro! Aqui está o título:\n\nExportar PDF", "Exportar PDF"},
		{"Here's a title:\nExport PDF", "Export PDF"},
		{"Título: Exportar relatórios", "Exportar relatórios"},
		{"**Título:** Exportar PDF", "Exportar PDF"},
		{`Title: "Export PDF"`, "Export PDF"},
		{"- Exportar PDF", "Exportar PDF"},
		{"1. Exportar PDF", "Exportar PDF"},
		{"* Exportar PDF", "Exportar PDF"},
		{"_Exportar PDF_", "Exportar PDF"},
		{"«Exportar PDF»", "Exportar PDF"},
		{"#123 corrigir bug", "#123 corrigir bug"},
		{"Renomear snake_case_fn", "Renomear snake_case_fn"},
		{"Título:", ""},
	} {
		if got := cleanTitle(tc.in); got != tc.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

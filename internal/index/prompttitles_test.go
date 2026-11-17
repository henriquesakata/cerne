package index

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestPromptTitles (B-0020): precision over recall. Realistic prompts that
// must surface a note/runbook by its title, and ones that must stay silent.
func TestPromptTitles(t *testing.T) {
	repoDir := t.TempDir()
	seedNote(t, repoDir, "N-0001", "Webhook do cliente retorna timeout após deploy", "fix: raise the proxy timeout", domain.NoteActive)
	seedNote(t, repoDir, "N-0002", "Certificado SSL expirado no painel de pagamentos", "renew", domain.NoteActive)
	seedNote(t, repoDir, "N-0003", "Webhook antigo com timeout", "obsolete", domain.NoteSuperseded)
	seedNote(t, repoDir, "N-0004", "Deploy", "single-term title is never matched", domain.NoteActive)
	seedNote(t, repoDir, "N-0005", "Customer export fails with encoding error", "use utf-8", domain.NoteActive)
	now := time.Now().UTC()
	if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Rotacionar certificado SSL do painel", State: domain.RunbookActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0002", Title: "Rotacionar certificado SSL legado", State: domain.RunbookSuperseded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "content", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, prompt string
		want         []string
	}{
		{"symptom with title words", "o webhook do cliente está dando timeout depois do deploy, o que faço?", []string{"N-0001"}},
		{"accents and case", "WEBHOOK cliente: TIMEOUT após o deploy", []string{"N-0001"}},
		{"english title", "the customer export keeps failing with an encoding error", []string{"N-0005"}},
		{"note and runbook, never the superseded", "o certificado SSL do painel expirou", []string{"N-0002", "RB-0001"}},
		{"one shared word is not enough", "preciso fazer o deploy de uma versão nova", nil},
		{"unrelated", "como configuro o editor para formatar o código ao salvar?", nil},
		{"only stop words", "o que é isso para os que como", nil},
		{"superseded title alone", "webhook antigo com timeout", nil},
		{"empty", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := PromptTitles(context.Background(), db, tc.prompt, 3)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, h := range hits {
				got = append(got, h.ID)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("hits = %v, want %v", got, tc.want)
			}
		})
	}

	if hits, err := PromptTitles(context.Background(), filepath.Join(t.TempDir(), "none.db"), "webhook cliente timeout", 3); hits != nil || err != nil {
		t.Fatalf("missing index: %v %v", hits, err)
	}
}

// TestPromptTitlesLongPaste (B-0020, critic 2): a pasted log or code block
// holds many words, so titles must not match by chance; the real question at
// the top of a long prompt still does.
func TestPromptTitlesLongPaste(t *testing.T) {
	repoDir := t.TempDir()
	seedNote(t, repoDir, "N-0001", "Webhook cliente timeout", "b", domain.NoteActive)
	seedNote(t, repoDir, "N-0002", "Erro de conexão com banco", "b", domain.NoteActive)
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	log := ""
	for i := 0; i < 40; i++ {
		log += "2026-11-17 12:00:0" + string(rune('0'+i%10)) + " INFO worker pool handler request id=abc dispatch queue size=10 webhook retry cliente latency ok\n"
	}
	log += "ERROR timeout while waiting\n"
	code := "func handle(w http.ResponseWriter, r *http.Request) {\n\t// webhook handler for the cliente\n\tctx, cancel := context.WithTimeout(r.Context(), time.Second)\n\tdefer cancel()\n\tconn := banco.Conexao(ctx)\n\tfmt.Println(\"err\", conn)\n}\n"
	for _, tc := range []struct {
		name, prompt string
		want         []string
	}{
		{"pasted log", "segue o log, o que acha?\n" + log, nil},
		{"pasted code block", "revisa este código\n```go\n" + code + strings.Repeat("// helper comentario para preencher linhas do bloco\n", 20) + "```", nil},
		{"question first, log after", "o webhook do cliente está com timeout, veja o log\n" + strings.Repeat("x ", 10) + log, []string{"N-0001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := PromptTitles(context.Background(), db, tc.prompt, 3)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, h := range hits {
				got = append(got, h.ID)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("hits = %v, want %v", got, tc.want)
			}
		})
	}
}

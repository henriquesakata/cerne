package index

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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

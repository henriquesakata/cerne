package index

import (
	"context"
	"fmt"
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
		// The superseded N-0003 never; the active note on the same subject,
		// webhook and timeout, does (B-0026: 2 rare shared terms suffice).
		{"superseded title: only the active note on its subject", "webhook antigo com timeout", []string{"N-0001"}},
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

// TestPromptTitlesNaturalPrompts (B-0026): a natural prompt names 2 to 4 terms
// of a descriptive title, never half of it. The table is the bug report's
// reproduction: each prompt must surface its note within the 3 lines.
func TestPromptTitlesNaturalPrompts(t *testing.T) {
	repoDir := t.TempDir()
	for id, title := range map[string]string{
		"N-0001": "Google OAuth callback must stay on the apex domain",
		"N-0002": "Driver do banco local extrai a biblioteca nativa para a pasta temporária e deixa arquivos órfãos após encerramento abrupto",
		"N-0003": "Menu raiz do cliente Alfa: empate de prioridade resolvido com um UPDATE de uma linha; desempate por id",
		"N-0004": "Servidor roda em processo de 32 bits: aumentar a memória máxima quebra a subida por falta de espaço de endereçamento",
		"N-0005": "Limite de 3 tentativas ignorado depois do upgrade: o id fixo do botão ganhou o prefixo do contêiner",
	} {
		seedNote(t, repoDir, id, title, "corpo", domain.NoteActive)
	}
	// A real-sized corpus where "depois", s4's second word, is common.
	for i := range 16 {
		title := fmt.Sprintf("Assunto avulso %03d", i)
		if i < 4 {
			title += " depois do deploy"
		}
		seedNote(t, repoDir, fmt.Sprintf("N-%04d", 100+i), title, "corpo", domain.NoteActive)
	}
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	log := strings.Repeat("2026-11-17 12:00:01 INFO worker pool handler request id=abc dispatch queue size=10 latency ok\n", 10)
	for _, tc := range []struct {
		name, prompt, want string // want "" = nothing injected
	}{
		{"s1", "o driver ainda gera aquela biblioteca na pasta temporária?", "N-0002"},
		{"s2", "o menu do cliente Alfa voltou a sair fora de ordem", "N-0003"},
		{"s3", "aumentei a memória máxima e o servidor não sobe mais", "N-0004"},
		{"s4", "a tela de tentativas não bloqueia depois de 3 erros", "N-0005"},
		{"s5", "can I move the OAuth callback to the www subdomain?", "N-0001"},
		{"s6", "pode revisar meu texto de apresentação?", ""},
		{"literal title", "Driver do banco local extrai a biblioteca nativa para a pasta temporária e deixa arquivos órfãos após encerramento abrupto", "N-0002"},
		{"question first, long log after", "o menu do cliente Alfa voltou a sair fora de ordem, segue o log\n" + log, "N-0003"},
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
			if tc.want == "" && got != nil || tc.want != "" && !slices.Contains(got, tc.want) {
				t.Fatalf("hits = %v, want %q", got, tc.want)
			}
		})
	}
}

// TestPromptTitlesCommonWords (B-0026): two words a third of the titles share
// never fire alone, however many titles hold both; one rare word with them
// does.
func TestPromptTitlesCommonWords(t *testing.T) {
	repoDir := t.TempDir()
	for i := range 24 {
		title := fmt.Sprintf("Assunto avulso %03d", i)
		if i < 8 {
			title = fmt.Sprintf("Agente do cerne e o assunto %03d", i)
		}
		seedNote(t, repoDir, fmt.Sprintf("N-%04d", i+1), title, "b", domain.NoteActive)
	}
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	for prompt, want := range map[string][]string{
		"como o agente usa o cerne?":      nil,
		"o agente do cerne travou no 003": {"N-0004"},
	} {
		hits, err := PromptTitles(context.Background(), db, prompt, 3)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, h := range hits {
			got = append(got, h.ID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q: hits = %v, want %v", prompt, got, want)
		}
	}
}

// TestPromptTitlesCamelCase (B-0028): a title naming a class holds the
// class's parts too, so a prompt naming the parts fires it; the whole
// identifier still does, and a paste still needs (almost) the whole title.
func TestPromptTitlesCamelCase(t *testing.T) {
	repoDir := t.TempDir()
	seedNote(t, repoDir, "N-0001", "JsonDateSerializer próprio: porte para a versão 3 incompleto", "corpo", domain.NoteActive)
	seedNote(t, repoDir, "N-0002", "InvoiceExportJob trava na virada do mês", "corpo", domain.NoteActive)
	seedNote(t, repoDir, "N-0003", "getData vazio no dragover do kanban", "corpo", domain.NoteActive)
	// A real-sized corpus where "job", a part of N-0002's identifier, is common.
	for i := range 20 {
		title := fmt.Sprintf("Assunto avulso %03d", i)
		if i < 8 {
			title = fmt.Sprintf("Rotina avulsa %03d do job noturno", i)
		}
		seedNote(t, repoDir, fmt.Sprintf("N-%04d", 100+i), title, "corpo", domain.NoteActive)
	}
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	logLine := "2026-11-17 12:00:01 INFO worker pool handler request id=abc dispatch queue size=10 latency ok\n"
	for _, tc := range []struct {
		name, prompt, want string // want "" = nothing injected
	}{
		{"d1 parts in prose", "o serializer de datas está incompleto?", "N-0001"},
		{"d2 parts in order", "o date serializer ficou incompleto?", "N-0001"},
		{"d3 words beside the identifier", "o export job das faturas trava no fim do mês?", "N-0002"},
		{"d4 parts only", "o job de export trava?", "N-0002"},
		{"d5 literal title", "JsonDateSerializer próprio: porte para a versão 3 incompleto", "N-0001"},
		{"whole identifier", "o JsonDateSerializer quebrou na versão nova", "N-0001"},
		{"one part each, common words beside", "o JsonParser do job noturno", ""},
		{"accessor prefix is no subject", "o getUserName retorna vazio", ""},
		{"paste holding the whole title", "segue o log\n" + logLine + "InvoiceExportJob trava na virada do mês\n", "N-0002"},
		{"unrelated", "pode revisar meu texto de apresentação?", ""},
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
			if tc.want == "" && got != nil || tc.want != "" && !slices.Equal(got, []string{tc.want}) {
				t.Fatalf("hits = %v, want %q", got, tc.want)
			}
		})
	}
}

package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestPushRecordsDocAuthor (F-0375 crit. 1): the push records one
// feature.doc_edited per feature document it sends — in the same commit as
// the documents, with the writer and no rev (not an amend).
func TestPushRecordsDocAuthor(t *testing.T) {
	projDir, origin, _, specPath := pulledProject(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_SESSION", "")
	t.Setenv("CERNE_PROVIDER", "openai")
	t.Setenv("CERNE_MODEL", "gpt-x")
	mustWrite(t, specPath, "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nDividir compras em parcelas, editado.\n")
	mustWrite(t, filepath.Join(filepath.Dir(specPath), "plan.md"), "---\ncerne_id: F-0001\n---\n\n# Plano\n\nPassos.\n")
	start := time.Now().UTC()
	if out, err := execCerne(t, "push", "--dir", projDir); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatal(err)
	}
	evs, err := store.ReadEvents(verify)
	if err != nil {
		t.Fatal(err)
	}
	var docs []string
	for _, ev := range evs {
		if ev.EntityID != "F-0001" || ev.Type != "feature.doc_edited" || ev.At.Before(start) {
			continue
		}
		p := ev.Payload
		if p["provider"] != "openai" || p["model"] != "gpt-x" || p["source"] != "declared" || p["rev"] != "" {
			t.Fatalf("push event payload = %v, want declared openai/gpt-x without rev", p)
		}
		docs = append(docs, p["doc"])
	}
	slices.Sort(docs)
	if !slices.Equal(docs, []string{"plan", "spec"}) {
		t.Fatalf("doc_edited docs = %v, want one per pushed document", docs)
	}
	files, err := exec.Command("git", "-C", verify, "show", "--name-only", "--format=", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	got := string(files)
	for _, want := range []string{"content/features/F-0001/spec.md", "content/features/F-0001/plan.md", "events/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("push commit files = %q, missing %s (events go in the same commit)", got, want)
		}
	}
}

// TestDraftsRecordLLMConfig (F-0375 crit. 6): a spec, plan or result drafted
// by cerne's own [llm] call is recorded as that config's provider and model.
func TestDraftsRecordLLMConfig(t *testing.T) {
	bodies := map[string]string{
		"spec":   `## Problema\n\nFalta gerar boletos. Hoje a cobrança sai por planilha e o time reconcilia à mão, o que já custou dois fechamentos atrasados.\n\n## Objetivo e critérios de sucesso\n\nGerar boletos válidos.\n\n1. QUANDO um pedido é aprovado O SISTEMA DEVE emitir o boleto correspondente.`,
		"plan":   `## Estratégia\n\nMR único, porque a mudança cabe em um pacote e não altera o schema do backend.\n\n## Sequência de tarefas\n\nTarefa 1: gerar boleto. Teste primeiro em internal/finance, depois a implementação mínima que o faz passar.`,
		"result": `## Resultado\n\nEntregue. A emissão de boletos passou a rodar no fechamento, sem planilha intermediária.\n\n## Aceitação\n\n1. ATENDIDO — TestX prova a emissão para um pedido aprovado, com o boleto gravado no fechamento.`,
	}
	for _, doc := range []string{"spec", "plan", "result"} {
		t.Run(doc, func(t *testing.T) {
			projDir, _, repoDir := setupProject(t, "finance")
			if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
				t.Fatalf("feature new: %v\n%s", err, out)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"`+bodies[doc]+`"}]}`)
			}))
			defer srv.Close()
			t.Setenv("TEST_BYO_KEY", "sk-byo")
			enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")
			cfg, dir, err := project.LoadConfig(projDir)
			if err != nil {
				t.Fatal(err)
			}
			cfg.LLM.Model = "claude-draft-model"
			if err := project.SaveConfig(dir, cfg); err != nil {
				t.Fatal(err)
			}
			if out, err := execCerne(t, doc, "draft", "F-0001", "--dir", projDir); err != nil {
				t.Fatalf("%s draft: %v\n%s", doc, err, out)
			}
			evs, err := store.ReadEvents(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			last := evs[len(evs)-1]
			p := last.Payload
			if last.Type != "feature.doc_edited" || p["doc"] != doc || p["source"] != "llm-config" || p["provider"] != "anthropic" || p["model"] != "claude-draft-model" {
				t.Fatalf("last event = %s %v, want feature.doc_edited{doc:%s, llm-config anthropic/claude-draft-model}", last.Type, p, doc)
			}
		})
	}
}

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
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

// TestShowDocWrites (F-0375 crit. 9, 11): show prints, per document, who wrote
// it and when — the first writer and, when written again, the last edit — and
// --json carries every recorded write. An event from before F-0375 (no source)
// is no writer: its document gets no line, and nothing is invented for it.
func TestShowDocWrites(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDocFeature(t, repoDir, "F-0001", domain.StateInDevelopment, realDoc("F-0001", "Spec."), realDoc("F-0001", "Plano."))
	day := func(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.UTC) }
	// 23:00 at UTC-3 on the 1st is the 2nd in UTC: dates print in UTC.
	brt := time.Date(2026, 11, 1, 23, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	for _, ev := range []domain.Event{
		{ID: "w0", At: day(1), EntityID: "F-0001", Type: "feature.doc_edited", Payload: map[string]string{"doc": "result", "state": "candidate"}},
		{ID: "w1", At: brt, EntityID: "F-0001", Type: "feature.doc_edited", Payload: map[string]string{"doc": "spec", "state": "candidate", "provider": "anthropic", "model": "claude-opus-5-5", "source": "transcript"}},
		{ID: "w2", At: day(3), EntityID: "F-0001", Type: "feature.doc_edited", Payload: map[string]string{"doc": "plan", "state": "approved", "provider": "", "model": "", "source": "human"}},
		{ID: "w3", At: day(4), EntityID: "F-0001", Type: "feature.doc_edited", Payload: map[string]string{"doc": "spec", "state": "approved", "provider": "openai", "model": "gpt-x", "source": "declared"}},
		// the same writer again: no "last edit" (crit. 8: more than one model)
		{ID: "w4", At: day(6), EntityID: "F-0001", Type: "feature.doc_edited", Payload: map[string]string{"doc": "plan", "state": "in_development", "provider": "", "model": "", "source": "human"}},
		{ID: "x1", At: day(5), EntityID: "F-0002", Type: "feature.doc_edited", Payload: map[string]string{"doc": "spec", "source": "unknown"}},
	} {
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)

	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	for _, want := range []string{
		"spec: written by anthropic/claude-opus-5-5 on 2026-11-02; last edit openai/gpt-x on 2026-11-04\n",
		"plan: written by human on 2026-11-03\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("show missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "result: written by") {
		t.Fatalf("a pre-F-0375 event gave the result a writer:\n%s", out)
	}

	js, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Writes []struct {
			Doc, Provider, Model, Source string
			At                           time.Time
		} `json:"writes"`
	}
	if err := json.Unmarshal([]byte(js), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
	var rows []string
	for _, w := range got.Writes {
		rows = append(rows, w.Doc+" "+w.Provider+"/"+w.Model+" "+w.Source+" "+w.At.Format(time.RFC3339))
	}
	want := []string{"spec anthropic/claude-opus-5-5 transcript 2026-11-02T02:00:00Z", "plan / human 2026-11-03T12:00:00Z",
		"spec openai/gpt-x declared 2026-11-04T12:00:00Z", "plan / human 2026-11-06T12:00:00Z"}
	if !slices.Equal(rows, want) {
		t.Fatalf("writes = %q, want %q", rows, want)
	}
	if !strings.Contains(js, `"provider": ""`) || !strings.Contains(js, `"model": ""`) {
		t.Fatalf("a write keeps provider and model keys even when empty:\n%s", js)
	}
}

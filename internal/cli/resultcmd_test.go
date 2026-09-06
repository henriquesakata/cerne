package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/restmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestBuildResultDraftPrompt asserts the assembled prompt carries the feature's
// id/title/module, embeds the spec and plan as context, requests every canonical
// result section in order, and teaches the result authoring rules.
func TestBuildResultDraftPrompt(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateInDevelopment}
	spec := "## Objetivo e critérios de aceitação\n\n1. QUANDO o extrato chega O SISTEMA DEVE conciliar."
	plan := "## Definição de pronto\n\nsuíte verde; critério 1 com teste."
	prompt := BuildResultDraftPrompt(f, spec, plan, "# Conciliação bancária\n\n> Resultado pendente")

	for _, want := range []string{"F-0007", "Conciliação bancária", "finance"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// The spec (criteria) and plan (done) are fed in as context to verify against.
	if !strings.Contains(prompt, "O SISTEMA DEVE conciliar") || !strings.Contains(prompt, "Definição de pronto") {
		t.Fatalf("prompt should embed the spec and plan as context:\n%s", prompt)
	}
	// All canonical result sections must be requested, in restmpl order.
	for _, want := range restmpl.Sections {
		if !strings.Contains(prompt, "## "+want) {
			t.Fatalf("prompt missing required section %q:\n%s", want, prompt)
		}
	}
	// Teaches the shared ambiguity marker and the result rules: evidence, binary
	// 1:1 acceptance.
	for _, want := range []string{spectmpl.AmbiguityMarker, "EVIDÊNCIA", "ATENDIDO/PENDENTE", "1:1"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing authoring guidance %q:\n%s", want, prompt)
		}
	}
	// F-0279: the drafting agent stamps which model and effort implemented the
	// feature, and writes "desconhecido" instead of guessing.
	for _, want := range []string{"**Agente:**", "·", "esforço", "humano", "desconhecido"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing the agent stamp rule %q:\n%s", want, prompt)
		}
	}
}

// TestResultDraftByoKeyWritesAndCommits drives `cerne result draft` in byo-key
// mode: the provider's body is written to the backend result.md with
// cerne-stamped cerne_id frontmatter and origin advances.
func TestResultDraftByoKeyWritesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		// Long enough to clear app.ValidateDraftBody's floor (F-0290).
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"## Resultado\n\nEntregue. A emissão de boletos passou a rodar no fechamento, sem planilha intermediária.\n\n## Aceitação\n\n1. ATENDIDO — TestX prova a emissão para um pedido aprovado, com o boleto gravado no fechamento."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "result", "draft", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("result draft byo-key: %v\n%s", err, out)
	}

	if !strings.Contains(gotPrompt, "Boletos") || !strings.Contains(gotPrompt, "F-0001") {
		t.Fatalf("provider prompt missing the feature id/title:\n%s", gotPrompt)
	}

	res, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "result.md"))
	if err != nil {
		t.Fatalf("read drafted result: %v", err)
	}
	if !strings.Contains(string(res), "Entregue.") {
		t.Fatalf("result missing the synthesized body:\n%s", res)
	}
	if !strings.HasPrefix(string(res), "---\ncerne_id: F-0001\n---\n") {
		t.Fatalf("result missing cerne_id frontmatter stamped by cerne:\n%s", res)
	}

	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after result draft")
	}
	if !strings.Contains(out, "drafted and pushed") {
		t.Fatalf("result draft output missing confirmation:\n%s", out)
	}
}

// TestResultDraftUnknownFeature errors when the id does not exist in the backend.
func TestResultDraftUnknownFeature(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, "http://127.0.0.1:0", "TEST_BYO_KEY")

	_, err := execCerne(t, "result", "draft", "F-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("result draft on unknown feature succeeded, want error")
	}
	if !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("error %q does not name the missing feature", err)
	}
}

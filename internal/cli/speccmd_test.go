package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestBuildSpecDraftPrompt asserts the assembled prompt carries the feature's
// id, title and module so the model has the context it needs.
func TestBuildSpecDraftPrompt(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateCandidate}
	prompt := BuildSpecDraftPrompt(f, "# Conciliação bancária\n\n> Spec pendente")
	for _, want := range []string{"F-0007", "Conciliação bancária", "finance"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// All canonical template sections must be requested, in spectmpl order.
	for _, want := range spectmpl.Sections {
		if !strings.Contains(prompt, "## "+want) {
			t.Fatalf("prompt missing required section %q:\n%s", want, prompt)
		}
	}
	// The prompt must teach the ambiguity marker and the EARS phrasing.
	for _, want := range []string{spectmpl.AmbiguityMarker, "O SISTEMA DEVE"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing authoring guidance %q:\n%s", want, prompt)
		}
	}
	// F-0194: the prompt asks for the suggested capability level, derived from
	// spectmpl.Levels (no duplicated literal), and forbids vendor model names.
	for _, want := range []string{
		"**Nível:**",
		"<" + strings.Join(spectmpl.Levels, "|") + ">",
		"não cite nomes de modelos",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing capability-level guidance %q:\n%s", want, prompt)
		}
	}
	// F-0207 criterion 3: the prompt asks for the agent-execution section too,
	// right after Verificação (mirrors spectmpl.Body's placement).
	if i, j := strings.Index(prompt, "## Verificação"), strings.Index(prompt, "## Execução recomendada (agente)"); i < 0 || j < 0 || j < i {
		t.Fatalf("prompt missing '## Execução recomendada (agente)' after Verificação:\n%s", prompt)
	}
	if !strings.Contains(prompt, "mesmo agente") {
		t.Fatalf("prompt missing same-agent-grouping guidance:\n%s", prompt)
	}
}

// TestBuildSpecDraftPromptIncludesGauntletContract asserts the spec draft
// prompt carries the Gauntlet protocol block (F-0270's marker + the three
// literal Verificação forms), so a spec authored via CLI gets the same
// quality-bar contract as one elaborated through the Backlog chat (F-0273).
func TestBuildSpecDraftPromptIncludesGauntletContract(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateCandidate}
	prompt := BuildSpecDraftPrompt(f, "")
	for _, want := range []string{
		app.ChatGauntletMarker,
		"Barra:",
		"Método:",
		"Saída:",
		"não aplicável",
		"dispensada por decisão humana",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing Gauntlet contract piece %q:\n%s", want, prompt)
		}
	}
}

// TestSpecDraftPromptGauntletSharesSourceWithChat pins the single-source
// requirement (F-0273 criterion 2): the Gauntlet block embedded in the spec
// draft prompt must be byte-identical to app.GauntletContract — the same
// constant the Backlog chat prompt uses — never a parallel copy that could
// drift silently.
func TestSpecDraftPromptGauntletSharesSourceWithChat(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateCandidate}
	prompt := BuildSpecDraftPrompt(f, "")
	if !strings.Contains(prompt, app.GauntletContract) {
		t.Fatalf("prompt does not contain app.GauntletContract verbatim:\n%s", prompt)
	}
}

// TestSpecDraftByoKeyWritesAndCommits drives `cerne spec draft` in byo-key mode:
// the provider's body is written to the backend spec.md with cerne-stamped
// cerne_id frontmatter, origin advances and the lock snapshot is updated.
func TestSpecDraftByoKeyWritesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		// Long enough to clear app.ValidateDraftBody's floor (F-0290): a body
		// the size of an agent preamble is refused, not stamped as the spec.
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"## Problema\n\nFalta gerar boletos. Hoje a cobrança sai por planilha e o time reconcilia à mão, o que já custou dois fechamentos atrasados.\n\n## Objetivo e critérios de sucesso\n\nGerar boletos válidos.\n\n1. QUANDO um pedido é aprovado O SISTEMA DEVE emitir o boleto correspondente."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	lockBefore, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock before: %v", err)
	}
	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "spec", "draft", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("spec draft byo-key: %v\n%s", err, out)
	}

	// The provider received a prompt mentioning the feature.
	if !strings.Contains(gotPrompt, "Boletos") || !strings.Contains(gotPrompt, "F-0001") {
		t.Fatalf("provider prompt missing the feature id/title:\n%s", gotPrompt)
	}

	// The spec was written to the backend clone with the synthesized body...
	specPath := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read drafted spec: %v", err)
	}
	if !strings.Contains(string(spec), "Falta gerar boletos.") {
		t.Fatalf("spec missing the synthesized body:\n%s", spec)
	}
	// ...and cerne-stamped cerne_id frontmatter (not trusted from the model).
	if !strings.HasPrefix(string(spec), "---\ncerne_id: F-0001\n---\n") {
		t.Fatalf("spec missing cerne_id frontmatter stamped by cerne:\n%s", spec)
	}

	// Origin advanced (push happened).
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after spec draft")
	}
	// Lock snapshot updated to the new origin HEAD.
	lockAfter, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock after: %v", err)
	}
	if lockAfter.Snapshot == lockBefore.Snapshot || lockAfter.Snapshot != headAfter {
		t.Fatalf("lock.Snapshot = %s, want new origin HEAD %s", lockAfter.Snapshot, headAfter)
	}

	// pt-BR confirmation.
	if !strings.Contains(out, "drafted and pushed") {
		t.Fatalf("spec draft output missing confirmation:\n%s", out)
	}
}

// TestSpecDraftEchoedFrontmatterNotDuplicated (F-0328 critic C8): the draft
// prompt carries the current spec raw, frontmatter included; a fake provider
// that echoes that block back gets one block stored, not two.
func TestSpecDraftEchoedFrontmatterNotDuplicated(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"---\ncerne_id: F-0001\n---\n\n## Problema\n\nFalta gerar boletos. Hoje a cobrança sai por planilha e o time reconcilia à mão, o que já custou dois fechamentos atrasados.\n\n## Objetivo e critérios de sucesso\n\nGerar boletos válidos."}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	if out, err := execCerne(t, "spec", "draft", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("spec draft: %v\n%s", err, out)
	}
	spec, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if strings.Count(string(spec), "cerne_id:") != 1 || !strings.HasPrefix(string(spec), "---\ncerne_id: F-0001\n---\n\n## Problema") {
		t.Fatalf("drafted spec = %q, want one cerne_id block", spec)
	}
}

// TestSpecDraftUnknownFeature errors when the id does not exist in the backend.
func TestSpecDraftUnknownFeature(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, "http://127.0.0.1:0", "TEST_BYO_KEY")

	_, err := execCerne(t, "spec", "draft", "F-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("spec draft on unknown feature succeeded, want error")
	}
	if !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("error %q does not name the missing feature", err)
	}
}

// TestBuildSpecDraftPromptMetadataRules pins F-0280 criterion 4: the draft
// prompt asks the model for the SAME three METADADOS rules the template teaches
// — no **Status:** line, an Origem: in the shape of the shared example, and the
// unknown-field escape hatch instead of a guess. The example and the marker come
// from spectmpl, never restated here, so template and prompt cannot drift (the
// drift between the two was the critic's finding in F-0279).
func TestBuildSpecDraftPromptMetadataRules(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateCandidate}
	prompt := BuildSpecDraftPrompt(f, "")
	if strings.Contains(prompt, "**Status:**") {
		t.Fatalf("prompt must not ask for a **Status:** metadata line:\n%s", prompt)
	}
	for _, want := range []string{
		"cerne status",
		"**Origem:**",
		spectmpl.OrigemExample,
		spectmpl.UnknownMarker,
		"palpite",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing METADADOS rule %q:\n%s", want, prompt)
		}
	}
}

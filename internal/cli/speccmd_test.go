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
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestBuildSpecDraftPrompt asserts the assembled prompt carries the feature's
// id, title and module, lists the template sections of the project's language
// in order, and closes with that language's directive (F-0447).
func TestBuildSpecDraftPrompt(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Bank reconciliation", Module: "finance", State: domain.StateCandidate}
	for _, l := range doclang.Langs() {
		t.Run(string(l), func(t *testing.T) {
			prompt := BuildSpecDraftPrompt(l, f, "# Bank reconciliation\n\n> Pending spec")
			for _, want := range []string{"F-0007", "Bank reconciliation", "finance"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt missing %q:\n%s", want, prompt)
				}
			}
			sections := doclang.Sections(doclang.Spec, l)
			for _, want := range sections {
				if !strings.Contains(prompt, "## "+want+"\n") {
					t.Fatalf("prompt missing required section %q:\n%s", want, prompt)
				}
			}
			// F-0207 criterion 3: the agent-execution section right after the
			// verification one (mirrors spectmpl.Body's placement).
			if i, j := strings.Index(prompt, "## "+sections[4]+"\n"), strings.Index(prompt, "## "+sections[5]+"\n"); i < 0 || j < i {
				t.Fatalf("prompt lacks %q after %q", sections[5], sections[4])
			}
			if !strings.HasSuffix(prompt, doclang.Directive(l)) {
				t.Fatalf("prompt does not end with the %s directive", l)
			}
			// F-0194: the prompt asks for the Level and forbids vendor model names.
			for _, want := range []string{"the Level", "do not name LLM models or vendors", "same agent"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt missing guidance %q:\n%s", want, prompt)
				}
			}
		})
	}
	// The Portuguese level vocabulary the directive quotes is the one spectmpl owns.
	if !strings.Contains(doclang.Directive(doclang.PT), strings.Join(spectmpl.Levels, " | ")) {
		t.Errorf("the pt-BR directive does not quote spectmpl.Levels %q", spectmpl.Levels)
	}
	if !strings.Contains(doclang.Directive(doclang.PT), spectmpl.AmbiguityMarker) {
		t.Errorf("the pt-BR directive does not quote spectmpl.AmbiguityMarker")
	}
}

// TestBuildSpecDraftPromptIncludesGauntletContract asserts the spec draft
// prompt carries the Gauntlet protocol block (F-0270's marker + the three
// literal Verification forms in the project's language), so a spec authored
// via CLI gets the same quality-bar contract as one elaborated through the
// Backlog chat (F-0273).
func TestBuildSpecDraftPromptIncludesGauntletContract(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Bank reconciliation", Module: "finance", State: domain.StateCandidate}
	for _, l := range doclang.Langs() {
		prompt := BuildSpecDraftPrompt(l, f, "")
		wants := []string{app.ChatGauntletMarker}
		for _, term := range []doclang.Term{doclang.BarLabel, doclang.MethodLabel, doclang.ExitLabel, doclang.NotApplicable, doclang.Waived} {
			wants = append(wants, doclang.Display(term, l))
		}
		for _, want := range wants {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s: prompt missing Gauntlet contract piece %q:\n%s", l, want, prompt)
			}
		}
	}
}

// TestSpecDraftPromptGauntletSharesSourceWithChat pins the single-source
// requirement (F-0273 criterion 2): the Gauntlet block embedded in the spec
// draft prompt must be byte-identical to app.GauntletContract — the same
// function the Backlog chat prompt uses — never a parallel copy that could
// drift silently.
func TestSpecDraftPromptGauntletSharesSourceWithChat(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Bank reconciliation", Module: "finance", State: domain.StateCandidate}
	for _, l := range doclang.Langs() {
		if !strings.Contains(BuildSpecDraftPrompt(l, f, ""), app.GauntletContract(l)) {
			t.Fatalf("%s: prompt does not contain app.GauntletContract verbatim", l)
		}
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
// prompt asks the model for the SAME three metadata rules the template teaches
// — no status line, an Origin in the shape of the example, and the
// unknown-field escape hatch instead of a guess; the unknown value itself and
// the labels come from the directive, in the project's language (F-0447).
func TestBuildSpecDraftPromptMetadataRules(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Bank reconciliation", Module: "finance", State: domain.StateCandidate}
	for _, l := range doclang.Langs() {
		prompt := BuildSpecDraftPrompt(l, f, "")
		if strings.Contains(prompt, "**Status:**") {
			t.Fatalf("prompt must not ask for a **Status:** metadata line:\n%s", prompt)
		}
		for _, want := range []string{"cerne status", "Origin line", "who reported it or where it came from", "never guess"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s: prompt missing metadata rule %q:\n%s", l, want, prompt)
			}
		}
	}
	if !strings.Contains(doclang.Directive(doclang.PT), `"`+spectmpl.UnknownMarker+`"`) {
		t.Errorf("the pt-BR directive does not quote spectmpl.UnknownMarker")
	}
	if !strings.Contains(doclang.Directive(doclang.PT), "**Origem:**") {
		t.Errorf("the pt-BR directive does not quote the Origem label")
	}
}

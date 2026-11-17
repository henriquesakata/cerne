package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestBuildPlanDraftPrompt asserts the assembled prompt carries the feature's
// id/title/module, embeds the approved spec as context, requests every canonical
// plan section of the project's language in order, teaches the plan authoring
// rules and closes with the directive (F-0447).
func TestBuildPlanDraftPrompt(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Bank reconciliation", Module: "finance", State: domain.StateApproved}
	spec := "## Goal\n\n1. WHEN the statement arrives THE SYSTEM SHALL reconcile."
	for _, l := range doclang.Langs() {
		t.Run(string(l), func(t *testing.T) {
			prompt := BuildPlanDraftPrompt(l, f, spec, "# Bank reconciliation\n\n> Pending plan", nil)
			for _, want := range []string{"F-0007", "Bank reconciliation", "finance"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt missing %q:\n%s", want, prompt)
				}
			}
			// The approved spec is fed in as context the plan must reference, not repeat.
			if !strings.Contains(prompt, "THE SYSTEM SHALL reconcile") {
				t.Fatalf("prompt should embed the approved spec as context:\n%s", prompt)
			}
			prev := -1
			for _, want := range doclang.Sections(doclang.Plan, l) {
				i := strings.Index(prompt, "## "+want+"\n")
				if i < 0 || i < prev {
					t.Fatalf("prompt lacks section %q in order:\n%s", want, prompt)
				}
				prev = i
			}
			if !strings.HasSuffix(prompt, doclang.Directive(l)) {
				t.Fatalf("prompt does not end with the %s directive", l)
			}
			// The plan rules: test-first, criterion traceability, right-sizing, binary done-check.
			for _, want := range []string{"test first", "number of the spec criterion", "Size by the work", "binary checklist"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("prompt missing authoring guidance %q:\n%s", want, prompt)
				}
			}
		})
	}
}

// TestBuildPlanDraftPromptListsChildren (F-0213 crit.3): when the feature is an
// epic (has children), the prompt lists the roster (id, title, state) and asks
// for the canonical execution-map format, whose "Etapa" lines stay as the
// parser reads them in every language (N-0087).
func TestBuildPlanDraftPromptListsChildren(t *testing.T) {
	f := store.Feature{ID: "F-0211", Title: "Epic", State: domain.StateApproved}
	children := []store.Feature{
		{ID: "T-0001", Title: "room manager on the server", State: domain.StateImplemented},
		{ID: "T-0003", Title: "WarRoom UI", State: domain.StateApproved},
	}
	for _, l := range doclang.Langs() {
		prompt := BuildPlanDraftPrompt(l, f, "", "", children)
		for _, want := range []string{"T-0001", "room manager on the server", "implemented", "T-0003", "WarRoom UI", "approved", "execution map", "Etapa 1: T-0001", "Etapa 2 (paralelas): T-0002"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s: prompt missing %q:\n%s", l, want, prompt)
			}
		}
	}
}

// TestPlanDraftByoKeyWritesAndCommits drives `cerne plan draft` in byo-key mode:
// the provider's body is written to the backend plan.md with cerne-stamped
// cerne_id frontmatter, origin advances and the lock snapshot is updated.
func TestPlanDraftByoKeyWritesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		// Long enough to clear app.ValidateDraftBody's floor (F-0290).
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"## Estratégia\n\nMR único, porque a mudança cabe em um pacote e não altera o schema do backend.\n\n## Sequência de tarefas\n\nTarefa 1: gerar boleto. Teste primeiro em internal/finance, depois a implementação mínima que o faz passar."}]}`)
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

	out, err := execCerne(t, "plan", "draft", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("plan draft byo-key: %v\n%s", err, out)
	}

	// The provider received a prompt mentioning the feature.
	if !strings.Contains(gotPrompt, "Boletos") || !strings.Contains(gotPrompt, "F-0001") {
		t.Fatalf("provider prompt missing the feature id/title:\n%s", gotPrompt)
	}

	// The plan was written to the backend clone with the synthesized body...
	plan, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "plan.md"))
	if err != nil {
		t.Fatalf("read drafted plan: %v", err)
	}
	if !strings.Contains(string(plan), "Tarefa 1: gerar boleto.") {
		t.Fatalf("plan missing the synthesized body:\n%s", plan)
	}
	// ...and cerne-stamped cerne_id frontmatter (not trusted from the model).
	if !strings.HasPrefix(string(plan), "---\ncerne_id: F-0001\n---\n") {
		t.Fatalf("plan missing cerne_id frontmatter stamped by cerne:\n%s", plan)
	}

	// Origin advanced (push happened).
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after plan draft")
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
		t.Fatalf("plan draft output missing confirmation:\n%s", out)
	}
}

// TestPlanDraftUnknownFeature errors when the id does not exist in the backend.
func TestPlanDraftUnknownFeature(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, "http://127.0.0.1:0", "TEST_BYO_KEY")

	_, err := execCerne(t, "plan", "draft", "F-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("plan draft on unknown feature succeeded, want error")
	}
	if !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("error %q does not name the missing feature", err)
	}
}

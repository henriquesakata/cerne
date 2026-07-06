package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/runtmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// newRunbook creates a runbook via the CLI and returns its expected ID.
func newRunbook(t *testing.T, projDir, title string, seq int) string {
	t.Helper()
	out, err := execCerne(t, "runbook", "new", title, "--dir", projDir)
	if err != nil {
		t.Fatalf("runbook new %q: %v\noutput: %s", title, err, out)
	}
	return fmt.Sprintf("RB-%04d", seq)
}

// TestRunbookNewWithFileBody: a runbook created with a --file body is one-shot —
// born active with real content, already committed and pushed (no draft+push
// round-trip). The CLI confirms it is synced and points at 'cerne pull', not the
// stub draft+push hint. Mirrors the note/reference one-shot flow.
func TestRunbookNewWithFileBody(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	content := "## Quando usar\nQuando o index.db corromper.\n\n" +
		"## Procedimento\n1. Apague o index.db.\n2. Rode 'cerne search' para reconstruir.\n"
	if err := os.WriteFile(bodyFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	out, err := execCerne(t, "runbook", "new", "Reconstruir índice", "--file", bodyFile, "--dir", projDir)
	if err != nil {
		t.Fatalf("runbook new --file: %v", err)
	}
	if !strings.Contains(out, "already synced") || !strings.Contains(out, "with a body") {
		t.Fatalf("runbook new --file output must confirm a synced body:\n%s", out)
	}
	if !strings.Contains(out, "cerne pull") {
		t.Fatalf("runbook new --file output must point at 'cerne pull' to materialize:\n%s", out)
	}
	if strings.Contains(out, "preencha") || strings.Contains(out, "cerne push") {
		t.Fatalf("runbook new --file must not print the stub draft+push hint:\n%s", out)
	}

	body, err := os.ReadFile(filepath.Join(repoDir, "content", "runbooks", "RB-0001.md"))
	if err != nil {
		t.Fatalf("read runbook doc: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\ncerne_id: RB-0001\n---") {
		t.Fatalf("runbook doc missing cerne_id frontmatter:\n%s", body)
	}
	if !strings.Contains(string(body), "Apague o index.db") {
		t.Fatalf("runbook body not written from --file:\n%s", body)
	}
	if !doccheck.HasRealContent(body) {
		t.Fatalf("a runbook with a --file body must read as real content:\n%s", body)
	}
}

// TestRunbookRetag: retag updates one axis, preserves the other, rejects an empty
// invocation and an unknown facet (parity with decision/reference retag, F-0110).
func TestRunbookRetag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newRunbook(t, projDir, "Deploy", 1)
	if _, err := execCerne(t, "runbook", "retag", "RB-0001", "--facet", "infra", "--dir", projDir); err != nil {
		t.Fatalf("runbook retag --facet: %v", err)
	}
	out, err := execCerne(t, "runbook", "retag", "RB-0001", "--component", "gitback", "--dir", projDir)
	if err != nil {
		t.Fatalf("runbook retag --component: %v\n%s", err, out)
	}
	rb, _ := store.LoadRunbook(repoDir, "RB-0001")
	if len(rb.Facets) != 1 || rb.Facets[0] != "infra" || len(rb.Components) != 1 || rb.Components[0] != "gitback" {
		t.Fatalf("after retag = %v / %v, want [infra] / [gitback]", rb.Facets, rb.Components)
	}
	// no axis flag → error.
	if _, err := execCerne(t, "runbook", "retag", "RB-0001", "--dir", projDir); err == nil {
		t.Fatalf("retag with no axis succeeded, want error")
	}
	// unknown facet → error.
	if _, err := execCerne(t, "runbook", "retag", "RB-0001", "--facet", "prod", "--dir", projDir); err == nil {
		t.Fatalf("retag --facet prod succeeded, want ErrUnknownFacet")
	}
}

func TestRunbookNew(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)

	out, err := execCerne(t, "runbook", "new", "Recuperar dev env", "--dir", projDir)
	if err != nil {
		t.Fatalf("runbook new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "RB-0001 created (active)") {
		t.Fatalf("runbook new output missing RB-0001 confirmation:\n%s", out)
	}

	rb, err := store.LoadRunbook(repoDir, "RB-0001")
	if err != nil {
		t.Fatalf("LoadRunbook: %v", err)
	}
	if rb.State != domain.RunbookActive || rb.Title != "Recuperar dev env" {
		t.Fatalf("runbook = %+v, want active titled 'Recuperar dev env'", rb)
	}

	// scaffolded from the canonical template.
	body, err := os.ReadFile(filepath.Join(repoDir, "content", "runbooks", "RB-0001.md"))
	if err != nil {
		t.Fatalf("read runbook doc: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\ncerne_id: RB-0001\n---") {
		t.Fatalf("runbook doc missing cerne_id frontmatter:\n%s", body)
	}
	for _, want := range []string{"## Quando usar", "## Procedimento", "## Rollback e troubleshooting"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("runbook doc not scaffolded from the template (missing %q):\n%s", want, body)
		}
	}

	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "runbook.created" || last.EntityID != "RB-0001" || last.Payload["title"] != "Recuperar dev env" {
		t.Fatalf("last event = %+v, want runbook.created for RB-0001 with title", last)
	}

	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	head, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if lock.Snapshot != head {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, head)
	}

	if _, err := execCerne(t, "runbook", "new", "   ", "--dir", projDir); err == nil {
		t.Fatalf("runbook new with blank title succeeded, want error")
	}
}

func TestRunbookSubstituir(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newRunbook(t, projDir, "Runbook A", 1)
	id2 := newRunbook(t, projDir, "Runbook B", 2)

	if _, err := execCerne(t, "runbook", "substituir", id, "--by", id, "--reason", "dup", "--dir", projDir); err == nil {
		t.Fatalf("substituir --by self succeeded, want error")
	}
	if _, err := execCerne(t, "runbook", "substituir", id, "--by", "RB-9999", "--reason", "dup", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "RB-9999") {
		t.Fatalf("substituir --by nonexistent err = %v, want error naming RB-9999", err)
	}

	out, err := execCerne(t, "runbook", "substituir", id, "--by", id2, "--reason", "obsoleto", "--dir", projDir)
	if err != nil {
		t.Fatalf("substituir: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing old→new state:\n%s", out)
	}
	rb, err := store.LoadRunbook(repoDir, id)
	if err != nil {
		t.Fatalf("LoadRunbook: %v", err)
	}
	if rb.State != domain.RunbookSuperseded || rb.SupersededBy != id2 || rb.Reason != "obsoleto" {
		t.Fatalf("runbook = %+v, want superseded by %s with reason", rb, id2)
	}

	// a terminal (already superseded) runbook cannot be superseded again.
	if _, err := execCerne(t, "runbook", "substituir", id, "--by", id2, "--reason", "again", "--dir", projDir); err == nil {
		t.Fatalf("substituir already-superseded runbook succeeded, want error")
	}
}

func TestRunbookRetire(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newRunbook(t, projDir, "Procedimento que virou nota", 1)

	// --reason is required.
	if _, err := execCerne(t, "runbook", "retire", id, "--dir", projDir); err == nil {
		t.Fatalf("runbook retire without --reason succeeded, want error")
	}

	// retire moves the runbook to superseded with NO successor (like substituir
	// sem --by), and the output reads as a retirement.
	out, err := execCerne(t, "runbook", "retire", id, "--reason", "reclassificado como N-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("runbook retire: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "retired") || !strings.Contains(out, "superseded") {
		t.Fatalf("runbook retire output missing retired/superseded:\n%s", out)
	}
	rb, err := store.LoadRunbook(repoDir, id)
	if err != nil {
		t.Fatalf("LoadRunbook: %v", err)
	}
	if rb.State != domain.RunbookSuperseded || rb.SupersededBy != "" || rb.Reason != "reclassificado como N-0001" {
		t.Fatalf("runbook = %+v, want superseded with no successor and the reason", rb)
	}

	// a terminal (already superseded) runbook cannot be retired again.
	if _, err := execCerne(t, "runbook", "retire", id, "--reason", "again", "--dir", projDir); err == nil {
		t.Fatalf("retire already-superseded runbook succeeded, want error")
	}
}

func TestBuildRunbookDraftPrompt(t *testing.T) {
	rb := store.Runbook{ID: "RB-0007", Title: "Rotacionar chave", State: domain.RunbookActive}
	prompt := BuildRunbookDraftPrompt(rb, "# Rotacionar chave\n\n> Runbook pendente")

	for _, want := range []string{"RB-0007", "Rotacionar chave"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, want := range runtmpl.Sections {
		if !strings.Contains(prompt, "## "+want) {
			t.Fatalf("prompt missing required section %q:\n%s", want, prompt)
		}
	}
	// Teaches the shared ambiguity marker and the runbook rules: expected
	// output per step, escalation.
	for _, want := range []string{spectmpl.AmbiguityMarker, "SAÍDA ESPERADA", "escalar"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing authoring guidance %q:\n%s", want, prompt)
		}
	}
}

func TestRunbookDraftByoKeyWritesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	if out, err := execCerne(t, "runbook", "new", "Rotacionar chave", "--dir", projDir); err != nil {
		t.Fatalf("runbook new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"## Quando usar\n\nAo rotacionar.\n\n## Procedimento\n\n1. Gere a chave."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "runbook", "draft", "RB-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("runbook draft byo-key: %v\n%s", err, out)
	}

	if !strings.Contains(gotPrompt, "Rotacionar chave") || !strings.Contains(gotPrompt, "RB-0001") {
		t.Fatalf("provider prompt missing the runbook id/title:\n%s", gotPrompt)
	}

	doc, err := os.ReadFile(filepath.Join(repoDir, "content", "runbooks", "RB-0001.md"))
	if err != nil {
		t.Fatalf("read drafted runbook: %v", err)
	}
	if !strings.Contains(string(doc), "1. Gere a chave.") {
		t.Fatalf("runbook missing the synthesized body:\n%s", doc)
	}
	if !strings.HasPrefix(string(doc), "---\ncerne_id: RB-0001\n---\n") {
		t.Fatalf("runbook missing cerne_id frontmatter stamped by cerne:\n%s", doc)
	}

	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after runbook draft")
	}
	if !strings.Contains(out, "drafted and pushed") {
		t.Fatalf("runbook draft output missing confirmation:\n%s", out)
	}
}

// plantRunbookRef writes a reference entity + its content doc carrying the given
// imported_from directly into the backend clone, as `cerne import` would have left
// a docs/runbooks doc before the runbook entity existed.
func plantRunbookRef(t *testing.T, repoDir, id, title, importedFrom string) {
	t.Helper()
	if err := store.SaveReference(repoDir, store.Reference{ID: id, Title: title, State: domain.ReferenceCurrent}); err != nil {
		t.Fatalf("SaveReference %s: %v", id, err)
	}
	doc := "---\ncerne_id: " + id + "\nimported_from: " + importedFrom + "\n---\n\n# " + title + "\n\nProcedure.\n"
	p := filepath.Join(repoDir, "content", "references", id+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunbookMigrateDryRun(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	plantRunbookRef(t, repoDir, "R-0001", "Deploy", "docs/runbooks/deploy.md")

	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}
	out, err := execCerne(t, "runbook", "migrate", "--dir", projDir)
	if err != nil {
		t.Fatalf("migrate dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "R-0001") || !strings.Contains(out, "--yes") {
		t.Fatalf("dry-run should list R-0001 and mention --yes:\n%s", out)
	}
	// dry-run writes nothing and pushes nothing
	if _, err := store.LoadReference(repoDir, "R-0001"); err != nil {
		t.Fatalf("dry-run removed the reference: %v", err)
	}
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter != headBefore {
		t.Fatalf("dry-run advanced origin HEAD")
	}
}

func TestRunbookMigrateYes(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	plantRunbookRef(t, repoDir, "R-0001", "Deploy", "docs/runbooks/deploy.md")
	plantRunbookRef(t, repoDir, "R-0002", "Smoke", "docs/runbooks/smoke.md")
	commitBackend(t, repoDir)

	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}
	out, err := execCerne(t, "runbook", "migrate", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("migrate --yes: %v\n%s", err, out)
	}
	if !strings.Contains(out, "R-0001 → RB-0001") {
		t.Fatalf("output missing migration line:\n%s", out)
	}
	// runbooks created, refs gone
	if _, err := store.LoadRunbook(repoDir, "RB-0001"); err != nil {
		t.Fatalf("RB-0001 not created: %v", err)
	}
	if _, err := store.LoadReference(repoDir, "R-0001"); err == nil {
		t.Fatalf("R-0001 still present after migrate")
	}
	// committed + pushed: origin HEAD advanced and the lock pins it
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after migrate --yes")
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Snapshot != headAfter {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, headAfter)
	}
}

func TestRunbookDraftUnknownRunbook(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, "http://127.0.0.1:0", "TEST_BYO_KEY")

	_, err := execCerne(t, "runbook", "draft", "RB-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("runbook draft on unknown runbook succeeded, want error")
	}
	if !strings.Contains(err.Error(), "RB-9999") {
		t.Fatalf("error %q does not name the missing runbook", err)
	}
}

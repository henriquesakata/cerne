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

	"gitlab.com/rockt/cerne/internal/dectmpl"
	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// newDecision creates a decision via the CLI and returns its expected ID.
func newDecision(t *testing.T, projDir, title string, seq int) string {
	t.Helper()
	out, err := execCerne(t, "decision", "new", title, "--dir", projDir)
	if err != nil {
		t.Fatalf("decision new %q: %v\noutput: %s", title, err, out)
	}
	return fmt.Sprintf("D-%04d", seq)
}

// TestDecisionNewWithFileBody: a decision created with a --file body is one-shot —
// born active with a real ADR, already committed and pushed (no draft+push
// round-trip). The CLI confirms it is synced and points at 'cerne pull', not the
// stub draft+push hint. Mirrors the note/reference one-shot flow.
func TestDecisionNewWithFileBody(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	content := "## Contexto\nPrecisamos de busca full-text local.\n\n" +
		"## Decisão\nUsaremos SQLite FTS5 como cache derivado.\n\n" +
		"## Consequências\nO índice é descartável e reconstruível a partir do backend.\n"
	if err := os.WriteFile(bodyFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	out, err := execCerne(t, "decision", "new", "Usar SQLite FTS5", "--file", bodyFile, "--dir", projDir)
	if err != nil {
		t.Fatalf("decision new --file: %v", err)
	}
	if !strings.Contains(out, "already synced") || !strings.Contains(out, "with a body") {
		t.Fatalf("decision new --file output must confirm a synced body:\n%s", out)
	}
	if !strings.Contains(out, "cerne pull") {
		t.Fatalf("decision new --file output must point at 'cerne pull' to materialize:\n%s", out)
	}
	if strings.Contains(out, "preencha") || strings.Contains(out, "cerne push") {
		t.Fatalf("decision new --file must not print the stub draft+push hint:\n%s", out)
	}

	body, err := os.ReadFile(filepath.Join(repoDir, "content", "decisions", "D-0001.md"))
	if err != nil {
		t.Fatalf("read decision doc: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\ncerne_id: D-0001\n---") {
		t.Fatalf("decision doc missing cerne_id frontmatter:\n%s", body)
	}
	if !strings.Contains(string(body), "FTS5 como cache derivado") {
		t.Fatalf("decision body not written from --file:\n%s", body)
	}
	if !doccheck.HasRealContent(body) {
		t.Fatalf("a decision with a --file body must read as real content:\n%s", body)
	}
}

func TestDecisionNew(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)

	out, err := execCerne(t, "decision", "new", "Usar Postgres", "--dir", projDir)
	if err != nil {
		t.Fatalf("decision new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "D-0001 created (active)") {
		t.Fatalf("decision new output missing D-0001 confirmation:\n%s", out)
	}

	d, err := store.LoadDecision(repoDir, "D-0001")
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if d.State != domain.DecisionActive || d.Title != "Usar Postgres" {
		t.Fatalf("decision = %+v, want active titled 'Usar Postgres'", d)
	}

	// ADR doc with cerne_id frontmatter, scaffolded from the canonical template.
	body, err := os.ReadFile(filepath.Join(repoDir, "content", "decisions", "D-0001.md"))
	if err != nil {
		t.Fatalf("read decision doc: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\ncerne_id: D-0001\n---") {
		t.Fatalf("decision doc missing cerne_id frontmatter:\n%s", body)
	}
	for _, want := range []string{"## Contexto", "## Decisão", "## Consequências"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("decision doc not scaffolded from the template (missing %q):\n%s", want, body)
		}
	}

	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "decision.created" || last.EntityID != "D-0001" || last.Payload["title"] != "Usar Postgres" {
		t.Fatalf("last event = %+v, want decision.created for D-0001 with title", last)
	}

	// lock snapshot tracks the pushed origin HEAD.
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	head, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if lock.Snapshot != head {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, head)
	}

	// empty title is rejected.
	if _, err := execCerne(t, "decision", "new", "   ", "--dir", projDir); err == nil {
		t.Fatalf("decision new with blank title succeeded, want error")
	}
}

func TestDecisionSubstituir(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newDecision(t, projDir, "ADR A", 1)
	id2 := newDecision(t, projDir, "ADR B", 2)

	// --by self is rejected.
	if _, err := execCerne(t, "decision", "substituir", id, "--by", id, "--reason", "dup", "--dir", projDir); err == nil {
		t.Fatalf("substituir --by self succeeded, want error")
	}
	// --by must exist in the backend.
	if _, err := execCerne(t, "decision", "substituir", id, "--by", "D-9999", "--reason", "dup", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "D-9999") {
		t.Fatalf("substituir --by nonexistent err = %v, want error naming D-9999", err)
	}
	// the decision itself must exist.
	if _, err := execCerne(t, "decision", "substituir", "D-9999", "--by", id2, "--reason", "x", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "D-9999") {
		t.Fatalf("substituir missing decision err = %v, want error naming D-9999", err)
	}

	out, err := execCerne(t, "decision", "substituir", id, "--by", id2, "--reason", "trocamos de banco", "--dir", projDir)
	if err != nil {
		t.Fatalf("substituir: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing old→new state:\n%s", out)
	}
	d, err := store.LoadDecision(repoDir, id)
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if d.State != domain.DecisionSuperseded || d.SupersededBy != id2 || d.Reason != "trocamos de banco" {
		t.Fatalf("decision = %+v, want superseded by %s with reason", d, id2)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "decision.superseded" || last.Payload["by"] != id2 || last.Payload["reason"] != "trocamos de banco" {
		t.Fatalf("last event = %+v, want decision.superseded with by/reason payload", last)
	}

	// a terminal (already superseded) decision cannot be superseded again.
	if _, err := execCerne(t, "decision", "substituir", id, "--by", id2, "--reason", "again", "--dir", projDir); err == nil {
		t.Fatalf("substituir already-superseded decision succeeded, want error")
	}
}

func TestDecisionSubstituirFlagRules(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newDecision(t, projDir, "ADR A", 1)
	id2 := newDecision(t, projDir, "ADR B", 2)

	if _, err := execCerne(t, "decision", "substituir", id, "--reason", "x", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--by") {
		t.Fatalf("substituir without --by err = %v, want error mentioning --by", err)
	}
	if _, err := execCerne(t, "decision", "substituir", id, "--by", id2, "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("substituir without --reason err = %v, want error mentioning --reason", err)
	}
}

// TestBuildDecisionDraftPrompt asserts the assembled prompt carries the decision
// id/title, requests every canonical ADR section in order, and teaches the ADR
// authoring rules.
func TestBuildDecisionDraftPrompt(t *testing.T) {
	d := store.Decision{ID: "D-0007", Title: "Adotar micro-frontends", State: domain.DecisionActive}
	prompt := BuildDecisionDraftPrompt(d, "# Adotar micro-frontends\n\n> Decisão pendente")

	for _, want := range []string{"D-0007", "Adotar micro-frontends"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, want := range dectmpl.Sections {
		if !strings.Contains(prompt, "## "+want) {
			t.Fatalf("prompt missing required section %q:\n%s", want, prompt)
		}
	}
	// The prompt teaches the shared ambiguity marker and the ADR rules:
	// imperative decision, mandatory negatives, why-rejected alternatives.
	for _, want := range []string{spectmpl.AmbiguityMarker, "imperativo", "negativas", "rejeitadas", "registro histórico"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing authoring guidance %q:\n%s", want, prompt)
		}
	}
}

// TestDecisionDraftByoKeyWritesAndCommits drives `cerne decision draft` in
// byo-key mode: the provider's body is written to the backend ADR with
// cerne-stamped cerne_id frontmatter and origin advances.
func TestDecisionDraftByoKeyWritesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	if out, err := execCerne(t, "decision", "new", "Usar Postgres", "--dir", projDir); err != nil {
		t.Fatalf("decision new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"## Contexto\n\nForças em tensão.\n\n## Decisão\n\nUsaremos Postgres."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "decision", "draft", "D-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("decision draft byo-key: %v\n%s", err, out)
	}

	if !strings.Contains(gotPrompt, "Usar Postgres") || !strings.Contains(gotPrompt, "D-0001") {
		t.Fatalf("provider prompt missing the decision id/title:\n%s", gotPrompt)
	}

	adr, err := os.ReadFile(filepath.Join(repoDir, "content", "decisions", "D-0001.md"))
	if err != nil {
		t.Fatalf("read drafted ADR: %v", err)
	}
	if !strings.Contains(string(adr), "Usaremos Postgres.") {
		t.Fatalf("ADR missing the synthesized body:\n%s", adr)
	}
	if !strings.HasPrefix(string(adr), "---\ncerne_id: D-0001\n---\n") {
		t.Fatalf("ADR missing cerne_id frontmatter stamped by cerne:\n%s", adr)
	}

	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after decision draft")
	}
	if !strings.Contains(out, "drafted and pushed") {
		t.Fatalf("decision draft output missing confirmation:\n%s", out)
	}
}

// TestDecisionNewWithTags: --facet/--component are normalized and persisted on
// the decision, mirroring 'note new'.
func TestDecisionNewWithTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "decision", "new", "Usar Keycloak", "--facet", "Auth,auth", "--component", "Keycloak", "--dir", projDir)
	if err != nil {
		t.Fatalf("decision new --facet: %v\n%s", err, out)
	}
	d, err := store.LoadDecision(repoDir, "D-0001")
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if len(d.Facets) != 1 || d.Facets[0] != "auth" || len(d.Components) != 1 || d.Components[0] != "keycloak" {
		t.Fatalf("tags = %v / %v, want [auth] / [keycloak]", d.Facets, d.Components)
	}
	// an unknown facet is rejected.
	if _, err := execCerne(t, "decision", "new", "X", "--facet", "prod", "--dir", projDir); err == nil {
		t.Fatalf("decision new --facet prod succeeded, want ErrUnknownFacet")
	}
}

// TestDecisionList covers active-by-default, --all, --facet/--component filters
// (AND), and --json, mirroring 'note list'.
func TestDecisionList(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "decision", "new", "ADR Auth", "--facet", "auth", "--component", "keycloak", "--dir", projDir); err != nil {
		t.Fatalf("new D-0001: %v", err)
	}
	if _, err := execCerne(t, "decision", "new", "ADR Infra", "--facet", "infra", "--dir", projDir); err != nil {
		t.Fatalf("new D-0002: %v", err)
	}
	// supersede D-0002 (becomes superseded; D-0001 stays active).
	if _, err := execCerne(t, "decision", "substituir", "D-0002", "--by", "D-0001", "--reason", "x", "--dir", projDir); err != nil {
		t.Fatalf("substituir: %v", err)
	}

	// default: only active.
	out, err := execCerne(t, "decision", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("decision list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "D-0001") || strings.Contains(out, "D-0002") {
		t.Fatalf("default list should show only active D-0001:\n%s", out)
	}
	// --all includes superseded.
	out, _ = execCerne(t, "decision", "list", "--all", "--dir", projDir)
	if !strings.Contains(out, "D-0001") || !strings.Contains(out, "D-0002") {
		t.Fatalf("--all should include superseded D-0002:\n%s", out)
	}
	// --facet filters (AND); auth matches D-0001 only.
	out, _ = execCerne(t, "decision", "list", "--facet", "auth", "--dir", projDir)
	if !strings.Contains(out, "D-0001") {
		t.Fatalf("--facet auth should show D-0001:\n%s", out)
	}
	// --facet infra over active set is empty (D-0002 superseded).
	out, _ = execCerne(t, "decision", "list", "--facet", "infra", "--dir", projDir)
	if strings.Contains(out, "D-0002") {
		t.Fatalf("--facet infra (active only) should be empty:\n%s", out)
	}
	// --component filters.
	out, _ = execCerne(t, "decision", "list", "--component", "keycloak", "--dir", projDir)
	if !strings.Contains(out, "D-0001") {
		t.Fatalf("--component keycloak should show D-0001:\n%s", out)
	}
	// --json carries the tags.
	out, _ = execCerne(t, "decision", "list", "--json", "--dir", projDir)
	if !strings.Contains(out, "\"facets\"") || !strings.Contains(out, "auth") {
		t.Fatalf("--json should carry facets:\n%s", out)
	}
}

// TestDecisionRetag: retag updates one axis, preserves the other, rejects an
// empty invocation and an unknown facet.
func TestDecisionRetag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "decision", "new", "ADR", "--facet", "auth", "--dir", projDir); err != nil {
		t.Fatalf("new: %v", err)
	}
	out, err := execCerne(t, "decision", "retag", "D-0001", "--component", "keycloak", "--dir", projDir)
	if err != nil {
		t.Fatalf("decision retag: %v\n%s", err, out)
	}
	d, _ := store.LoadDecision(repoDir, "D-0001")
	if len(d.Facets) != 1 || d.Facets[0] != "auth" || len(d.Components) != 1 || d.Components[0] != "keycloak" {
		t.Fatalf("after retag = %v / %v, want [auth] / [keycloak]", d.Facets, d.Components)
	}
	// no axis flag → error.
	if _, err := execCerne(t, "decision", "retag", "D-0001", "--dir", projDir); err == nil {
		t.Fatalf("retag with no axis succeeded, want error")
	}
	// unknown facet → error.
	if _, err := execCerne(t, "decision", "retag", "D-0001", "--facet", "prod", "--dir", projDir); err == nil {
		t.Fatalf("retag --facet prod succeeded, want ErrUnknownFacet")
	}
}

// TestDecisionDraftUnknownDecision errors when the id does not exist.
func TestDecisionDraftUnknownDecision(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, "http://127.0.0.1:0", "TEST_BYO_KEY")

	_, err := execCerne(t, "decision", "draft", "D-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("decision draft on unknown decision succeeded, want error")
	}
	if !strings.Contains(err.Error(), "D-9999") {
		t.Fatalf("error %q does not name the missing decision", err)
	}
}

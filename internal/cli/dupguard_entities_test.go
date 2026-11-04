package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

func writeBodyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// F-0324 crit. 1, 4, 7, 8, 12: the second equivalent `note new` fails naming the
// first id (also via --file), writes nothing, and --force goes through.
func TestNoteNewDupGuard(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	const title = "Desligar capacidade rebaixa a cobrança"
	if out, err := execCerne(t, "note", "new", title, "--dir", projDir); err != nil {
		t.Fatalf("first: %v\n%s", err, out)
	}
	out, err := execCerne(t, "note", "new", title, "--dir", projDir)
	if err == nil {
		t.Fatalf("second note new must fail:\n%s", out)
	}
	for _, want := range []string{"N-0001", "cerne note retag N-0001", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
	// One-shot path: same guard, same message, before writing.
	f := writeBodyFile(t, "corpo real da nota\n")
	_, err = execCerne(t, "note", "new", title, "--file", f, "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "N-0001") {
		t.Fatalf("--file path must be refused naming N-0001, got %v", err)
	}
	if notes, _ := store.LoadAllNotes(repoDir); len(notes) != 1 {
		t.Fatalf("notes = %d, want 1", len(notes))
	}
	if out, err := execCerne(t, "note", "new", title, "--file", f, "--force", "--dir", projDir); err != nil {
		t.Fatalf("--force: %v\n%s", err, out)
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "note.created" || last.Payload["guard"] != "forced" {
		t.Fatalf("last event = %+v, want note.created guard=forced", last)
	}
}

func TestDecisionNewDupGuard(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	const title = "Usar Keycloak para OIDC"
	if out, err := execCerne(t, "decision", "new", title, "--dir", projDir); err != nil {
		t.Fatalf("first: %v\n%s", err, out)
	}
	f := writeBodyFile(t, "adr real\n")
	_, err := execCerne(t, "decision", "new", title, "--file", f, "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "D-0001") || !strings.Contains(err.Error(), "cerne decision retag D-0001") {
		t.Fatalf("want refusal naming D-0001 + retag verb, got %v", err)
	}
	if ds, _ := store.LoadAllDecisions(repoDir); len(ds) != 1 {
		t.Fatalf("decisions = %d, want 1", len(ds))
	}
	if out, err := execCerne(t, "decision", "new", title, "--force", "--dir", projDir); err != nil {
		t.Fatalf("--force: %v\n%s", err, out)
	}
}

func TestReferenceNewDupGuard(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	f := writeBodyFile(t, "conteúdo\n")
	if out, err := execCerne(t, "reference", "new", "Spec Kit", "--url", "https://Example.com/a/", "--file", f, "--dir", projDir); err != nil {
		t.Fatalf("first: %v\n%s", err, out)
	}
	// Similar title, different URL: allowed.
	if out, err := execCerne(t, "reference", "new", "Spec Kit", "--url", "https://example.com/b", "--file", f, "--dir", projDir); err != nil {
		t.Fatalf("different URL: %v\n%s", err, out)
	}
	// Different title, same normalized URL: refused.
	_, err := execCerne(t, "reference", "new", "Outro", "--url", "https://example.com/a#x", "--file", f, "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "R-0001") || !strings.Contains(err.Error(), "cerne reference retag R-0001") {
		t.Fatalf("want refusal naming R-0001 + retag verb, got %v", err)
	}
	if rs, _ := store.LoadAllReferences(repoDir); len(rs) != 2 {
		t.Fatalf("references = %d, want 2", len(rs))
	}
	if out, err := execCerne(t, "reference", "new", "Outro", "--url", "https://example.com/a#x", "--file", f, "--force", "--dir", projDir); err != nil {
		t.Fatalf("--force: %v\n%s", err, out)
	}
}

func TestReferenceRetire(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	f := writeBodyFile(t, "conteúdo\n")
	if out, err := execCerne(t, "reference", "new", "Fonte", "--url", "http://a", "--file", f, "--dir", projDir); err != nil {
		t.Fatalf("new: %v\n%s", err, out)
	}
	if _, err := execCerne(t, "reference", "retire", "R-0001", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--reason is required") {
		t.Fatalf("missing reason err = %v", err)
	}
	out, err := execCerne(t, "reference", "retire", "R-0001", "--reason", "duplicate of R-0002", "--dir", projDir)
	if err != nil || !strings.Contains(out, "R-0001 retired: current → obsolete") {
		t.Fatalf("retire: %v\n%s", err, out)
	}
	if r, _ := store.LoadReference(repoDir, "R-0001"); r.State != "obsolete" {
		t.Fatalf("state = %s", r.State)
	}
	if _, err := execCerne(t, "reference", "retire", "R-0001", "--reason", "again", "--dir", projDir); err == nil {
		t.Fatal("second retire must fail")
	}
	// retired no longer blocks a new registration of the same URL
	if out, err := execCerne(t, "reference", "new", "Fonte", "--url", "http://a", "--file", f, "--dir", projDir); err != nil {
		t.Fatalf("after retire: %v\n%s", err, out)
	}
}

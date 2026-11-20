package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// contextProject is a project with feature F-0001 (component gitea, a real
// spec), a note informing it and a note sharing its component.
func contextProject(t *testing.T) string {
	t.Helper()
	projDir, _, _ := setupProject(t)
	spec := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(spec, []byte("# Conta digital\n\n## Problema\n\nAbrir conta sem agência.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"feature", "new", "Conta digital", "--file", spec, "--component", "gitea"},
		{"note", "new", "Token do forge", "--informs", "F-0001", "--gist", "use o token de root"},
		{"note", "new", "Webhook do forge", "--component", "gitea"},
	} {
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	return projDir
}

// E-0005 criteria 6 and 7 through the CLI (T-0017): the text package — the
// stage document, the rings, the close — and --budget.
func TestContextCmdText(t *testing.T) {
	projDir := contextProject(t)
	out, err := execCerne(t, "context", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("context: %v\n%s", err, out)
	}
	for _, want := range []string{
		"F-0001  [candidate]  Conta digital\n",
		"\n## spec\n", "Abrir conta sem agência.",
		"[a] N-0001  note  Token do forge — use o token de root\n",
		"[c] N-0002  note  Webhook do forge  · gitea\n",
		"\n## on demand (components gitea)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("context output missing %q:\n%s", want, out)
		}
	}
	small, err := execCerne(t, "context", "F-0001", "--budget", "1", "--dir", projDir)
	if err != nil {
		t.Fatalf("context --budget 1: %v\n%s", err, small)
	}
	if !strings.Contains(small, "Abrir conta sem agência.") || strings.Contains(small, "N-0001") ||
		!strings.Contains(small, "left out: a 1 · b 0 · c 1 · d 0") {
		t.Fatalf("context --budget 1 keeps the document and says what it left out:\n%s", small)
	}
	if _, err := execCerne(t, "context", "N-0001", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "feature, epic, task or bug") {
		t.Fatalf("context of a note: %v", err)
	}
}

// E-0005 criterion 8 (T-0017): --json is the same package, structured.
func TestContextCmdJSON(t *testing.T) {
	projDir := contextProject(t)
	js, err := execCerne(t, "context", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("context --json: %v\n%s", err, js)
	}
	var p app.TaskPack
	if err := json.Unmarshal([]byte(js), &p); err != nil {
		t.Fatalf("context --json: %v\n%s", err, js)
	}
	if p.ID != "F-0001" || p.Doc.Name != "spec" || len(p.Items) != 2 || p.Items[0].ID != "N-0001" || p.Items[0].Ring != "a" ||
		p.Items[1].Ring != "c" || p.Budget != app.DefaultContextBudget || len(p.OnDemand) != 4 {
		t.Fatalf("context --json = %+v", p)
	}
	text, err := execCerne(t, "context", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if text != p.Text() {
		t.Fatalf("the text is not the JSON's package:\n--- text\n%s\n--- json\n%s", text, p.Text())
	}
}

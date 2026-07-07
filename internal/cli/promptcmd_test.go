package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedPromptTemplate commits a live prompt-template note into the backend.
func seedPromptTemplate(t *testing.T, repoDir string) {
	t.Helper()
	if err := store.SaveNote(repoDir, store.Note{
		ID: "N-0001", Title: "Investigação de erro", State: domain.NoteActive,
		Components: []string{"prompt-template"},
	}); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	p := filepath.Join(repoDir, "content", "notes", "N-0001.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("Use a skill observability.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed template"); err != nil {
		t.Fatalf("seed CommitAndPush: %v", err)
	}
}

func TestPromptComposeHappy(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedPromptTemplate(t, repoDir)

	out, err := execCerne(t, "prompt", "compose",
		"--type", "investigacao-de-erro",
		"--text", "Investigar lentidão nos approves.",
		"--dir", projDir)
	if err != nil {
		t.Fatalf("prompt compose: %v\n%s", err, out)
	}
	for _, want := range []string{
		"## Intenção",
		"Investigar lentidão nos approves.",
		"Use a skill observability.",
		"Débitos de lint:",
		"## Registro",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPromptComposeList(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedPromptTemplate(t, repoDir)

	out, err := execCerne(t, "prompt", "compose", "--list", "--dir", projDir)
	if err != nil {
		t.Fatalf("prompt compose --list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "investigacao-de-erro") || !strings.Contains(out, "Investigação de erro") {
		t.Fatalf("list missing the seeded type:\n%s", out)
	}
}

func TestPromptComposeUnknownType(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedPromptTemplate(t, repoDir)

	out, err := execCerne(t, "prompt", "compose", "--type", "nao-existe", "--text", "x", "--dir", projDir)
	if err == nil {
		t.Fatalf("unknown type must fail; output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "investigacao-de-erro") {
		t.Fatalf("error must list available types, got: %v", err)
	}
}

func TestPromptComposeRegister(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedPromptTemplate(t, repoDir)

	out, err := execCerne(t, "prompt", "compose",
		"--type", "investigacao-de-erro",
		"--text", "Corrigir drift nos runbooks.",
		"--register", "--dir", projDir)
	if err != nil {
		t.Fatalf("prompt compose --register: %v\n%s", err, out)
	}
	if !strings.Contains(out, "cerne work done W-") {
		t.Fatalf("prompt missing embedded closing instruction:\n%s", out)
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range works {
		if w.State == domain.WorkActive && strings.Contains(w.Title, "Corrigir drift") {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered work not found in backend: %+v", works)
	}
}

package prompt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// promptFixture builds a git-less backend with one live template note, one
// superseded template note, a live feature, a done feature, an active work
// and a done work — the board the composed prompt must snapshot.
func promptFixture(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir = t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Projeto Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	saveTemplateNote(t, repoDir, "N-0001", "Investigação de erro", domain.NoteActive,
		"Use a skill observability. Comece por /api/debug/timings.")
	saveTemplateNote(t, repoDir, "N-0002", "Template antigo", domain.NoteSuperseded, "obsoleto")

	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Feature viva", State: domain.StateCandidate}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "Feature entregue", State: domain.StateDone}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := store.SaveWork(repoDir, store.Work{ID: "W-0001", Title: "Trabalho em curso", State: domain.WorkActive, Actor: "agent", Refs: []string{"F-0001"}}); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if err := store.SaveWork(repoDir, store.Work{ID: "W-0002", Title: "Trabalho fechado", State: domain.WorkDone, Actor: "agent"}); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}

	projDir = t.TempDir()
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: repoDir}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return repoDir, projDir
}

func saveTemplateNote(t *testing.T, repoDir, id, title string, state domain.NoteState, body string) {
	t.Helper()
	if err := store.SaveNote(repoDir, store.Note{ID: id, Title: title, State: state, Components: []string{"prompt-template"}}); err != nil {
		t.Fatalf("SaveNote %s: %v", id, err)
	}
	p := filepath.Join(repoDir, "content", "notes", id+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestComposePromptGolden pins criterion 1 (F-0152): the composed prompt is
// exactly intent + template body + live board snapshot, deterministic. The
// lint count is computed with the same parameters the composer uses, so the
// golden stays exact without pinning rule evolution.
func TestComposePromptGolden(t *testing.T) {
	repoDir, projDir := promptFixture(t)
	findings, err := lint.RunConfig(repoDir, 0, 0)
	if err != nil {
		t.Fatalf("lint baseline: %v", err)
	}

	got, err := Compose(repoDir, projDir, "investigacao-de-erro", "Investigar lentidão nos approves da UI.")
	if err != nil {
		t.Fatalf("ComposePrompt: %v", err)
	}
	want := fmt.Sprintf(`## Intenção

Investigar lentidão nos approves da UI.

## Orientações — Investigação de erro

Use a skill observability. Comece por /api/debug/timings.

## Estado do board (demo)

Works ativos:
- W-0001 [agent] Trabalho em curso (refs: F-0001)

Features vivas:
- F-0001 candidate — Feature viva

Débitos de lint: %d

## Registro

Nenhum work registrado para este prompt — ao começar, rode 'cerne work start "<descrição>" --ref <F-...>' e feche com 'cerne work done <W-...>'.
`, len(findings))
	if got != want {
		t.Fatalf("composed prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if strings.Contains(got, "F-0002") || strings.Contains(got, "W-0002") {
		t.Fatal("done entities leaked into the live snapshot")
	}
}

// TestComposePromptUnknownType pins criterion 2: unknown type fails listing
// the available ones.
func TestComposePromptUnknownType(t *testing.T) {
	repoDir, projDir := promptFixture(t)
	_, err := Compose(repoDir, projDir, "nao-existe", "x")
	if !errors.Is(err, ErrUnknownType) {
		t.Fatalf("err = %v, want ErrUnknownType", err)
	}
	if !strings.Contains(err.Error(), "investigacao-de-erro") {
		t.Fatalf("error must list available types, got: %v", err)
	}
}

// TestListTypes pins criterion 4: live prompt-template notes become
// types (slug + title); superseded ones don't.
func TestListTypes(t *testing.T) {
	repoDir, _ := promptFixture(t)
	types, err := ListTypes(repoDir)
	if err != nil {
		t.Fatalf("ListTypes: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("types = %+v, want exactly the live template", types)
	}
	if types[0].Slug != "investigacao-de-erro" || types[0].Title != "Investigação de erro" {
		t.Fatalf("type = %+v", types[0])
	}
}

// promptGitFixture is promptFixture on a real git backend (origin + clone),
// as StartWork commits and pushes.
func promptGitFixture(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(root, "repo")
	if err := gitback.Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := store.InitLayout(repoDir, store.Project{Name: "Projeto Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	saveTemplateNote(t, repoDir, "N-0001", "Investigação de erro", domain.NoteActive,
		"Use a skill observability.")
	sha, err := gitback.CommitAndPush(repoDir, "seed templates")
	if err != nil {
		t.Fatalf("seed CommitAndPush: %v", err)
	}
	projDir = filepath.Join(root, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: origin}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin, Snapshot: sha}); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	return repoDir, projDir
}

// TestComposeRegister pins criterion 3 (F-0152): --register creates the
// W- (title derived from the intent) and embeds its id + closing instruction.
func TestComposeRegister(t *testing.T) {
	repoDir, projDir := promptGitFixture(t)
	intent := "Investigar lentidão nos approves da UI do hub."
	prompt, workID, err := ComposeRegister(repoDir, projDir, "investigacao-de-erro", intent)
	if err != nil {
		t.Fatalf("ComposeRegister: %v", err)
	}
	if workID == "" || !strings.HasPrefix(workID, "W-") {
		t.Fatalf("workID = %q, want a W- id", workID)
	}
	for _, want := range []string{
		"Trabalho já registrado para este prompt: " + workID,
		"cerne work done " + workID,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var created *store.Work
	for i, w := range works {
		if w.ID == workID {
			created = &works[i]
		}
	}
	if created == nil || created.State != domain.WorkActive {
		t.Fatalf("work %s not active in backend: %+v", workID, works)
	}
	if !strings.Contains(created.Title, "Investigar lentidão") {
		t.Fatalf("work title not derived from intent: %q", created.Title)
	}
}

// TestComposeRegisterDupGuard: registering the same intent twice trips
// the F-0147 guard instead of minting a duplicate W-.
func TestComposeRegisterDupGuard(t *testing.T) {
	repoDir, projDir := promptGitFixture(t)
	intent := "Mesmo trabalho duas vezes."
	if _, _, err := ComposeRegister(repoDir, projDir, "investigacao-de-erro", intent); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, _, err := ComposeRegister(repoDir, projDir, "investigacao-de-erro", intent)
	if err == nil {
		t.Fatal("second register with the same intent must trip the dup-guard")
	}
}

// TestDeriveWorkTitleTruncates: long intents become a bounded title.
func TestDeriveWorkTitleTruncates(t *testing.T) {
	long := strings.Repeat("palavra ", 20)
	got := deriveWorkTitle(long)
	if len([]rune(got)) > 72 {
		t.Fatalf("title too long (%d runes): %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated title must end with ellipsis: %q", got)
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
)

// turnContextFixture seeds a backend with a feature carrying a real spec, an
// active and a superseded decision, a convention note and a plain note, then
// builds the FTS index over it — the shape BuildTurnContext reads (F-0253).
func turnContextFixture(t *testing.T) (repoDir, dbPath string) {
	t.Helper()
	repoDir, _, _ = newFeatureBackend(t)

	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Conciliação bancária", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	specDir := filepath.Join(repoDir, "content", "features", "F-0001")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	spec := "## Problema\nO extrato do banco chega em OFX e ninguém concilia os lançamentos manualmente sem erro.\n" +
		strings.Repeat("Detalhe irrelevante da conciliação. ", 40)
	if err := os.WriteFile(filepath.Join(specDir, "spec.md"), []byte(spec), 0o644); err != nil {
		t.Fatalf("WriteFile spec: %v", err)
	}
	for _, d := range []store.Decision{
		{ID: "D-0001", Title: "Backend git é a fonte da verdade", State: domain.DecisionActive},
		{ID: "D-0002", Title: "Decisão antiga", State: domain.DecisionSuperseded, SupersededBy: "D-0001"},
	} {
		if err := store.SaveDecision(repoDir, d); err != nil {
			t.Fatalf("SaveDecision: %v", err)
		}
	}
	for _, n := range []store.Note{
		{ID: "N-0001", Title: "Specs usam EARS nos critérios", State: domain.NoteActive, Components: []string{"convention", "platform"}},
		{ID: "N-0002", Title: "Gotcha do go-git no SSH", State: domain.NoteActive, Components: []string{"gitback"}},
	} {
		if err := store.SaveNote(repoDir, n); err != nil {
			t.Fatalf("SaveNote: %v", err)
		}
	}
	commitBackend(t, repoDir)

	dbPath = filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	return repoDir, dbPath
}

// TestBuildTurnContextRetrieves (criterion 1): the message's terms pull the
// matching document into the block, labelled with entity id and state.
func TestBuildTurnContextRetrieves(t *testing.T) {
	repoDir, dbPath := turnContextFixture(t)

	got, err := BuildTurnContext(repoDir, dbPath, "como funciona a conciliação do extrato OFX?", 6000)
	if err != nil {
		t.Fatalf("BuildTurnContext: %v", err)
	}
	if !strings.Contains(got, "F-0001") {
		t.Fatalf("context missing the relevant feature:\n%s", got)
	}
	if !strings.Contains(got, "[implemented]") {
		t.Fatalf("context missing the entity state label:\n%s", got)
	}
	if !strings.Contains(got, "OFX") {
		t.Fatalf("context missing the retrieved snippet text:\n%s", got)
	}
}

// TestBuildTurnContextBaseline (criterion 2): active decisions and convention
// notes are always injected — here the message matches nothing in the index.
func TestBuildTurnContextBaseline(t *testing.T) {
	repoDir, dbPath := turnContextFixture(t)

	got, err := BuildTurnContext(repoDir, dbPath, "xyzzy plugh", 6000)
	if err != nil {
		t.Fatalf("BuildTurnContext: %v", err)
	}
	for _, want := range []string{"D-0001", "Backend git é a fonte da verdade", "N-0001", "Specs usam EARS"} {
		if !strings.Contains(got, want) {
			t.Fatalf("context missing always-on baseline %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "D-0002") {
		t.Fatalf("superseded decision must not be injected as current:\n%s", got)
	}
	if strings.Contains(got, "N-0002") {
		t.Fatalf("note without the convention component must not be injected:\n%s", got)
	}
}

// TestBuildTurnContextEmpty (criterion 4): nothing to inject → no block at
// all, and no error — an empty section would only waste prompt.
func TestBuildTurnContextEmpty(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	commitBackend(t, repoDir)
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}

	got, err := BuildTurnContext(repoDir, dbPath, "qualquer coisa", 6000)
	if err != nil {
		t.Fatalf("BuildTurnContext: %v", err)
	}
	if got != "" {
		t.Fatalf("context should be empty, got:\n%s", got)
	}
}

// TestBuildTurnContextBudget (criterion 5): a small budget truncates each
// retrieved snippet instead of blowing the prompt.
func TestBuildTurnContextBudget(t *testing.T) {
	repoDir, dbPath := turnContextFixture(t)

	const budget = 120
	got, err := BuildTurnContext(repoDir, dbPath, "conciliação extrato OFX lançamentos", budget)
	if err != nil {
		t.Fatalf("BuildTurnContext: %v", err)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("small budget should have truncated a snippet:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- F-") && len([]rune(line)) > budget {
			t.Fatalf("snippet line exceeds the budget (%d runes):\n%s", len([]rune(line)), line)
		}
	}
}

// TestBuildTurnContextDisabled: a non-positive budget turns the injection off
// (the [agent] retrieval_budget escape hatch).
func TestBuildTurnContextDisabled(t *testing.T) {
	repoDir, dbPath := turnContextFixture(t)

	got, err := BuildTurnContext(repoDir, dbPath, "conciliação", -1)
	if err != nil {
		t.Fatalf("BuildTurnContext: %v", err)
	}
	if got != "" {
		t.Fatalf("negative budget should disable the injection, got:\n%s", got)
	}
}

// TestBuildTurnContextBrokenIndex: a missing index costs the priming, never
// the turn — the baseline still travels and no error surfaces.
func TestBuildTurnContextBrokenIndex(t *testing.T) {
	repoDir, _ := turnContextFixture(t)

	got, err := BuildTurnContext(repoDir, filepath.Join(t.TempDir(), "absent.db"), "conciliação", 6000)
	if err != nil {
		t.Fatalf("BuildTurnContext: %v", err)
	}
	if !strings.Contains(got, "D-0001") {
		t.Fatalf("baseline should survive a broken index:\n%s", got)
	}
}

// TestBuildTurnContextExcludesIdeas (F-0312 crit. 9): the automatic priming
// never carries ideas, even one that matches the message word for word — the
// corpus is read on demand only (cerne idea list --search).
func TestBuildTurnContextExcludesIdeas(t *testing.T) {
	repoDir, dbPath := turnContextFixture(t)
	if err := store.SaveIdea(repoDir, store.Idea{ID: "I-0001", Text: "conciliação via planilha zebra", State: domain.IdeaActive}); err != nil {
		t.Fatalf("SaveIdea: %v", err)
	}
	commitBackend(t, repoDir)
	if err := index.Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	got, err := BuildTurnContext(repoDir, dbPath, "o que temos sobre zebra e conciliação?", 6000)
	if err != nil {
		t.Fatalf("BuildTurnContext: %v", err)
	}
	if !strings.Contains(got, "F-0001") {
		t.Fatalf("the priming lost the matching feature (fixture broken):\n%s", got)
	}
	if strings.Contains(got, "I-0001") || strings.Contains(got, "planilha zebra") {
		t.Fatalf("the priming carries an idea:\n%s", got)
	}
}

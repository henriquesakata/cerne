package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// recommendedSpecBody has the recommended option in the middle, so display
// order (recommended first) differs from the written one.
func recommendedSpecBody() string {
	return completeSpec("# Feature\n\n## Problema\n\nContexto real.\n\n" +
		"[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona (recomendada) | hibrida]\n\n" +
		"## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n")
}

// F-0517 crit. 5
func TestClarifyListMarksRecommended(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com recomendada", recommendedSpecBody())

	out, err := execCerne(t, "clarify", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "options: 1: assincrona (recommended), 2: sincrona, 3: hibrida") {
		t.Errorf("list does not put the recommended option first and mark it:\n%s", out)
	}

	out, err = execCerne(t, "clarify", "list", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify list --json: %v\n%s", err, out)
	}
	var items []app.OpenClarification
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("list --json: %v\n%s", err, out)
	}
	if len(items) != 1 || items[0].Recommended != 1 || items[0].Options[0] != "assincrona" {
		t.Fatalf("list --json = %+v, want the recommended index and the option first, suffix stripped", items)
	}
}

// F-0517 crit. 5
func TestPickFollowsListOrder(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com recomendada", recommendedSpecBody())

	out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--pick", "2", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify answer --pick 2: %v\n%s", err, out)
	}
	if spec := readBackendSpec(t, repoDir); !strings.Contains(spec, "→ sincrona]") {
		t.Errorf("--pick 2 is the second option of the list (sincrona), spec:\n%s", spec)
	}
}

// F-0517 crit. 3
func TestAnswerPickRecommendedStripsSuffix(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com recomendada", recommendedSpecBody())

	out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--pick", "1", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify answer --pick 1: %v\n%s", err, out)
	}
	spec := readBackendSpec(t, repoDir)
	if !strings.Contains(spec, "[ESCLARECIDO: sincrona ou assincrona? → assincrona]") {
		t.Errorf("picking the recommended option did not store it without the suffix:\n%s", spec)
	}
	if strings.Contains(spec, "(recomendada)") {
		t.Errorf("suffix leaked into the answer:\n%s", spec)
	}
}

// F-0517 crit. 5: the approve refusal numbers the options the same way and
// marks the recommended one.
func TestApproveRefusalMarksRecommended(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com recomendada", recommendedSpecBody())
	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("approve succeeded over an open clarification:\n%s", out)
	}
	if !strings.Contains(err.Error(), "1: assincrona (recommended), 2: sincrona, 3: hibrida") {
		t.Errorf("refusal lacks the numbered list with the mark:\n%v", err)
	}
}

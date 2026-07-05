package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
)

func TestAskDelegationPrintsPrompt(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	out, err := execCerne(t, "ask", "o que é boletos?", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask: %v\n%s", err, out)
	}
	// Delegation prints the assembled prompt for the host agent, not an answer.
	if !strings.Contains(out, "Pergunta: o que é boletos?") {
		t.Fatalf("ask output missing the assembled question:\n%s", out)
	}
	if !strings.Contains(out, "Respeite o estado de cada fonte") {
		t.Fatalf("ask output missing the state-respect instruction:\n%s", out)
	}
}

func TestAskEmptyQuestion(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "ask", "   ", "--dir", projDir); err == nil {
		t.Fatalf("ask with blank question succeeded, want error")
	}
}

// TestAskNoPreamble: --no-preamble drops the instruction scaffold from the
// assembled prompt (token-lean), keeping the question and the sources.
func TestAskNoPreamble(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if _, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	out, err := execCerne(t, "ask", "o que é boletos?", "--no-preamble", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask --no-preamble: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Pergunta: o que é boletos?") {
		t.Fatalf("ask --no-preamble dropped the question:\n%s", out)
	}
	if strings.Contains(out, "Responda em pt-BR") || strings.Contains(out, "Respeite o estado") {
		t.Fatalf("ask --no-preamble must omit the instruction preamble:\n%s", out)
	}
}

// TestAskJSON: --json emits the retrieved sources as a parseable object and no
// human chatter (no "Construindo índice..."), even when the index is built on
// the first run.
func TestAskJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas mensais sem juros."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	out, err := execCerne(t, "ask", "parcelas", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask --json: %v\n%s", err, out)
	}
	var data struct {
		Sources []struct {
			Source string `json:"source"`
			Kind   string `json:"kind"`
			State  string `json:"state"`
			Text   string `json:"text"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("ask --json is not valid JSON: %v\n%s", err, out)
	}
	var found bool
	for _, s := range data.Sources {
		if s.Source == "F-0001" && s.Kind == "feature" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ask --json missing the F-0001 feature source: %+v", data.Sources)
	}
}

// TestAskMaxSources: --max-sources caps how many sources land in the prompt.
func TestAskMaxSources(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	// Create both features first: creation starts from a clean HEAD (F-0121),
	// so an uncommitted doc write would not survive the next creation.
	ids := make([]string, 0, 2)
	for i, title := range []string{"Parcelamento mensal", "Parcelamento anual"} {
		ids = append(ids, newFeature(t, projDir, title, i+1))
	}
	for _, id := range ids {
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	}
	if _, err := gitback.CommitAndPush(repoDir, "specs"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	full, err := execCerne(t, "ask", "parcelas", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask: %v\n%s", err, full)
	}
	if got := strings.Count(full, "(feature,"); got < 2 {
		t.Fatalf("expected >=2 feature sources by default, got %d:\n%s", got, full)
	}
	capped, err := execCerne(t, "ask", "parcelas", "--max-sources", "1", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask --max-sources: %v\n%s", err, capped)
	}
	if got := strings.Count(capped, "(feature,"); got != 1 {
		t.Fatalf("--max-sources 1 must yield exactly 1 feature source, got %d:\n%s", got, capped)
	}
}

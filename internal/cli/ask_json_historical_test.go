package cli

import (
	"encoding/json"
	"testing"
)

// TestAskJSONHistorical: an implemented feature's prose reaches --json flagged
// historical:true, so machine consumers can tell present from past.
func TestAskJSONHistorical(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas mensais."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar parcelas."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err != nil {
		t.Fatalf("start: %v", err)
	}
	writeDoc(t, repoDir, id, "result.md", realDoc(id, "Parcelas entregues conforme o plano."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
		t.Fatalf("complete: %v", err)
	}

	out, err := execCerne(t, "ask", "parcelas", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("ask --json: %v\n%s", err, out)
	}
	var data struct {
		Sources []struct {
			Source     string `json:"source"`
			State      string `json:"state"`
			Historical bool   `json:"historical"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("ask --json is not valid JSON: %v\n%s", err, out)
	}
	var found bool
	for _, s := range data.Sources {
		if s.Source == id && s.State == "implemented" {
			found = true
			if !s.Historical {
				t.Fatalf("implemented source not flagged historical: %+v", s)
			}
		}
	}
	if !found {
		t.Fatalf("ask --json missing the implemented %s source: %+v", id, data.Sources)
	}
}

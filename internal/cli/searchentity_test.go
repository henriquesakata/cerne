package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// perDocFixture: one feature with spec, plan and result matching "parcelas",
// another with a spec only.
func perDocFixture(t *testing.T) string {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	newFeature(t, projDir, "Parcelamento", 1)
	newFeature(t, projDir, "Cartão", 2)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Dividir compras em parcelas mensais sem juros."))
	writeDoc(t, repoDir, "F-0001", "plan.md", realDoc("F-0001", "Calcular as parcelas no checkout."))
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", "As parcelas saem no extrato."))
	writeDoc(t, repoDir, "F-0002", "spec.md", realDoc("F-0002", "Fatura do cartão com parcelas de outras compras."))
	return projDir
}

// F-0367 criterion 2: --per-doc prints one line per document, byte for byte
// the output `cerne search` printed before F-0367 (golden captured then).
func TestSearchPerDocGolden(t *testing.T) {
	out, err := execCerne(t, "search", "parcelas", "--per-doc", "--dir", perDocFixture(t))
	if err != nil {
		t.Fatalf("search --per-doc: %v\n%s", err, out)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "search_per_doc.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if out != string(want) {
		t.Fatalf("--per-doc output differs from the golden:\n%s\nwant:\n%s", out, want)
	}
}

// F-0367 criterion 1 (text): one line per entity with the documents that matched.
func TestSearchOneLinePerEntity(t *testing.T) {
	out, err := execCerne(t, "search", "parcelas", "--dir", perDocFixture(t))
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	lines := resultLines(out)
	if len(lines) != 2 {
		t.Fatalf("result lines = %d, want 2 (one per entity):\n%s", len(lines), out)
	}
	if !strings.Contains(out, "F-0001 [candidate] Parcelamento — spec, plan, result\n") ||
		!strings.Contains(out, "F-0002 [candidate] Cartão — spec\n") {
		t.Fatalf("missing entity lines:\n%s", out)
	}
	if strings.Count(out, "«parcelas»") != 2 {
		t.Fatalf("want one snippet per entity:\n%s", out)
	}
}

// F-0367 criterion 1 (JSON): each result gains docs; the fields of today stay.
func TestSearchJSONCarriesDocs(t *testing.T) {
	out, err := execCerne(t, "search", "parcelas", "--json", "--dir", perDocFixture(t))
	if err != nil {
		t.Fatalf("search --json: %v\n%s", err, out)
	}
	var data struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(data.Results) != 2 {
		t.Fatalf("results = %d, want 2:\n%s", len(data.Results), out)
	}
	r := data.Results[0]
	for _, k := range []string{"entity_id", "kind", "doc", "state", "title", "snippet"} {
		if _, ok := r[k]; !ok {
			t.Fatalf("result lost field %q:\n%s", k, out)
		}
	}
	if !reflect.DeepEqual(r["docs"], []any{"spec", "plan", "result"}) {
		t.Fatalf("docs = %v, want [spec plan result]", r["docs"])
	}
}

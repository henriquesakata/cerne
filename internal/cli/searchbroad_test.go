package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

const broadLine = "No document has all the terms; showing those that have any."

// broadFixture: doc A has "troca de conta e chave", doc B has
// "criptografia do switcher dual"; no document has every term of broadQuery.
func broadFixture(t *testing.T) string {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	newFeature(t, projDir, "Documento A", 1)
	newFeature(t, projDir, "Documento B", 2)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "troca de conta e chave"))
	writeDoc(t, repoDir, "F-0002", "spec.md", realDoc("F-0002", "criptografia do switcher dual"))
	return projDir
}

const broadQuery = "troca conta chave criptografia switcher dual"

// Criteria 2 and 8: broadened text output starts with the notice and lists A and B.
func TestSearchBroadenedText(t *testing.T) {
	projDir := broadFixture(t)
	out, err := execCerne(t, "search", broadQuery, "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	// "Building index..." precedes on the first run; the notice leads the results.
	if !strings.Contains(out, broadLine+"\n") {
		t.Fatalf("missing broadened notice:\n%s", out)
	}
	lines := resultLines(out)
	if len(lines) != 2 || !strings.Contains(out, "F-0001 [feature/spec]") || !strings.Contains(out, "F-0002 [feature/spec]") {
		t.Fatalf("want hits for F-0001 and F-0002, got:\n%s", out)
	}
	if strings.Index(out, broadLine) > strings.Index(out, lines[0]) {
		t.Fatalf("notice must come before the first hit:\n%s", out)
	}
}

// Criterion 2 (JSON): "broadened": true; criterion 3: field absent on an exact hit.
func TestSearchBroadenedJSON(t *testing.T) {
	projDir := broadFixture(t)
	out, err := execCerne(t, "search", broadQuery, "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("search --json: %v\n%s", err, out)
	}
	var data struct {
		Results []struct {
			EntityID string `json:"entity_id"`
		} `json:"results"`
		Broadened bool `json:"broadened"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !data.Broadened || len(data.Results) != 2 {
		t.Fatalf("broadened=%v results=%d, want true and 2:\n%s", data.Broadened, len(data.Results), out)
	}

	out, err = execCerne(t, "search", "troca conta chave", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("search --json exact: %v\n%s", err, out)
	}
	if strings.Contains(out, "broadened") {
		t.Fatalf("exact hit must not carry broadened:\n%s", out)
	}
}

// Criterion 3 (text): exact hit prints no notice.
func TestSearchExactHasNoBroadenedNotice(t *testing.T) {
	projDir := broadFixture(t)
	out, err := execCerne(t, "search", "troca conta chave", "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	if strings.Contains(out, broadLine) || len(resultLines(out)) != 1 {
		t.Fatalf("exact search must not broaden:\n%s", out)
	}
}

// Criteria 4 and 5: FTS5 syntax is not broadened; nothing even broadened -> "Nothing found".
func TestSearchBroadenedNegatives(t *testing.T) {
	projDir := broadFixture(t)
	out, err := execCerne(t, "search", `"troca criptografia"`, "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	if strings.Contains(out, broadLine) || !strings.Contains(out, `Nothing found for ""troca criptografia"".`) {
		t.Fatalf("syntax query must not broaden:\n%s", out)
	}
	out, err = execCerne(t, "search", "xxxx yyyy", "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}
	if strings.Contains(out, broadLine) || !strings.Contains(out, `Nothing found for "xxxx yyyy".`) {
		t.Fatalf("hopeless query: want plain Nothing found:\n%s", out)
	}
}

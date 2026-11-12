package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0351 crit. 1-5: after `runbook merge` the errata block of show says where
// the merged item went instead of repeating it; the open one stays whole; the
// errata file is not touched.
func TestShowRunbookMergedErrataIsOneLine(t *testing.T) {
	projDir, repoDir := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Decidida e com passo.", "--altera", "S6.6")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Ainda proposta.", "--altera", "S6.7")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--quote", "vale")
	rb(t, projDir, "runbook", "merge", "RB-0001")
	errFile := filepath.Join(repoDir, "content", "errata", "RB-0001.md")
	before, err := os.ReadFile(errFile)
	if err != nil || len(before) == 0 {
		t.Fatalf("errata file unreadable or empty: %v", err)
	}

	day := time.Now().UTC().Format("2006-01-02")
	line := "E1 merged into revision 2 (" + day + ")"
	out := rb(t, projDir, "show", "RB-0001")
	if !strings.Contains(out, line) || strings.Count(out, "Decidida e com passo.") != 1 || !strings.Contains(out, "Ainda proposta.") {
		t.Fatalf("crit 1+2: merged one line, open item whole, body text once:\n%s", out)
	}
	var js struct {
		Content []struct{ Name, Body string }
		Merged  []struct {
			Item     string
			Revision int
			Date     string
		} `json:"merged_errata"`
	}
	if err := json.Unmarshal([]byte(rb(t, projDir, "show", "RB-0001", "--json")), &js); err != nil || len(js.Merged) != 1 ||
		js.Merged[0].Item != "E1" || js.Merged[0].Revision != 2 || js.Merged[0].Date != day {
		t.Fatalf("crit 4 merged_errata = %+v (err %v)", js.Merged, err)
	}
	found := false
	for _, d := range js.Content {
		if d.Name != "errata" {
			continue
		}
		found = true
		if strings.Contains(d.Body, "Decidida e com passo.") || !strings.Contains(d.Body, line) || !strings.Contains(d.Body, "Ainda proposta.") {
			t.Fatalf("crit 4 content errata:\n%s", d.Body)
		}
	}
	if !found {
		t.Fatalf("crit 4: json content has no errata doc: %+v", js.Content)
	}
	// crit 5: show never writes.
	after, err := os.ReadFile(errFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("crit 5: show rewrote the errata file")
	}

	// crit 3: everything merged -> only the lines.
	rb(t, projDir, "runbook", "decide", "RB-0001", "E2", "--by", "dono", "--quote", "vale")
	rb(t, projDir, "runbook", "merge", "RB-0001")
	want := "E1 merged into revision 2 (" + day + `) under S6.6 — decided by dono: "vale"` + "\n\n" +
		"E2 merged into revision 3 (" + day + `) under S6.7 — decided by dono: "vale"` + "\n\n" +
		"earlier body: 'cerne show RB-0001 --rev 1'"
	all := rb(t, projDir, "show", "RB-0001")
	_, block, _ := strings.Cut(all, "## errata\n")
	if got := strings.TrimSpace(block); got != want {
		t.Fatalf("crit 3: errata block = %q, want only the lines %q", got, want)
	}
}

// F-0351: a feature's errata is shown exactly as stored, even when a block
// carries a `Fundida:` line (only runbooks have merged items).
func TestShowFeatureErrataUnchanged(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	rb(t, projDir, "feature", "new", "Boletos", "--context", "ctx")
	if err := store.AppendErrata(repoDir, "F-0001", "Fundida: revisão 1\n\nTexto da emenda.", "agent", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if out := rb(t, projDir, "show", "F-0001", "--all"); !strings.Contains(out, "Fundida: revisão 1") || strings.Contains(out, "merged into revision") {
		t.Fatalf("feature errata must be verbatim:\n%s", out)
	}
}

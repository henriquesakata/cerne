package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// F-0351 rodada 2, J1: the merged item's line says where it went, who decided and
// why; the way back to the earlier body is said once, at the end of the block.
func TestShowMergedErrataLineIsComplete(t *testing.T) {
	projDir, _ := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Um.", "--altera", "S6.6")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Dois.", "--altera", "S6.7")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "tl", "--quote", "so com alerta firing")
	rb(t, projDir, "runbook", "merge", "RB-0001")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E2", "--by", "dono", "--quote", "vale")
	rb(t, projDir, "runbook", "merge", "RB-0001")
	day := time.Now().UTC().Format("2006-01-02")

	text := rb(t, projDir, "show", "RB-0001")
	_, block, _ := strings.Cut(text, "## errata\n")
	want := "E1 merged into revision 2 (" + day + `) under S6.6 — decided by tl: "so com alerta firing"` + "\n\n" +
		"E2 merged into revision 3 (" + day + `) under S6.7 — decided by dono: "vale"` + "\n\n" +
		"earlier body: 'cerne show RB-0001 --rev 1'"
	if got := strings.TrimSpace(block); got != want {
		t.Fatalf("errata block:\n%s\nwant:\n%s", got, want)
	}

	var js struct {
		Merged  []map[string]any `json:"merged_errata"`
		Earlier int              `json:"earlier_body_rev"`
	}
	if err := json.Unmarshal([]byte(rb(t, projDir, "show", "RB-0001", "--json")), &js); err != nil {
		t.Fatal(err)
	}
	m := js.Merged[0]
	if m["step"] != "S6.6" || m["decided_by"] != "tl" || m["quote"] != "so com alerta firing" || js.Earlier != 1 {
		t.Fatalf("json: %v earlier=%d", js.Merged, js.Earlier)
	}
}

// J1: unknown parts leave with their separator.
func TestShowMergedErrataLineDropsUnknownParts(t *testing.T) {
	projDir, repoDir := runbookFixture(t)
	seedMergedItem(t, repoDir, "Altera: -\nFundida: revisão 4", time.Now().UTC(), "", "", time.Time{})
	text := rb(t, projDir, "show", "RB-0001")
	if !strings.Contains(text, "E1 merged into revision 4\n") || strings.Contains(text, " under ") || strings.Contains(text, "decided by") {
		t.Fatalf("bare line expected:\n%s", text)
	}
	if !strings.Contains(text, "earlier body: 'cerne show RB-0001 --rev 3'") {
		t.Fatalf("earlier body line:\n%s", text)
	}
}

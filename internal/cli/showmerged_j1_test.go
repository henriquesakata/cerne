package cli

import (
	"encoding/json"
	"regexp"
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
	want := "E1 merged into revision 2 (" + day + `) — shown in the body under S6.6` + "\n\n" +
		"E2 merged into revision 3 (" + day + `) — shown in the body under S6.7` + "\n\n" +
		"earlier body: 'cerne show RB-0001 --rev 1'"
	if got := strings.TrimSpace(block); got != want {
		t.Fatalf("errata block:\n%s\nwant:\n%s", got, want)
	}

	// A7: the text no longer repeats who decided and why; the body shows it, once.
	if n := strings.Count(text, `"so com alerta firing" — tl`); n != 1 || strings.Count(text, "so com alerta firing") != 1 {
		t.Fatalf("decision must appear exactly once in the text (got %d):\n%s", n, text)
	}
	// A6: the printed pointer is a command that works and returns the body before the merge.
	m := regexp.MustCompile(`earlier body: 'cerne (show [^']+)'`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no earlier body pointer:\n%s", text)
	}
	old, err := execCerne(t, append(strings.Fields(m[1]), "--dir", projDir)...)
	if err != nil || !strings.Contains(old, "Para no primeiro alerta.") || strings.Contains(old, "Errata E1") {
		t.Fatalf("pointer %q: err=%v, want the body before the merge:\n%s", m[1], err, old)
	}

	var js struct {
		Merged  []map[string]any `json:"merged_errata"`
		Earlier int              `json:"earlier_body_rev"`
	}
	if err := json.Unmarshal([]byte(rb(t, projDir, "show", "RB-0001", "--json")), &js); err != nil {
		t.Fatal(err)
	}
	jm := js.Merged[0]
	if jm["step"] != "S6.6" || jm["decided_by"] != "tl" || jm["quote"] != "so com alerta firing" || js.Earlier != 1 {
		t.Fatalf("json: %v earlier=%d", js.Merged, js.Earlier)
	}
}

// J1: unknown parts leave with their separator.
func TestShowMergedErrataLineDropsUnknownParts(t *testing.T) {
	projDir, repoDir := runbookFixture(t)
	seedMergedItem(t, repoDir, "Altera: -\nFundida: revisão 4", time.Now().UTC(), "", "", time.Time{})
	text := rb(t, projDir, "show", "RB-0001")
	if !strings.Contains(text, "E1 merged into revision 4 — shown in the body\n") || strings.Contains(text, " under ") || strings.Contains(text, "decided by") {
		t.Fatalf("bare line expected:\n%s", text)
	}
	if !strings.Contains(text, "earlier body: 'cerne show RB-0001 --rev 3'") {
		t.Fatalf("earlier body line:\n%s", text)
	}
}

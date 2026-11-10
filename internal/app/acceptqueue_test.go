package app

import (
	"strings"
	"testing"
	"time"
)

// debts runs summarizeResult over a result whose pendings section is body.
func debts(t *testing.T, head, body string) AcceptRow {
	t.Helper()
	return summarizeResult([]byte("# R\n\n## " + head + "\n\n" + body + "\n"))
}

// F-0887 crit. 1 (P-0018): "Nenhum/Nenhuma/Não há" plus a debt noun declares
// zero only when nothing but a closed qualifier and final punctuation follows;
// any other continuation declares a debt and counts.
func TestNoneCountsOnlyAPureZero(t *testing.T) {
	for _, tc := range []struct {
		item string
		want int
	}{
		{"- Nenhuma limitação conhecida além do rebase manual, ainda aberto.", 1},
		{"- Nenhum problema de performance foi medido.", 1},
		{"- Não há débitos exceto o P-0023.", 1},
		{"- Nenhum teste de carga foi escrito.", 1},
		{"- Nenhum.", 0},
		{"- Nenhum débito aberto.", 0},
		{"- Nenhuma pendência.", 0},
		{"- Não há débitos conhecidos.", 0},
		{"- **Nenhuma pendência registrada.**", 0},
		{"- Nenhum item restante", 0},
		{"- n/a", 0},
		{"- N/A.", 0},
		{"- none", 0}, // mutant C of F-0875 round 3
	} {
		row := debts(t, "Débitos", tc.item)
		if !row.PendingReported || len(row.Pending) != tc.want {
			t.Errorf("%q: reported=%v pending=%q, want %d", tc.item, row.PendingReported, row.Pending, tc.want)
		}
	}
	// A prose "Nenhum." is still an explicit zero.
	if row := debts(t, "Débitos", "Nenhum."); !row.PendingReported || len(row.Pending) != 0 {
		t.Errorf("prose Nenhum.: %+v", row)
	}
}

// F-0887 crit. 2 (P-0019): a fence opens with three or more backticks or
// tildes and closes only with the same character, at least as long; what is
// inside is code, never evidence.
func TestTildeFenceIsCode(t *testing.T) {
	for name, doc := range map[string]string{
		"tilde":            "~~~\nProdução: x\n~~~\n",
		"backticks inside": "~~~\n```\nProdução: x\n```\n~~~\n",
		"longer opener":    "~~~~\n~~~\nProdução: x\n~~~~\n",
		"tildes inside":    "````\n~~~\nProdução: x\n````\n",
	} {
		if row := summarizeResult([]byte("# R\n\n" + doc)); row.Production != NotInformed {
			t.Errorf("%s: production = %q, want %s", name, row.Production, NotInformed)
		}
	}
	// After the fence closes, a production line counts again.
	if row := summarizeResult([]byte("# R\n\n~~~\ncódigo\n~~~\n\nProdução: no ar desde 2026-11-10\n")); !strings.Contains(row.Production, "no ar") {
		t.Errorf("a line after the fence must count: %q", row.Production)
	}
}

// F-0887 crit. 3 (P-0019): Follow-ups and Pendências sections (level 2 or
// more) are read as pendings, with the Débitos rules.
func TestFollowUpsAndPendenciasSectionsCount(t *testing.T) {
	for _, head := range []string{"Follow-ups", "Follow-up", "Followups", "Pendências", "Pendencias", "Pendências e riscos"} {
		if row := debts(t, head, "- falta X"); !row.PendingReported || len(row.Pending) != 1 || row.Pending[0] != "falta X" {
			t.Errorf("## %s: %+v, want pending 1 (falta X)", head, row)
		}
	}
	if row := summarizeResult([]byte("# R\n\n### Pendências\n\n- falta Y\n")); len(row.Pending) != 1 {
		t.Errorf("### Pendências: %+v, want pending 1", row)
	}
	if row := debts(t, "Pendências", "- Nenhuma."); !row.PendingReported || len(row.Pending) != 0 {
		t.Errorf("## Pendências with Nenhuma.: %+v, want 0", row)
	}
	if row := debts(t, "Notas", "- falta X"); row.PendingReported {
		t.Errorf("any other section is not a pendings section: %+v", row)
	}
}

// F-0887 crit. 4 (P-0019): same ImplementedAt, input in inverted order — the
// output comes by id. The store hands features sorted by id, so the inverted
// input exists only below it; the age still decides first.
func TestAcceptQueueTiesById(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	rows := []AcceptRow{{ID: "F-0903", ImplementedAt: at}, {ID: "F-0902", ImplementedAt: at}, {ID: "F-0901", ImplementedAt: at.Add(time.Hour)}, {ID: "F-0900", ImplementedAt: at}}
	sortAcceptRows(rows)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "F-0900,F-0902,F-0903,F-0901" {
		t.Fatalf("order = %s, want F-0900,F-0902,F-0903,F-0901", got)
	}
}

package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestHookCitations pins the closed grammar of criterion 1: the positives and,
// as important, the sentences that look like a citation and are not.
func TestHookCitations(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []HookCitation
	}{
		{"regra K da", "ver regra 15 da N-0600", []HookCitation{{Note: "N-0600", Rules: []int{15}, Line: 1}}},
		{"regra K de", "regra 3 de N-0600", []HookCitation{{Note: "N-0600", Rules: []int{3}, Line: 1}}},
		{"regras K, L e M da", "as regras 2, 4 e 9 da N-0600", []HookCitation{{Note: "N-0600", Rules: []int{2, 4, 9}, Line: 1}}},
		{"N regra K", "N-0600 regra 15", []HookCitation{{Note: "N-0600", Rules: []int{15}, Line: 1}}},
		{"N, regra K", "(N-0600, regra 15)", []HookCitation{{Note: "N-0600", Rules: []int{15}, Line: 1}}},
		{"rule K of", "rule 7 of N-0600", []HookCitation{{Note: "N-0600", Rules: []int{7}, Line: 1}}},
		{"N rule K", "N-0600 rule 7", []HookCitation{{Note: "N-0600", Rules: []int{7}, Line: 1}}},
		{"sem diferenciar maiúsculas", "REGRA 5 DA n-0600", []HookCitation{{Note: "N-0600", Rules: []int{5}, Line: 1}}},
		{"menção solta", "leia a N-0600 antes", []HookCitation{{Note: "N-0600", Line: 1}}},
		{"linha certa", "a\nb\nN-0600 regra 2", []HookCitation{{Note: "N-0600", Rules: []int{2}, Line: 3}}},
		{"duas notas na linha", "regra 1 da N-0001 e N-0002 regra 2", []HookCitation{
			{Note: "N-0001", Rules: []int{1}, Line: 1}, {Note: "N-0002", Rules: []int{2}, Line: 1}}},
		{"forma com regra não vira solta também", "N-0600 regra 15", []HookCitation{{Note: "N-0600", Rules: []int{15}, Line: 1}}},
		// Not citations of a rule: the nearby number is a count, not a rule.
		{"tem 14 regras", "a N-0600 tem 14 regras", []HookCitation{{Note: "N-0600", Line: 1}}},
		{"regras sem número", "N-0600 regras", []HookCitation{{Note: "N-0600", Line: 1}}},
		{"regra K sem nota", "aplique a regra 3", nil},
		{"número de nota sem regra", "veja o N-0600 e o item 4", []HookCitation{{Note: "N-0600", Line: 1}}},
		{"id colado em palavra", "XN-0600 e N-0600x", nil},
		{"vazio", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := HookCitations(c.text)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("HookCitations(%q) = %+v, want %+v", c.text, got, c.want)
			}
		})
	}
}

// TestNoteRuleCount pins criterion 2.
func TestNoteRuleCount(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"lista simples", "1. a\n2. b\n3. c\n", 3},
		{"sem lista", "texto\n- a\n- b\n", 0},
		{"sublista recuada não conta", "1. a\n   9. sub\n2. b\n", 2},
		{"fora de ordem, maior número", "3. c\n1. a\n7. g\n", 7},
		{"número no meio da linha não conta", "veja 15. adiante\n1. a\n", 1},
		{"vazio", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NoteRuleCount(c.body); got != c.want {
				t.Errorf("NoteRuleCount = %d, want %d", got, c.want)
			}
		})
	}
}

func hookRulesBackend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC()
	notes := []struct {
		n    store.Note
		body string
	}{
		{store.Note{ID: "N-0600", Title: "catorze", State: domain.NoteActive}, "1. a\n2. b\n3. c\n4. d\n5. e\n6. f\n7. g\n8. h\n9. i\n10. j\n11. k\n12. l\n13. m\n14. n\n"},
		{store.Note{ID: "N-0601", Title: "velha", State: domain.NoteSuperseded}, "1. a\n"},
		{store.Note{ID: "N-0602", Title: "sem lista", State: domain.NoteActive}, "prosa\n"},
	}
	for _, x := range notes {
		x.n.CreatedAt, x.n.UpdatedAt = now, now
		if err := store.SaveNote(dir, x.n); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "content", "notes", x.n.ID+".md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(x.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestCheckHookCitations pins criteria 3 to 5.
func TestCheckHookCitations(t *testing.T) {
	dir := hookRulesBackend(t)
	hooks := []HookText{
		{Path: "scripts/hooks/x.mjs", Text: "const m = '(N-0600, regra 15)';\n// N-0600 regra 14\n// N-0600 regra 15\n"},
		{Path: ".claude/hooks/y.sh", Text: "# regra 1 da N-0601\n# N-0999 regra 1\n# solta N-0602\n# regra 1 da N-0602\n# solta N-0600\n"},
	}
	got, err := CheckHookCitations(dir, hooks)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ entity, file, detail string }
	var rows []row
	for _, f := range got {
		if f.Rule != RuleHookCitesMissingRule {
			t.Errorf("Rule = %q", f.Rule)
		}
		rows = append(rows, row{f.Entity, f.File, f.Detail})
	}
	wantFiles := []string{
		"scripts/hooks/x.mjs:1", "scripts/hooks/x.mjs:3",
		".claude/hooks/y.sh:1", ".claude/hooks/y.sh:2", ".claude/hooks/y.sh:4",
	}
	if len(rows) != len(wantFiles) {
		t.Fatalf("achados = %+v, want %d", rows, len(wantFiles))
	}
	for i, w := range wantFiles {
		if rows[i].file != w {
			t.Errorf("achado %d File = %q, want %q", i, rows[i].file, w)
		}
	}
	if !strings.Contains(rows[0].detail, "regra 15 da N-0600, que tem 14 regras numeradas") ||
		!strings.Contains(rows[0].detail, "acrescente") {
		t.Errorf("detalhe sem a regra/total/correção: %q", rows[0].detail)
	}
	if rows[0].entity != "N-0600" {
		t.Errorf("Entity = %q", rows[0].entity)
	}
	if !strings.Contains(rows[2].detail, "superseded") {
		t.Errorf("nota não ativa deve nomear o estado: %q", rows[2].detail)
	}
	if !strings.Contains(rows[3].detail, "não existe") {
		t.Errorf("nota ausente deve dizer que não existe: %q", rows[3].detail)
	}
	if !strings.Contains(rows[4].detail, "0 regras") {
		t.Errorf("nota sem lista tem zero regras: %q", rows[4].detail)
	}
}

// TestHookRuleBlocks: the rule fails the lint (not advisory).
func TestHookRuleBlocks(t *testing.T) {
	if Advisory(RuleHookCitesMissingRule) {
		t.Error("hook-cites-missing-rule não pode ser advisory")
	}
	if ruleRank[RuleHookCitesMissingRule] != len(RuleOrder)-1 {
		t.Error("a regra vai no fim do RuleOrder")
	}
}

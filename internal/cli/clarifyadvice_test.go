package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// F-0517 crit. 6
func TestSpecCheckAdvisesOptions(t *testing.T) {
	for _, tt := range []struct {
		lang     doclang.Lang
		marker   string
		question string
		form     string
	}{
		{doclang.PT, "[ESCLARECER: qual formato?]", "qual formato?", "(recomendada)"},
		{doclang.EN, "[CLARIFY: which format?]", "which format?", "(recommended)"},
	} {
		projDir, _, _ := setupProjectIn(t, string(tt.lang))
		var b strings.Builder
		for _, h := range doclang.Sections(doclang.Spec, tt.lang) {
			b.WriteString("## " + h + "\n\ntexto\n\n")
		}
		okBar := doclang.Display(doclang.BarLabel, tt.lang) + ": " + doclang.Display(doclang.NotApplicable, tt.lang) + " (sem comparavel) — padrao alternativo: testes\n"
		p := filepath.Join(t.TempDir(), "spec.md")
		if err := os.WriteFile(p, []byte(b.String()+okBar+tt.marker+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := execCerne(t, "spec", "check", "--file", p, "--dir", projDir)
		if err != nil {
			t.Fatalf("%s: the advice must not change the exit code: %v\n%s", tt.lang, err, out)
		}
		for _, want := range []string{"clarify-without-options", tt.question, tt.form} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: output lacks %q:\n%s", tt.lang, want, out)
			}
		}
		// A marker that already has options draws no advice.
		withOptions := strings.Replace(b.String()+okBar+tt.marker, "?]", "? | a | b]", 1)
		if err := os.WriteFile(p, []byte(withOptions), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err = execCerne(t, "spec", "check", "--file", p, "--dir", projDir)
		if err != nil || strings.Contains(out, "clarify-without-options") {
			t.Errorf("%s: a marker with options drew advice (err %v):\n%s", tt.lang, err, out)
		}
	}
}

// F-0517 crit. 6
func TestApproveCheckAdvisesOptions(t *testing.T) {
	for _, tt := range []struct {
		lang   doclang.Lang
		marker string
		form   string
	}{
		{doclang.PT, "[ESCLARECER: qual formato?]", "(recomendada)"},
		{doclang.EN, "[CLARIFY: which format?]", "(recommended)"},
	} {
		projDir, _, _ := setupProjectIn(t, string(tt.lang))
		newFeatureWithSpec(t, projDir, "Com pergunta aberta",
			"# Feature\n\n## Problema\n\nContexto real.\n\n"+tt.marker+"\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n")
		out, err := execCerne(t, "feature", "approve", "F-0001", "--check", "--dir", projDir)
		// The open question is a (soft) impediment either way; the advice adds none.
		if err == nil || !strings.Contains(err.Error(), "1 (0 hard)") {
			t.Errorf("%s: the check's verdict changed: %v\n%s", tt.lang, err, out)
		}
		for _, want := range []string{"clarify-without-options", tt.form} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: output lacks %q:\n%s", tt.lang, want, out)
			}
		}
	}
}

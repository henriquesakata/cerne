package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
)

func specCheckBody(t *testing.T, bar, extra string) string {
	t.Helper()
	var b strings.Builder
	for _, h := range doclang.Sections(doclang.Spec, doclang.PT) {
		b.WriteString("## " + h + "\n\ntexto\n\n")
	}
	return b.String() + bar + extra
}

// TestSpecCheck_ReportsMissingSectionsAndBar (F-0438 crit. 1).
func TestSpecCheck_ReportsMissingSectionsAndBar(t *testing.T) {
	projDir, _, _ := setupProject(t)
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "spec.md")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	okBar := "Barra: não aplicável (sem comparável) — padrão alternativo: testes\n"

	out, err := execCerne(t, "spec", "check", "--file", write("## Problema\n\ntexto\n"), "--dir", projDir)
	if !errors.Is(err, errSpecCheckFailed) || !strings.Contains(out, "Fora de escopo") || !strings.Contains(out, "bar-missing") {
		t.Fatalf("truncated: err %v, out:\n%s", err, out)
	}
	if out, err := execCerne(t, "spec", "check", "--file", write(specCheckBody(t, okBar, "")), "--dir", projDir); err != nil {
		t.Fatalf("complete: err %v, out:\n%s", err, out)
	}
	out, err = execCerne(t, "spec", "check", "--json", "--file", write(specCheckBody(t, okBar, "[ESCLARECER: qual?]\n")), "--dir", projDir)
	if err != nil {
		t.Fatalf("clarification must only warn: %v\n%s", err, out)
	}
	var issues []doccheck.Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil || len(issues) != 1 || issues[0].Severity != doccheck.SeverityWarning {
		t.Fatalf("json = %q (%v)", out, err)
	}
}

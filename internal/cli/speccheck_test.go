package cli

import (
	"bytes"
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

func specCheckStdin(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// TestSpecCheck_InputsAndErrors (F-0438 crit. 1): stdin, empty input, a missing
// file, JSON errors, and the same title/metadata stripping the chat does.
func TestSpecCheck_InputsAndErrors(t *testing.T) {
	projDir, _, _ := setupProject(t)
	okBar := "Barra: não aplicável (sem comparável) — padrão alternativo: testes\n"

	// A proposal-shaped draft (title + metadata lines) checks like its body.
	draft := "# Título\nfacet: code\n" + specCheckBody(t, okBar, "")
	if out, err := specCheckStdin(t, draft, "spec", "check", "--dir", projDir); err != nil {
		t.Fatalf("stdin draft: %v\n%s", err, out)
	}
	out, err := specCheckStdin(t, "## Problema\n\ntexto\n", "spec", "check", "--json", "--dir", projDir)
	var issues []doccheck.Issue
	if !errors.Is(err, errSpecCheckFailed) || json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &issues) != nil || len(issues) == 0 {
		t.Fatalf("stdin truncated: err %v, out %q", err, out)
	}
	if _, err := specCheckStdin(t, "  \n", "spec", "check", "--dir", projDir); err == nil || errors.Is(err, errSpecCheckFailed) {
		t.Fatalf("empty input must be a usage error, got %v", err)
	}
	out, err = specCheckStdin(t, "", "spec", "check", "--json", "--file", filepath.Join(t.TempDir(), "nope.md"), "--dir", projDir)
	var e struct{ Error string }
	if err == nil || json.Unmarshal([]byte(out[:strings.Index(out, "\n")]), &e) != nil || e.Error == "" {
		t.Fatalf("missing file in --json mode: err %v, out %q", err, out)
	}
}

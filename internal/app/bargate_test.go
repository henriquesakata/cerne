package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpecFixture(t *testing.T, repoDir, id, body string) {
	t.Helper()
	dir := filepath.Join(repoDir, "content", "features", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSpecBar(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		wantErr bool
		wantIn  string // substring the teaching message must carry
	}{
		{"named reference passes", "# t\n\n## Verificação\n\nBarra: work_gate do cerne — .cernerc [gates]\nMétodo: comparação cega\n", false, ""},
		{"nao aplicavel with justification passes", "# t\n\nBarra: não aplicável (fix pontual sem artefato comparável, confirmado pelo humano) — padrão alternativo: teste de integração\n", false, ""},
		{"dispensada with quote passes", "# t\n\nBarra: dispensada por decisão humana (\"pode seguir sem barra dessa vez\")\n", false, ""},
		{"no bar line refuses", "# t\n\n## Verificação\n\nSó testes unitários.\n", true, "Barra:"},
		{"empty bar refuses", "# t\n\nBarra:\n", true, "sem valor"},
		{"nao aplicavel without parens refuses", "# t\n\nBarra: não aplicável\n", true, "justificativa"},
		{"dispensada without quote refuses", "# t\n\nBarra: dispensada por decisão humana\n", true, "citação"},
		{"bar only inside guide comment refuses", "# t\n\n## Verificação\n<!-- uma das três formas: \"Barra: não\naplicável (<justificativa>)\" ou \"Barra: dispensada por decisão humana\". -->\n", true, "Barra:"},
		{"bar inside code fence passes", "# t\n\n## Verificação\n\n```\nBarra: lint:i18n-baseline do CI (F-0484)\nMétodo: comparação cega\n```\n", false, ""},
		{"unaccented nao aplicavel without parens refuses", "# t\n\nBarra: nao aplicavel\n", true, "justificativa"},
		{"malformed first bar does not veto a valid later one", "# t\n\nBarra:\n\n## Verificação\n\nBarra: referência nomeada — acesso real\n", false, ""},
		{"nfd accents pass", "# t\n\nBarra: na\u0303o aplica\u0301vel (sem artefato compara\u0301vel, confirmado) \u2014 padra\u0303o alternativo: invariante\n", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			writeSpecFixture(t, repo, "F-0001", tc.spec)
			err := CheckSpecBar(repo, "F-0001")
			if tc.wantErr {
				if !errors.Is(err, ErrSpecMissingBar) {
					t.Fatalf("want ErrSpecMissingBar, got %v", err)
				}
				if tc.wantIn != "" && !strings.Contains(err.Error(), tc.wantIn) {
					t.Fatalf("error %q lacks teaching substring %q", err, tc.wantIn)
				}
				// Every refusal teaches all three accepted forms.
				for _, form := range []string{"Barra: <referência", "não aplicável", "dispensada por decisão humana"} {
					if !strings.Contains(err.Error(), form) {
						t.Fatalf("error %q does not teach form %q", err, form)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// A missing spec passes — presence is the document gate's concern.
func TestCheckSpecBarMissingSpec(t *testing.T) {
	if err := CheckSpecBar(t.TempDir(), "F-0001"); err != nil {
		t.Fatalf("missing spec should pass, got %v", err)
	}
}

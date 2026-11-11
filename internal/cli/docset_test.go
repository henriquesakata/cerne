package cli

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/spectmpl"
)

func TestHasRealContent(t *testing.T) {
	specStub := "---\ncerne_id: F-0001\n---\n\n# Login\n\n> Spec pendente — escreva o problema, objetivo e critérios de aceite.\n"
	tmplStub := "---\ncerne_id: F-0001\n---\n\n" + spectmpl.Stub("Login com Google")
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty file", "", false},
		{"blank lines only", "\n\n  \n", false},
		{"frontmatter only", "---\ncerne_id: F-0001\n---\n", false},
		{"heading only", "# Login\n", false},
		{"spec stub", specStub, false},
		{"plan stub", "---\ncerne_id: F-0001\n---\n\n# Login\n\n> Plano pendente — descreva os passos.\n", false},
		{"result stub", "# Login\n\n> Resultado pendente — documente o que foi entregue.\n", false},
		{"stub plus real paragraph", specStub + "\nProblema: usuários não conseguem entrar.\n", true},
		{"frontmatter heading and body", "---\ncerne_id: F-0001\n---\n\n# Login\n\nCritérios de aceite definidos.\n", true},
		{"headings only are scaffold", "# Login\n\n## Objetivo\n", false},
		{"template scaffold is a stub", tmplStub, false},
		{"template with a filled section", tmplStub + "\nProblema real: ninguém consegue entrar.\n", true},
		{"body without heading", "Só texto direto.\n", true},
		{"spec stub with utf-8 bom", "\xef\xbb\xbf" + specStub, false},
		{"crlf spec stub", "---\r\ncerne_id: F-0001\r\n---\r\n\r\n# Login\r\n\r\n> Spec pendente — escreva.\r\n", false},
		{"crlf real body", "# Login\r\n\r\nCritérios definidos.\r\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasRealContent([]byte(tt.content)); got != tt.want {
				t.Fatalf("hasRealContent(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

package doccheck

import "testing"

func TestFrontmatterValue(t *testing.T) {
	tests := []struct {
		name string
		data string
		key  string
		want string
	}{
		{
			name: "key present",
			data: "---\ncerne_id: F-0090\nimported_from: docs/process/cases/0090_plugin-autonomous-agent/\n---\n\n# Spec\n",
			key:  "imported_from",
			want: "docs/process/cases/0090_plugin-autonomous-agent/",
		},
		{
			name: "first key",
			data: "---\ncerne_id: F-0090\n---\n",
			key:  "cerne_id",
			want: "F-0090",
		},
		{
			name: "bom prefix",
			data: "\xef\xbb\xbf---\nimported_from: docs/architecture/adr/adr-045-x.md\n---\n",
			key:  "imported_from",
			want: "docs/architecture/adr/adr-045-x.md",
		},
		{
			name: "key absent",
			data: "---\ncerne_id: F-0090\n---\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "no frontmatter block",
			data: "# Spec\n\nimported_from: docs/x.md\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "key only after closing delimiter",
			data: "---\ncerne_id: F-0090\n---\n\nimported_from: docs/x.md\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "key is a prefix of another key",
			data: "---\nimported_from_old: docs/old.md\n---\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "empty data",
			data: "",
			key:  "imported_from",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FrontmatterValue([]byte(tt.data), tt.key); got != tt.want {
				t.Errorf("FrontmatterValue(%q, %q) = %q, want %q", tt.data, tt.key, got, tt.want)
			}
		})
	}
}

// TestHasRealContent pins the stub-vs-content rule. Frontmatter, blank lines,
// markdown headings (structure), HTML-comment guidance and the legacy stub
// blockquotes are scaffold; anything else is real content. This lets a freshly
// scaffolded template (headings + comment guidance) still read as a stub.
func TestHasRealContent(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"frontmatter only", "---\ncerne_id: F-1\n---\n", false},
		{"title heading only", "# Login\n", false},
		{"multiple headings only", "# Login\n\n## Problema\n\n## Desenho\n", false},
		{"html comment guidance only", "# T\n\n<!-- preencha aqui -->\n", false},
		{"multiline html comment scaffold", "# T\n\n<!-- linha um\nlinha dois -->\n\n## Problema\n<!-- guia -->\n", false},
		{"legacy one-line spec stub", "---\ncerne_id: F-1\n---\n\n# T\n\n> Spec pendente — escreva.\n", false},
		{"legacy plan stub", "# T\n\n> Plano pendente — descreva os passos.\n", false},
		{"section filled with prose", "# T\n\n## Problema\n\nUsuários não conseguem entrar.\n", true},
		{"prose without heading", "Só texto direto.\n", true},
		{"table counts as content", "# T\n\n## Decisões do design\n\n| a | b |\n", true},
		{"comment then real line", "<!-- guia -->\nConteúdo real.\n", true},
		{"crlf scaffold is stub", "# T\r\n\r\n## Problema\r\n<!-- guia -->\r\n", false},
		{"bom scaffold is stub", "\xef\xbb\xbf# T\n\n## Problema\n<!-- guia -->\n", false},
		{"crlf real body", "# T\r\n\r\nCritérios definidos.\r\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasRealContent([]byte(tt.data)); got != tt.want {
				t.Errorf("HasRealContent(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

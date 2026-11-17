package doccheck

import "testing"

func TestBodyTitle(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"plain", "# Title\n\nbody", "Title"},
		{"trimmed", "#   Spaced title  \nx", "Spaced title"},
		{"after frontmatter", "---\ncerne_id: N-1\n---\n\n# FM title\n", "FM title"},
		{"BOM and blank lines", "\xef\xbb\xbf\n\n# BOM\n", "BOM"},
		{"h2 is not h1", "## Sub\n# Late\n", ""},
		{"after other content", "intro\n\n# Late\n", ""},
		{"inside a fence", "```\n# Not a title\n```\n", ""},
		{"fence after frontmatter", "---\na: b\n---\n~~~\n# x\n~~~\n", ""},
		{"no space", "#NoSpace\n", ""},
		{"empty", "", ""},
		{"unterminated frontmatter is content", "---\n# Title\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BodyTitle([]byte(tc.body)); got != tc.want {
				t.Fatalf("BodyTitle(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

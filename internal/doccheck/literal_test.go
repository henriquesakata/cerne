package doccheck

import "testing"

// F-0500 crit. 5: the literal check, whitespace-normalized, with real lines.
func TestFindLiteral(t *testing.T) {
	tests := []struct {
		name, content, snippet string
		start, end             int
		ok                     bool
	}{
		{"single line", "a\nhello world\nb\n", "hello world", 2, 2, true},
		{"wraps lines, double spaces and tabs", "intro\nthe  cache\n\twas   removed\nnext\n", "the cache was removed", 2, 3, true},
		{"snippet with different breaks", "x\nfoo bar baz\n", "foo\nbar\tbaz", 2, 2, true},
		{"end of file without newline", "a\nb\nlast line", "last line", 3, 3, true},
		{"absent", "a\nb\n", "missing", 0, 0, false},
		{"empty snippet", "a\nb\n", "", 0, 0, false},
		{"blank snippet", "a\nb\n", " \t\n ", 0, 0, false},
		{"multibyte before the snippet", "ação — é ótimo\nlinha ã\nalvo aqui\n", "alvo aqui", 3, 3, true},
		{"crlf", "a\r\nfoo\r\nbar\r\nz\r\n", "foo bar", 2, 3, true},
		{"trimmed snippet", "a\nfoo\n", "  foo  \n", 2, 2, true},
		{"unicode space", "a\nfoo bar\n", "foo bar", 2, 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e, ok := FindLiteral(tt.content, tt.snippet)
			if s != tt.start || e != tt.end || ok != tt.ok {
				t.Fatalf("FindLiteral = (%d,%d,%v), want (%d,%d,%v)", s, e, ok, tt.start, tt.end, tt.ok)
			}
		})
	}
}

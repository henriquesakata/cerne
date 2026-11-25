package doccheck

import "testing"

// F-0500 crit. 5: one normalization of the file serves every snippet.
func TestLiteralIndexFindsManySnippets(t *testing.T) {
	content := "intro\nthe  cache\n\twas   removed\nand later\nthe end\n"
	idx := NewLiteralIndex(content)
	for _, tt := range []struct {
		snippet    string
		start, end int
		ok         bool
	}{
		{"the cache was removed", 2, 3, true},
		{"later the end", 4, 5, true},
		{"never there", 0, 0, false},
		{"", 0, 0, false},
	} {
		s, e, ok := idx.Find(tt.snippet)
		if s != tt.start || e != tt.end || ok != tt.ok {
			t.Errorf("Find(%q) = (%d,%d,%v), want (%d,%d,%v)", tt.snippet, s, e, ok, tt.start, tt.end, tt.ok)
		}
	}
}

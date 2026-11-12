package lint

import "testing"

// F-0352: findings of one file come in document order (line 9 before line 10),
// not in the lexical order of "line N: …".
func TestSortOrdersByLine(t *testing.T) {
	fs := []Finding{
		{Rule: RuleBrokenLink, File: "a.md", Line: 10, Detail: "line 10: x"},
		{Rule: RuleBrokenLink, File: "a.md", Line: 9, Detail: "line 9: x"},
	}
	Sort(fs)
	if fs[0].Line != 9 {
		t.Fatalf("order = %+v", fs)
	}
}

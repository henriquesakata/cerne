package doccheck

import (
	"strings"
	"unicode"
)

// FindLiteral looks for snippet in content, comparing with every run of Unicode
// whitespace collapsed to one space on both sides and the snippet trimmed. It
// returns the 1-based lines of the original content where the match starts and
// ends. An empty (or all-whitespace) snippet never matches.
func FindLiteral(content, snippet string) (start, end int, ok bool) {
	want := strings.Join(strings.FieldsFunc(snippet, unicode.IsSpace), " ")
	if want == "" {
		return 0, 0, false
	}
	// norm is content with whitespace runs collapsed; lines[i] is the original
	// line of norm's byte i.
	var norm strings.Builder
	var lines []int
	line, inSpace := 1, false
	for _, r := range content {
		if unicode.IsSpace(r) {
			if !inSpace {
				norm.WriteByte(' ')
				lines = append(lines, line)
				inSpace = true
			}
			if r == '\n' {
				line++
			}
			continue
		}
		inSpace = false
		n, _ := norm.WriteRune(r)
		for range n {
			lines = append(lines, line)
		}
	}
	i := strings.Index(norm.String(), want)
	if i < 0 {
		return 0, 0, false
	}
	return lines[i], lines[i+len(want)-1], true
}

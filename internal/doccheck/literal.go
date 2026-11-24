package doccheck

import (
	"strings"
	"unicode"
)

// LiteralIndex is a document normalized once for literal searches: every run
// of Unicode whitespace collapsed to one space, with the original line of each
// byte kept, so many snippets can be looked up without renormalizing.
type LiteralIndex struct {
	norm  string
	// ponytail: ~4 bytes of index per source byte (int32 per normalized byte), so a
	// 10 MiB source costs ~40 MiB while cached. Upgrade: store line offsets and
	// binary-search them if sources ever grow past the cap.
	lines []int32 // lines[i] is the 1-based original line of norm's byte i
}

// NewLiteralIndex normalizes content for Find.
func NewLiteralIndex(content string) *LiteralIndex {
	var norm strings.Builder
	var lines []int32
	line, inSpace := int32(1), false
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
	return &LiteralIndex{norm: norm.String(), lines: lines}
}

// Find looks for snippet, trimmed and with whitespace runs collapsed, and
// returns the 1-based original lines where the match starts and ends. An empty
// (or all-whitespace) snippet never matches.
func (x *LiteralIndex) Find(snippet string) (start, end int, ok bool) {
	want := strings.Join(strings.FieldsFunc(snippet, unicode.IsSpace), " ")
	if want == "" {
		return 0, 0, false
	}
	i := strings.Index(x.norm, want)
	if i < 0 {
		return 0, 0, false
	}
	return int(x.lines[i]), int(x.lines[i+len(want)-1]), true
}

// FindLiteral looks for snippet in content, comparing with every run of Unicode
// whitespace collapsed to one space on both sides and the snippet trimmed. It
// returns the 1-based lines of the original content where the match starts and
// ends. An empty (or all-whitespace) snippet never matches. To look up several
// snippets in one document, build a LiteralIndex once.
func FindLiteral(content, snippet string) (start, end int, ok bool) {
	return NewLiteralIndex(content).Find(snippet)
}

// Package doccheck decides whether a lifecycle document carries real
// content beyond the stub cerne writes when the document is created.
package doccheck

import (
	"bytes"
	"strings"
)

// stubPrefixes are the placeholder lines cerne writes into freshly created
// lifecycle documents; a document whose body is only these is still a stub.
var stubPrefixes = []string{"> Spec pendente", "> Plano pendente", "> Resultado pendente", "> Decisão pendente"}

// FrontmatterValue returns the value of key inside the leading "---"
// delimited frontmatter block of data; "" when the block or the key is
// absent. A UTF-8 BOM before the block is tolerated.
func FrontmatterValue(data []byte, key string) string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return ""
		}
		if v, ok := strings.CutPrefix(trimmed, key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// HasRealContent reports whether a lifecycle document carries content beyond
// its scaffold. Scaffold is everything cerne (or the spec template) lays down
// for the author to fill: the leading frontmatter block, blank lines, markdown
// headings (structure, not content), HTML-comment guidance (single- or
// multi-line) and the legacy stub placeholder blockquotes. If any other line
// remains, the document has real content. This lets a freshly scaffolded
// template — section headings plus comment guidance — still read as a stub so
// the lifecycle gates stay closed until it is filled in.
func HasRealContent(data []byte) bool {
	// Editors on Windows may prepend a UTF-8 BOM; without stripping it the
	// frontmatter delimiter on line 1 would not match and a stub would
	// wrongly count as real content.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := strings.Split(string(data), "\n")
	// Skip a leading frontmatter block delimited by "---" lines.
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	inComment := false
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case inComment:
			// Inside a multi-line HTML comment; it ends on the line with "-->".
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		case strings.HasPrefix(trimmed, "<!--"):
			// HTML-comment guidance is scaffold; a comment that does not close
			// on its own line opens a multi-line block.
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
			continue
		case strings.HasPrefix(trimmed, "#"):
			// Markdown headings are structure, not content.
			continue
		case isStubLine(trimmed):
			continue
		default:
			return true
		}
	}
	return false
}

// isStubLine reports whether trimmed is one of the legacy one-line stub
// placeholders cerne used to write.
func isStubLine(trimmed string) bool {
	for _, p := range stubPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

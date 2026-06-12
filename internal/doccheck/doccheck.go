// Package doccheck decides whether a lifecycle document carries real
// content beyond the stub cerne writes when the document is created.
package doccheck

import (
	"bytes"
	"strings"
)

// stubPrefixes are the placeholder lines cerne writes into freshly created
// lifecycle documents; a document whose body is only these is still a stub.
var stubPrefixes = []string{"> Spec pendente", "> Plano pendente", "> Resultado pendente"}

// HasRealContent reports whether a lifecycle document carries content beyond
// its stub: ignoring the leading frontmatter block, blank lines, the first
// "#" heading and stub placeholder lines, at least one line remains.
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
	headingSeen := false
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !headingSeen && strings.HasPrefix(trimmed, "#") {
			headingSeen = true
			continue
		}
		isStub := false
		for _, p := range stubPrefixes {
			if strings.HasPrefix(trimmed, p) {
				isStub = true
				break
			}
		}
		if !isStub {
			return true
		}
	}
	return false
}

// Package doccheck decides whether a lifecycle document carries real
// content beyond the stub cerne writes when the document is created.
package doccheck

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
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

// MissingSections returns the canonical sections absent from data as level-2
// (## ) headings, in canonical order. A heading present with "N/A" content still
// counts as present (right-sizing): only the heading is checked, not its body.
func MissingSections(data []byte, kind doclang.Kind, l doclang.Lang) []string {
	present := map[string]bool{}
	var fence string // the opening run of the fenced block we are inside, if any
	for _, line := range strings.Split(string(data), "\n") {
		if run := fenceRun(line); run != "" {
			switch {
			case fence == "":
				fence = run
			case run[0] == fence[0] && len(run) >= len(fence) && strings.TrimSpace(line) == run:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue // a "## " inside a code block is not a heading (CommonMark)
		}
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "## "); ok {
			present[strings.TrimSpace(h)] = true
		}
	}
	return doclang.Missing(kind, present, l)
}

// fenceRun returns the run of 3+ backticks or tildes that opens or closes a
// fenced code block on line (up to 3 spaces of indent), or "".
func fenceRun(line string) string {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return t[:n]
}

// StripHTMLComments drops HTML comments (template scaffold, never record). A
// "<!--" inside a fenced code block or an inline code span is text, never an
// opener (CommonMark): read left to right, whichever of a code span or a
// comment starts first wins, so a comment quoting `code` still goes (B-0077).
// A comment goes whole, inner newlines included; one with no "-->" stays.
func StripHTMLComments(b []byte) []byte {
	out := make([]byte, 0, len(b))
	fence := "" // the opening run of the fenced block we are inside, if any
	for i := 0; i < len(b); {
		if i == 0 || b[i-1] == '\n' {
			end := len(b)
			if k := bytes.IndexByte(b[i:], '\n'); k >= 0 {
				end = i + k + 1
			}
			line := string(b[i:end])
			if run := fenceRun(line); fence != "" || run != "" {
				switch {
				case fence == "":
					fence = run
				case run != "" && run[0] == fence[0] && len(run) >= len(fence) && strings.TrimSpace(line) == run:
					fence = ""
				}
				out = append(out, line...)
				i = end
				continue
			}
		}
		switch {
		case b[i] == '`':
			n := runLen(b[i:], '`')
			if j := closingRun(b, i+n, n); j >= 0 {
				n = j + n - i
			}
			out = append(out, b[i:i+n]...) // a span, or a run with no closer (literal)
			i += n
			continue
		case bytes.HasPrefix(b[i:], []byte("<!--")):
			if j := bytes.Index(b[i+4:], []byte("-->")); j >= 0 {
				i += 4 + j + 3
				continue
			}
		}
		out = append(out, b[i])
		i++
	}
	return out
}

// closingRun returns the index of the next run of exactly n backticks from
// from on, or -1 when a blank line (a code span never leaves its paragraph) or
// the end comes first.
func closingRun(b []byte, from, n int) int {
	for j := from; j < len(b); {
		switch {
		case b[j] == '`':
			m := runLen(b[j:], '`')
			if m == n {
				return j
			}
			j += m
		case b[j] == '\n' && bytes.HasPrefix(bytes.TrimLeft(b[j+1:], " \t\r"), []byte("\n")):
			return -1
		default:
			j++
		}
	}
	return -1
}

// runLen is how many leading bytes of b are c.
func runLen(b []byte, c byte) int {
	n := 0
	for n < len(b) && b[n] == c {
		n++
	}
	return n
}

// frontmatterKeyLine is a top-level "key: value" line — with blank lines, the
// only line a frontmatter block may hold before its closing '---'.
var frontmatterKeyLine = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*:(\s|$)`)

// LeadingBlock reads the '---' frontmatter block lines open with: its inner
// lines and the lines after its closing '---'. opens is false when the first
// line is not '---'. closed is false when that '---' opens no block — a line
// that is neither blank nor "key: value" comes before any closing '---' (prose,
// a heading, a code fence), or no '---' follows at all — so a later rule or
// fence never closes it and swallows the document (F-0328 critic C2). One rule
// for the stored-body path (app) and the title of a `new` command (cli).
func LeadingBlock(lines []string) (inner, rest []string, opens, closed bool) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, lines, false, false
	}
	for i := 1; i < len(lines); i++ {
		switch l := lines[i]; {
		case strings.TrimSpace(l) == "---":
			return lines[1:i], lines[i+1:], true, true
		case strings.TrimSpace(l) != "" && !frontmatterKeyLine.MatchString(l):
			return nil, lines, true, false
		}
	}
	return nil, lines, true, false
}

// BodyTitle returns the text of the body's first heading when that heading is
// an H1 ("# ", or "#" and a tab) opening the body — after the leading
// frontmatter block (LeadingBlock), blank lines and a UTF-8 BOM — with the
// optional closing "#"s of CommonMark removed and the text trimmed; "" when
// there is none or it is empty. "## " is not an H1; a "# " after other content,
// inside a fenced block or after a '---' that opens no block (a rule) is not the
// first heading. Inline markdown is kept verbatim, like a title argument.
func BodyTitle(data []byte) string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(strings.TrimLeft(s, "\ufeff \t\r\n"), "\n")
	if _, rest, opens, closed := LeadingBlock(lines); opens && closed {
		lines = rest
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		text, ok := strings.CutPrefix(t, "#")
		if !ok || (!strings.HasPrefix(text, " ") && !strings.HasPrefix(text, "\t")) {
			return ""
		}
		text = strings.TrimSpace(text)
		// closing sequence: trailing #s preceded by a space or tab (or the whole text)
		if trimmed := strings.TrimRight(text, "#"); trimmed != text && (trimmed == "" || strings.HasSuffix(trimmed, " ") || strings.HasSuffix(trimmed, "\t")) {
			text = strings.TrimSpace(trimmed)
		}
		return text
	}
	return ""
}

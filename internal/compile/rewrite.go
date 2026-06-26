package compile

import (
	"fmt"
	"regexp"
	"strings"
)

// entityIDRx matches a cerne entity id as a whole token: a known prefix plus a
// 4-digit number, bounded so "D-00031" is not matched as "D-0003".
var entityIDRx = regexp.MustCompile(`\b(?:RB|F|D|N|H)-\d{4}\b`)

// inlineCodeRx matches a single- or double-backtick inline code span on one line
// (double-backtick alternative first so it is preferred at a given position).
var inlineCodeRx = regexp.MustCompile("``[^`\n]*``|`[^`\n]*`")

// rewriteSupersededRefs rewrites, in content, each entity id that resolve maps to
// a terminal successor — turning "D-0003" into "D-0002 (antes D-0003)" — so the
// wiki synthesis never presents a superseded id as the current reference. Ids
// inside fenced code blocks or inline code spans are left untouched (they are not
// live references), and the surrounding prose is preserved verbatim. The function
// is pure: it never reads or writes the source entity, honoring immutability —
// only the ephemeral excerpt that feeds the prompt is rewritten.
func rewriteSupersededRefs(content string, resolve func(id string) (string, bool)) string {
	lines := strings.Split(content, "\n")
	inFence := false
	for i, line := range lines {
		if isFenceDelim(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = rewriteOutsideInlineCode(line, resolve)
	}
	return strings.Join(lines, "\n")
}

// rewriteOutsideInlineCode rewrites ids in line, copying inline code spans verbatim
// so an id inside `...` is never treated as a live reference.
func rewriteOutsideInlineCode(line string, resolve func(id string) (string, bool)) string {
	var b strings.Builder
	last := 0
	for _, span := range inlineCodeRx.FindAllStringIndex(line, -1) {
		b.WriteString(rewriteIDs(line[last:span[0]], resolve))
		b.WriteString(line[span[0]:span[1]])
		last = span[1]
	}
	b.WriteString(rewriteIDs(line[last:], resolve))
	return b.String()
}

// rewriteIDs replaces every resolvable id token in s with "<successor> (antes <id>)".
func rewriteIDs(s string, resolve func(id string) (string, bool)) string {
	return entityIDRx.ReplaceAllStringFunc(s, func(id string) string {
		if succ, ok := resolve(id); ok {
			return fmt.Sprintf("%s (antes %s)", succ, id)
		}
		return id
	})
}

// isFenceDelim reports whether the line opens or closes a fenced code block: its
// first non-space content is a run of three or more backticks or tildes.
func isFenceDelim(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// stripFrontmatter removes a leading "---\n…\n---" block, returning the body that
// follows it (a doc with no leading frontmatter is returned unchanged).
func stripFrontmatter(s string) string {
	t := strings.TrimLeft(s, " \t\n")
	if !strings.HasPrefix(t, "---\n") {
		return s
	}
	if i := strings.Index(t[4:], "\n---"); i >= 0 {
		return strings.TrimLeft(t[4+i+4:], "\n")
	}
	return s
}

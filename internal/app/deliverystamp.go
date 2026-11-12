package app

import (
	"fmt"
	"regexp"
	"strings"
)

// F-0355: the "## Entrega" block of a result — the commit and PR that delivered
// the feature as markdown links. It is a projection of the feature's delivery
// data, generated and replaced like cost's "## Custo" (internal/cost/stamp.go).

var (
	entregaHeadingRe = regexp.MustCompile(`^##[ \t]+Entrega[ \t]*$`)
	custoHeadingRe   = regexp.MustCompile(`^##[ \t]+Custo[ \t]*$`)
	h2Re             = regexp.MustCompile(`^##[ \t]`)
	prURLRe          = regexp.MustCompile(`^https?://[^/]+/(.+?)(?:/-)?/(?:pull|pulls|merge_requests)/(\d+)/?$`)
)

// sectionSpan locates the first "## " section of result whose heading matches
// heading, ignoring fenced code blocks (a quoted example must not hijack the
// stamp): [start, end) byte offsets, end at the next "## " heading outside a
// fence or the end of the text.
func sectionSpan(result string, heading *regexp.Regexp) (start, end int, ok bool) {
	start, end = -1, len(result)
	var fchar byte // the open fence's character, 0 outside a fence
	fn, off := 0, 0
	for _, line := range strings.SplitAfter(result, "\n") {
		text := strings.TrimRight(line, "\r\n")
		ch, n, rest := fenceRun(text)
		switch {
		case fchar != 0:
			// CommonMark: closed only by the same character, at least as long,
			// with nothing but spaces after it.
			if ch == fchar && n >= fn && strings.TrimSpace(rest) == "" {
				fchar = 0
			}
		case n >= 3 && !(ch == '`' && strings.Contains(rest, "`")):
			fchar, fn = ch, n
		case start < 0:
			if heading.MatchString(text) {
				start = off
			}
		case h2Re.MatchString(text):
			return start, off, true
		}
		off += len(line)
	}
	return start, end, start >= 0
}

// fenceRun reads the run of backticks or tildes that starts a line (up to three
// spaces of indent): its character, length and the text after it. Length 0 when
// the line starts no such run.
func fenceRun(line string) (ch byte, n int, rest string) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || t == "" || (t[0] != '`' && t[0] != '~') {
		return 0, 0, ""
	}
	ch = t[0]
	for n < len(t) && t[n] == ch {
		n++
	}
	return ch, n, t[n:]
}

// deliveryBlock renders the "## Entrega" section — document content, pt-BR like
// the "## Custo" block. "" when there is nothing to show.
func deliveryBlock(commitURL, prURL string) string {
	var b strings.Builder
	if commitURL != "" {
		label := commitURL
		if i := strings.LastIndex(commitURL, "/"); i >= 0 && i < len(commitURL)-1 {
			label = commitURL[i+1:]
			if len(label) > 9 {
				label = label[:9]
			}
		}
		fmt.Fprintf(&b, "- Commit: [`%s`](%s)\n", label, commitURL)
	}
	if prURL != "" {
		label := prURL
		if m := prURLRe.FindStringSubmatch(prURL); m != nil {
			label = m[1] + "#" + m[2]
		}
		fmt.Fprintf(&b, "- PR: [%s](%s)\n", label, prURL)
	}
	if b.Len() == 0 {
		return ""
	}
	return "## Entrega\n\n" + b.String()
}

// StampDelivery writes the "## Entrega" block into result: replacing the real
// existing block in place, else inserted before "## Custo" when there is one,
// else appended at the end. With no link it returns result unchanged.
func StampDelivery(result, commitURL, prURL string) string {
	blk := deliveryBlock(commitURL, prURL)
	if blk == "" {
		return result
	}
	if start, end, ok := sectionSpan(result, entregaHeadingRe); ok {
		if end < len(result) {
			return result[:start] + blk + "\n" + result[end:]
		}
		return result[:start] + blk
	}
	if start, _, ok := sectionSpan(result, custoHeadingRe); ok {
		return result[:start] + blk + "\n" + result[start:]
	}
	return strings.TrimRight(result, "\n") + "\n\n" + blk
}

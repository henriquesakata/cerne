package compile

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotAPage marks a synthesis response that is not a wiki page: the model
// answered with something else — most often a report about having written the
// page somewhere — and the caller must refuse to stamp it as compiled.
var ErrNotAPage = errors.New("synthesis response is not a wiki page")

// minPageBodyRunes is the floor a synthesized page must clear. It is set well
// below any real page (the three live pages of this project's own backend run
// from ~1.5 KB up) and just above the agent reports that motivated F-0287,
// which fit in a couple of lines. The point is to catch a category error, not
// to judge how thorough a page is.
const minPageBodyRunes = 200

// ValidatePageBody reports whether body may be stamped as a wiki page, per the
// output contract BuildPagePrompt states: markdown on stdout, starting with a
// title. It exists because the response comes back from a Claude Code session,
// which can answer with prose about what it did instead of the artifact — that
// is exactly what shipped into content/wiki/project.md before F-0287, stamped
// with provenance and then reported clean by the input-hash.
//
// Two deterministic rules, both loose on purpose (a false positive only leaves
// the page dirty for the next round; a false negative corrupts the wiki):
// the first non-empty line must be a markdown heading, and the body must clear
// minPageBodyRunes. A leading frontmatter block the model added is ignored —
// cerne stamps its own anyway.
func ValidatePageBody(body []byte) error {
	text := strings.TrimSpace(stripFrontmatter(string(body)))
	if text == "" {
		return fmt.Errorf("%w: empty response", ErrNotAPage)
	}
	if first, _, _ := strings.Cut(text, "\n"); !isHeading(first) {
		return fmt.Errorf("%w: starts with %q, not a markdown title", ErrNotAPage, truncateRunes(first, 60))
	}
	if n := len([]rune(text)); n < minPageBodyRunes {
		return fmt.Errorf("%w: %d characters is too short to be a page", ErrNotAPage, n)
	}
	return nil
}

// isHeading reports whether line is an ATX markdown heading ("# Title"), the
// shape every compiled page opens with.
func isHeading(line string) bool {
	rest := strings.TrimLeft(strings.TrimSpace(line), "#")
	return rest != strings.TrimSpace(line) && strings.HasPrefix(rest, " ")
}

// truncateRunes bounds a quoted excerpt in an error message.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

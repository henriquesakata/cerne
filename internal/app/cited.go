package app

import (
	"regexp"
	"strings"
)

// cerneTrailerRe is a Cerne: trailer and the rest of its own line: one or more
// ids, separated by commas or spaces. [ \t] and not \s, so an empty trailer
// never borrows the next line; [^\r\n] so a CRLF line ends cleanly.
var cerneTrailerRe = regexp.MustCompile(`(?mi)^Cerne:[ \t]*([^\r\n]*\S)`)

// CitedCardIDs is every card a Cerne: trailer of msg names (any case, several
// ids per line): the one grammar the commit hook writes and the Mirror tab reads.
func CitedCardIDs(msg string) []string {
	var ids []string
	for _, m := range cerneTrailerRe.FindAllStringSubmatch(msg, -1) {
		ids = append(ids, strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })...)
	}
	return ids
}

package doccheck

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The two ways a spec fails the Gauntlet bar (F-0545). SpecBar wraps
// ErrBarMalformed with the specific reason.
var (
	ErrBarMissing   = errors.New(`no "Barra:" line`)
	ErrBarMalformed = errors.New(`malformed "Barra:" line`)
)

// The three literal forms a spec may carry (cerne-authoring skill, F-0270).
// Accent alternations tolerate NFC, NFD (darwin paste) and the bare unaccented
// spelling, mirroring the lint-side mention matching.
var (
	htmlCommentRe        = regexp.MustCompile(`(?s)<!--.*?-->`)
	barLineRe            = regexp.MustCompile(`(?m)^\s*Barra:\s*(.*)$`)
	naoAplicavelRe       = regexp.MustCompile(`(?i)^n(?:ã|a\x{0303}|a)o\s+aplic(?:á|a\x{0301}|a)vel\s*\(\s*[^)]+\)`)
	dispensadaRe         = regexp.MustCompile(`(?i)^dispensada\s+por\s+decis(?:ã|a\x{0303}|a)o\s+humana\s*\(\s*[^)]+\)`)
	naoAplicavelPrefixRe = regexp.MustCompile(`(?i)^n(?:ã|a\x{0303}|a)o\s+aplic(?:á|a\x{0301}|a)vel`)
	dispensadaPrefixRe   = regexp.MustCompile(`(?i)^dispensada`)
)

// SpecBar returns the first valid bar line of a spec body, or the reason none
// is valid (ErrBarMissing | ErrBarMalformed). Guide comments in the template
// contain the forms as examples, so HTML comments are stripped before
// scanning. Any valid line satisfies it — a malformed leftover (e.g. template
// residue) must not veto a valid bar written further down; when none is valid,
// the first failure is the one reported. Telling a stub from a written spec is
// the caller's job (the document gate).
func SpecBar(body []byte) (string, error) {
	text := htmlCommentRe.ReplaceAllString(string(body), "")
	matches := barLineRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return "", ErrBarMissing
	}
	var firstInvalid error
	for _, m := range matches {
		value := strings.TrimSpace(m[1])
		var err error
		switch {
		case value == "":
			err = fmt.Errorf(`%w: no value after "Barra:"`, ErrBarMalformed)
		case naoAplicavelPrefixRe.MatchString(value) && !naoAplicavelRe.MatchString(value):
			err = fmt.Errorf(`%w: "não aplicável" needs its one-line justification in parentheses`, ErrBarMalformed)
		case dispensadaPrefixRe.MatchString(value) && !dispensadaRe.MatchString(value):
			err = fmt.Errorf(`%w: "dispensada por decisão humana" needs the quoted words in parentheses`, ErrBarMalformed)
		default:
			return "Barra: " + value, nil
		}
		if firstInvalid == nil {
			firstInvalid = err
		}
	}
	return "", firstInvalid
}

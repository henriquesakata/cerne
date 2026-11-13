package gauntlet

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// Sentinels of the record verb: each one refuses before anything is written.
var (
	ErrNoVerdict   = errors.New("the file does not end with a verdict line (VEREDITO: A|B|EMPATE or VERDICT: A|B|TIE) — the protocol counts that as a loss")
	ErrBadOurs     = errors.New("--ours must be A or B")
	ErrNoQuote     = errors.New("no quote to record: the verdict file has no sentence before the verdict line (pass --quote)")
	ErrEmptyCritic = errors.New("the critic's line is empty")
)

var (
	quotedLineRe = regexp.MustCompile(`^["“«]\s*(.+?)\s*["”»]\.?$`)
	sentenceRe   = regexp.MustCompile(`^.*?[.!?](?:\s|$)`)
	innerQuoteRe = regexp.MustCompile(`["“”«»]`)
	headingRe    = regexp.MustCompile(`^##[ \t]+(.*?)[ \t]*$`)
	markersRe    = regexp.MustCompile(`^` + doclang.LineMarkers)
	commentRe    = regexp.MustCompile("(?s)(^|[^`])(<!--.*?-->)")
	continuedRe  = regexp.MustCompile(`^(?:[ \t]+\S|[-*+][ \t]|\d+[.)][ \t]|\()`)
)

// VerdictLine reads the last non-empty line of text as the judge's verdict
// line: the winner "A" or "B", or "TIE" (doclang.JudgeVerdict). The one
// recognizer `gauntlet record` and the cost classifier share.
func VerdictLine(text string) (string, bool) {
	text = strings.TrimRight(text, " \t\r\n")
	return doclang.JudgeVerdict(strings.TrimSpace(text[strings.LastIndex(text, "\n")+1:]))
}

// AddRound records the next round in result's Gauntlet section from the
// judge's reply: the winner on its last non-empty line against ours (A or B).
// quote overrides the citation; without it the last fully quoted line before
// the verdict is used, else the first sentence of the reply. It returns the
// new result, the round's number and the outcome word written.
func AddRound(result string, l doclang.Lang, verdictFile, ours, quote string) (out string, n int, outcome string, err error) {
	ours = strings.ToUpper(strings.TrimSpace(ours))
	if ours != "A" && ours != "B" {
		return "", 0, "", ErrBadOurs
	}
	lines := strings.Split(strings.TrimRight(verdictFile, " \t\r\n"), "\n")
	winner, ok := VerdictLine(verdictFile)
	if !ok {
		return "", 0, "", ErrNoVerdict
	}
	term := doclang.Loss
	switch {
	case winner == "TIE":
		term = doclang.Tie
	case winner == ours:
		term = doclang.Win
	}
	if quote = cleanQuote(quote); quote == "" {
		quote = chooseQuote(lines[:len(lines)-1])
	}
	if quote == "" {
		return "", 0, "", ErrNoQuote
	}
	n = lastRound(result) + 1
	outcome = doclang.Display(term, l)
	line := fmt.Sprintf("%s %d — %s: \"%s\"", doclang.Display(doclang.Round, l), n, outcome, quote)
	return insert(result, l, line), n, outcome, nil
}

// AddCritic appends the critic's findings line to result's Gauntlet section.
func AddCritic(result string, l doclang.Lang, findings string) (string, error) {
	findings = strings.Join(strings.Fields(findings), " ")
	if findings == "" {
		return "", ErrEmptyCritic
	}
	return insert(result, l, doclang.Display(doclang.CriticFindings, l)+": "+findings), nil
}

func cleanQuote(s string) string {
	return strings.Join(strings.Fields(innerQuoteRe.ReplaceAllString(s, "'")), " ")
}

func chooseQuote(body []string) string {
	for i := len(body) - 1; i >= 0; i-- {
		if m := quotedLineRe.FindStringSubmatch(strings.TrimSpace(body[i])); m != nil {
			if q := cleanQuote(m[1]); q != "" {
				return q
			}
		}
	}
	text := strings.Join(strings.Fields(strings.Join(body, " ")), " ")
	if m := sentenceRe.FindString(text); m != "" {
		text = m
	}
	return cleanQuote(text)
}

// maskComments blanks every HTML comment (the lint's own notion: template
// guides are never record) keeping the line structure, so line i of the
// result is line i of s.
func maskComments(s string) string {
	return commentRe.ReplaceAllStringFunc(s, func(m string) string {
		c := commentRe.FindStringSubmatch(m)
		return c[1] + strings.Map(func(r rune) rune {
			if r == '\n' {
				return r
			}
			return ' '
		}, c[2])
	})
}

// gauntletSpan locates the Gauntlet section in the comment-masked lines: the
// index of its heading line and the end (the next level-2 heading or the end
// of the file), skipping fenced code. start is -1 when the section is absent.
func gauntletSpan(lines []string) (start, end int) {
	start, end = -1, len(lines)
	inCode := false
	for i, ln := range lines {
		if doclang.Fence(ln) {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if m := headingRe.FindStringSubmatch(ln); m != nil {
			if start >= 0 {
				return start, i
			}
			if doclang.IsGauntletSection(m[1]) {
				start = i
			}
		}
	}
	return start, end
}

func isRecordLine(ln string) bool {
	t := markersRe.ReplaceAllString(ln, "")
	if _, _, ok := doclang.RoundAnchor(t); ok || doclang.CriticLine(t) || doclang.HeaderLine(t) {
		return true
	}
	_, _, ok := doclang.BarLine(ln, false)
	return ok
}

func lastRound(result string) int {
	lines := strings.Split(maskComments(strings.ReplaceAll(result, "\r\n", "\n")), "\n")
	start, end := gauntletSpan(lines)
	if start < 0 {
		return 0
	}
	max := 0
	for _, ln := range lines[start+1 : end] {
		if n, _, ok := doclang.RoundAnchor(markersRe.ReplaceAllString(ln, "")); ok && n > max {
			max = n
		}
	}
	return max
}

// insert puts line after the section's last round, critic, header or bar
// line and the continuation lines that belong to it (indented, list items,
// parentheticals); after the heading when there is none, or it opens the
// section at the end of the file. The file's line ending is kept.
func insert(result string, l doclang.Lang, line string) string {
	crlf := strings.Contains(result, "\r\n")
	result = strings.ReplaceAll(result, "\r\n", "\n")
	lines := strings.Split(result, "\n")
	masked := strings.Split(maskComments(result), "\n")
	var out string
	if start, end := gauntletSpan(masked); start < 0 {
		heading := "## " + doclang.Sections(doclang.Result, l)[5]
		out = strings.TrimRight(result, "\n") + "\n\n" + heading + "\n\n" + line + "\n"
	} else {
		at := start
		for i := start + 1; i < end; i++ {
			if isRecordLine(masked[i]) {
				at = i
			}
		}
		if at != start {
			for at+1 < end && continuedRe.MatchString(masked[at+1]) {
				at++
			}
		}
		ins := []string{line}
		if at == start {
			ins = []string{"", line}
		}
		out = strings.Join(append(append(append([]string{}, lines[:at+1]...), ins...), lines[at+1:]...), "\n")
	}
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out
}

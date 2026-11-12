package store

import (
	"fmt"
	"regexp"
	"strings"
)

// Errata items (F-0872). An erratum stays what it always was — an additive
// block `## errata <date> — <author>` in content/errata/<id>.md — and gains
// optional declared metadata lines right under its title:
//
//	Altera: S6.6                                  (the step it changes, declared, never inferred)
//	Estado: decidida — "<quote>" (<nick>, <date>)  (absent = proposta)
//	Fundida: revisão 3                             (set by the merge)
//	Sem passo: sim                                 (F-0345: no step on purpose; written with `Altera: -`)
//	Anterior: decidida — "<quote>" (<nick>, <date>) (a decision a redecide replaced; one line each)
//
// Only the block cerne writes is metadata: AddErrataAt always opens it (`Altera: -`
// when no step is declared) and a blank line closes it, so free text that looks
// like a metadata line sits after the block and is never read as one.
//
// Blocks written before F-0872 have none of them and parse as proposals with no
// step. The item id is E<n>, by order in the file.

var (
	errataHeaderRe = regexp.MustCompile(`^## errata (\d{4}-\d{2}-\d{2}) — (.+)$`)
	errataMetaRe   = regexp.MustCompile(`^(Altera|Estado|Fundida|Anterior|Sem passo): (.*)$`)
	errataStateRe  = regexp.MustCompile(`^decidida — "(.*)" \((.+), (\d{4}-\d{2}-\d{2})\)$`)
	headingRe      = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
)

// NoStep is the `Altera:` value cerne writes when an erratum declares no step.
const NoStep = "-"

// ErrataItem is one parsed erratum block.
type ErrataItem struct {
	ID        string // E1, E2, … by order in the file
	Date      string // YYYY-MM-DD from the block title
	Author    string
	Target    string // declared step ("S6.6"); "" when none
	Stepless  bool   // `Sem passo: sim`: no step by choice, not by omission (F-0345)
	Quote     string // the decision, quoted; "" while a proposal
	DecidedBy string
	DecidedAt string
	Merged    string   // "" until the merge folds it into the body
	Previous  []string // decisions a redecide replaced, oldest first, as stored
	Text      string   // the erratum text, metadata lines excluded
}

// Decided reports whether the item carries a decision.
func (it ErrataItem) Decided() bool { return it.DecidedBy != "" }

// StateLabel is the item's state as shown to a reader.
func (it ErrataItem) StateLabel() string {
	if !it.Decided() {
		return "proposta"
	}
	l := fmt.Sprintf("decidida: \"%s\" — %s, %s", it.Quote, it.DecidedBy, it.DecidedAt)
	if len(it.Previous) > 0 {
		l += "; antes: " + strings.Join(it.Previous, "; ")
	}
	return l
}

type itemSpan struct {
	item  ErrataItem
	hdr   int // line index of the block title
	end   int // exclusive
	metaP int // line index of the first metadata line (or where one would go)
	nMeta int
}

func scanErrata(lines []string) []itemSpan {
	var spans []itemSpan
	for i, l := range lines {
		if m := errataHeaderRe.FindStringSubmatch(l); m != nil {
			spans = append(spans, itemSpan{hdr: i, item: ErrataItem{Date: m[1], Author: strings.TrimSpace(m[2])}})
		}
	}
	for k := range spans {
		s := &spans[k]
		s.end = len(lines)
		if k+1 < len(spans) {
			s.end = spans[k+1].hdr
		}
		s.item.ID = fmt.Sprintf("E%d", k+1)
		p := s.hdr + 1
		for p < s.end && strings.TrimSpace(lines[p]) == "" {
			p++
		}
		s.metaP = p
		for p < s.end {
			m := errataMetaRe.FindStringSubmatch(lines[p])
			if m == nil {
				break
			}
			switch v := strings.TrimSpace(m[2]); m[1] {
			case "Altera":
				if v != NoStep {
					s.item.Target = v
				}
			case "Sem passo":
				s.item.Stepless = v == "sim"
			case "Anterior":
				s.item.Previous = append(s.item.Previous, strings.TrimPrefix(v, "decidida — "))
			case "Estado":
				if sm := errataStateRe.FindStringSubmatch(v); sm != nil {
					s.item.Quote, s.item.DecidedBy, s.item.DecidedAt = sm[1], sm[2], sm[3]
				}
			case "Fundida":
				s.item.Merged = v
			}
			s.nMeta++
			p++
		}
		s.item.Text = strings.TrimSpace(strings.Join(lines[p:s.end], "\n"))
	}
	return spans
}

// ParseErrataItems reads the errata items of an errata document.
func ParseErrataItems(content string) []ErrataItem {
	spans := scanErrata(strings.Split(content, "\n"))
	out := make([]ErrataItem, len(spans))
	for i, s := range spans {
		out[i] = s.item
	}
	return out
}

// AddErrataMeta adds one more metadata line to item itemID, keeping any line of
// the same key (SetErrataMeta replaces it): history is appended, never overwritten.
func AddErrataMeta(content, itemID, key, value string) (string, error) {
	lines := strings.Split(content, "\n")
	for _, s := range scanErrata(lines) {
		if s.item.ID != itemID {
			continue
		}
		ins := []string{key + ": " + strings.ReplaceAll(value, "\n", " ")}
		if s.nMeta == 0 {
			ins = append(ins, "")
		}
		at := s.metaP + s.nMeta
		lines = append(lines[:at], append(ins, lines[at:]...)...)
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("errata item %s not found", itemID)
}

// SetErrataMeta sets (or replaces) one metadata line of item itemID in place:
// deciding an item changes its state, it never appends a new erratum.
func SetErrataMeta(content, itemID, key, value string) (string, error) {
	lines := strings.Split(content, "\n")
	for _, s := range scanErrata(lines) {
		if s.item.ID != itemID {
			continue
		}
		line := key + ": " + strings.ReplaceAll(value, "\n", " ")
		for p := s.metaP; p < s.metaP+s.nMeta; p++ {
			if strings.HasPrefix(lines[p], key+": ") {
				lines[p] = line
				return strings.Join(lines, "\n"), nil
			}
		}
		ins := []string{line}
		if s.nMeta == 0 {
			ins = append(ins, "")
		}
		at := s.metaP + s.nMeta
		lines = append(lines[:at], append(ins, lines[at:]...)...)
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("errata item %s not found", itemID)
}

type heading struct {
	idx, level int
	title      string
}

// headingsOutsideFences lists the markdown headings of lines, skipping code
// fences (a `# comment` in a bash block is not a heading).
func headingsOutsideFences(lines []string) []heading {
	var hs []heading
	fence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if m := headingRe.FindStringSubmatch(l); m != nil {
			hs = append(hs, heading{idx: i, level: len(m[1]), title: m[2]})
		}
	}
	return hs
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// stepLabels are the words a real step title may put before its token
// ("Passo S6.6", "Fase 3, passo S8: …"); any other word before it is prose.
var stepLabels = []string{"passo", "step", "etapa"}

var stepNumberingRe = regexp.MustCompile(`^\d+[.)]\s+`)

// hasStepToken reports whether title names step tok as a whole token, in the
// forms the runbooks use: "S6.6. Parada", "Passo S6.6", "**S6.6**", "6. S6.6 — x",
// "S6.5/S6.6" and "Fase 3, passo S6.6: x". S6.1 is not S6.10 nor S6.1.2, and a
// title that only mentions the token in prose ("Notas sobre S6.6") is not the step.
func hasStepToken(title, tok string) bool {
	if startsWithStep(title, tok) {
		return true
	}
	// "Fase 3, passo S8: …": the step may follow one clause, but only with its label.
	if _, rest, ok := strings.Cut(title, ", "); ok {
		if w, r2, _ := strings.Cut(strings.TrimSpace(rest), " "); labelled(w) {
			return startsWithStep(r2, tok)
		}
	}
	return false
}

func labelled(w string) bool {
	for _, l := range stepLabels {
		if strings.EqualFold(w, l) {
			return true
		}
	}
	return false
}

func startsWithStep(title, tok string) bool {
	title = strings.TrimLeft(title, "*_`[ ")
	title = stepNumberingRe.ReplaceAllString(title, "")
	title = strings.TrimLeft(title, "*_`[ ")
	if w, rest, ok := strings.Cut(title, " "); ok && labelled(w) {
		title = strings.TrimLeft(rest, "*_`[ ")
	}
	// the leading run of tokens, split by "/": "S6.5/S6.6"
	for {
		j := 0
		for j < len(title) && (isAlnum(title[j]) || title[j] == '.') {
			j++
		}
		if strings.TrimRight(title[:j], ".") == tok {
			return true
		}
		if j < len(title) && title[j] == '/' {
			title = title[j+1:]
			continue
		}
		return false
	}
}

// stepSectionEnd is the line index where the section of step tok ends (where an
// item for it is inserted), or -1 when no heading names the step.
func stepSectionEnd(lines []string, hs []heading, tok string) int {
	for k, h := range hs {
		if !hasStepToken(h.title, tok) {
			continue
		}
		end := len(lines)
		for _, n := range hs[k+1:] {
			if n.level <= h.level {
				end = n.idx
				break
			}
		}
		for end > h.idx+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		return end
	}
	return -1
}

// StepExists reports whether a heading of body names step: the same matching
// InsertAtSteps places an item with (F-0345).
func StepExists(body, step string) bool {
	lines := strings.Split(body, "\n")
	return stepSectionEnd(lines, headingsOutsideFences(lines), step) >= 0
}

// InsertAtSteps places each item as a quoted block at the end of the section of
// the step it declares, in the body, and returns the items it could not place
// (no declared step, or no section naming it) — those never break the caller.
// status renders the parenthesised state shown on each block.
func InsertAtSteps(body string, items []ErrataItem, status func(ErrataItem) string) (string, []ErrataItem) {
	lines := strings.Split(body, "\n")
	hs := headingsOutsideFences(lines)
	at := map[int][]string{}
	var orphans []ErrataItem
	for _, it := range items {
		pos := -1
		if it.Target != "" {
			pos = stepSectionEnd(lines, hs, it.Target)
		}
		if pos < 0 {
			orphans = append(orphans, it)
			continue
		}
		blk := []string{"", fmt.Sprintf("> **Errata %s** (%s, %s)", it.ID, it.Date, status(it)), ">"}
		for _, l := range strings.Split(it.Text, "\n") {
			blk = append(blk, strings.TrimRight("> "+l, " "))
		}
		at[pos] = append(at[pos], blk...)
	}
	var out []string
	for i := 0; i <= len(lines); i++ {
		out = append(out, at[i]...)
		if i < len(lines) {
			out = append(out, lines[i])
		}
	}
	return strings.Join(out, "\n"), orphans
}

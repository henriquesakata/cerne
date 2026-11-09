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
//
// Blocks written before F-0872 have none of them and parse as proposals with no
// step. The item id is E<n>, by order in the file.

var (
	errataHeaderRe = regexp.MustCompile(`^## errata (\d{4}-\d{2}-\d{2}) — (.+)$`)
	errataMetaRe   = regexp.MustCompile(`^(Altera|Estado|Fundida): (.*)$`)
	errataStateRe  = regexp.MustCompile(`^decidida — "(.*)" \((.+), (\d{4}-\d{2}-\d{2})\)$`)
	headingRe      = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
)

// ErrataItem is one parsed erratum block.
type ErrataItem struct {
	ID        string // E1, E2, … by order in the file
	Date      string // YYYY-MM-DD from the block title
	Author    string
	Target    string // declared step ("S6.6"); "" when none
	Quote     string // the decision, quoted; "" while a proposal
	DecidedBy string
	DecidedAt string
	Merged    string // "" until the merge folds it into the body
	Text      string // the erratum text, metadata lines excluded
}

// Decided reports whether the item carries a decision.
func (it ErrataItem) Decided() bool { return it.DecidedBy != "" }

// StateLabel is the item's state as shown to a reader.
func (it ErrataItem) StateLabel() string {
	if !it.Decided() {
		return "proposta"
	}
	return fmt.Sprintf("decidida: \"%s\" — %s, %s", it.Quote, it.DecidedBy, it.DecidedAt)
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
				s.item.Target = v
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

// hasStepToken reports whether title names step tok as a whole token: S6.1 is
// not in "S6.10" nor "S6.1.2", but is in "S6.1. Um" and "S6.1: um".
func hasStepToken(title, tok string) bool {
	for from := 0; ; {
		i := strings.Index(title[from:], tok)
		if i < 0 {
			return false
		}
		i += from
		j := i + len(tok)
		before := i == 0 || !isAlnum(title[i-1])
		after := j == len(title) || (!isAlnum(title[j]) && !(title[j] == '.' && j+1 < len(title) && title[j+1] >= '0' && title[j+1] <= '9'))
		if before && after {
			return true
		}
		from = i + 1
	}
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
			for k, h := range hs {
				if !hasStepToken(h.title, it.Target) {
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
				pos = end
				break
			}
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

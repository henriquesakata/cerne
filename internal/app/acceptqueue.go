package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0875: the accept queue. Read-only: it never calls Accept (F-0131), only
// summarizes what each result.md already carries, by markers the real results
// repeat (no new format). A field the result does not carry is NotInformed,
// never inferred.

// NotInformed is the literal the spec asks for (F-0875 criterion 3).
const NotInformed = "não informado"

// AcceptRow is one implemented feature waiting for the human's accept.
type AcceptRow struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	ImplementedAt   time.Time `json:"implemented_at"` // the entity's last update, as the stale-ready lint ages it
	Gauntlet        string    `json:"gauntlet"`       // vitória | empate | derrota | dispensada | NotInformed
	Production      string    `json:"production"`     // the first production-evidence line, or NotInformed
	Pending         []string  `json:"pending"`
	PendingReported bool      `json:"pending_reported"` // false = the result declares no pendings section or marker
	AcceptCommand   string    `json:"accept_command"`
}

var (
	// A production line is "<label>:" at the line start (list marker, quote and
	// emphasis allowed); the value after the colon is what counts as evidence.
	prodLineRe = regexp.MustCompile(`(?i)^[\s>*_-]*(?:em produ[cç][aã]o|produ[cç][aã]o|prod)[*_]*\s*:[*_]*(.*)$`)
	// A section whose title is just "Produção": its first body line is the evidence.
	prodHeadRe = regexp.MustCompile(`(?i)^(?:em\s+)?produ[cç][aã]o\s*:?$`)
	// Negative or empty evidence is no evidence: any line led by "não" (verificado,
	// foi, se aplica, houve...), pending or waiting, "n/a", or nothing. Heuristic:
	// "Não houve erro" reads as absent too, the safe side for a decision queue.
	// "na sexta..." and "sem erro nos logs" are positive and stay evidence.
	prodNegRe = regexp.MustCompile(`(?i)^[\s>*_\x60-]*(?:n[aã]o\b|ainda\s+n[aã]o|pendente|aguardando|n/a\b|a\s+(?:verificar|conferir)|tbd\b|sem\s+(?:evid[eê]ncia|registro|dados?|informa)|nada\b)|^[\s*_\x60.—–-]*$`)
	// PENDENTE counts only on an acceptance line (list item or table row that
	// names a "critério"), never in loose prose.
	pendingWord = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)]|\|).*crit[ée]rio.*\bPENDENTE\b`)
	pendingHead = regexp.MustCompile(`(?i)^(?:d[ée]bitos|n[aã]o verificado)\b`)
	headingRe   = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	bulletRe    = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+(.*)$`)
	// "Nenhum.", "Nenhum débito aberto além de..." and "n/a" declare no debt; "Nenhum
	// teste de carga foi escrito" is a debt (the word after "nenhum" is not a debt noun).
	noneRe       = regexp.MustCompile(`(?i)^(?:(?:nenhum[ao]?s?|n[aã]o h[aá](?:\s+nenhum[ao]?s?)?)(?:\s+(?:d[ée]bitos?|pend[êe]ncias?|follow-?ups?|itens?|pontos?|problemas?|limita[cç][õo]es|limita[cç][ãa]o)\b|\s*[;:,.]|\s*$)|(?:n/?a|none)\b)|^[—–.\s]*$`)
	markdownJunk = regexp.MustCompile("[*_`>#]|^[\\s-]+")
)

// AcceptQueue lists the features in implemented, oldest first (ties by id).
func AcceptQueue(repoDir string) ([]AcceptRow, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	rows := []AcceptRow{} // empty queue is [] in the JSON, never null
	for _, f := range features {
		if f.State != domain.StateImplemented {
			continue
		}
		result, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", f.ID, "result.md")) // missing result: every field is NotInformed
		row := summarizeResult(result)
		row.ID, row.Title, row.ImplementedAt = f.ID, f.Title, f.UpdatedAt
		row.AcceptCommand = "cerne feature accept " + f.ID
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b AcceptRow) int {
		if c := a.ImplementedAt.Compare(b.ImplementedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return rows, nil
}

func summarizeResult(result []byte) AcceptRow {
	row := AcceptRow{Gauntlet: NotInformed, Production: NotInformed, Pending: []string{}}
	if v, ok := lint.GauntletVerdict(result); ok {
		row.Gauntlet = v
	}
	inSection, secLevel, base, prodSect, fence := false, 0, -1, false, false
	// Débitos counts as reported only with a bullet list or an explicit "Nenhum":
	// a table or prose is no list, and "0" would affirm there are no debts.
	bullets := false
	for _, line := range strings.Split(string(lint.StripHTMLComments(result)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if fence { // code is output, not a declaration
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			if inSection && level <= secLevel { // only a heading of the same or higher level ends the section
				inSection = false
			}
			if !inSection && level >= 2 && pendingHead.MatchString(m[2]) {
				inSection, secLevel, base = true, level, -1
			}
			prodSect = prodHeadRe.MatchString(strings.TrimSpace(m[2]))
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if row.Production == NotInformed {
			if prodSect {
				prodSect = false
				if !prodNegRe.MatchString(line) {
					row.Production = clip(line)
				}
			} else if m := prodLineRe.FindStringSubmatch(line); m != nil && !prodNegRe.MatchString(m[1]) {
				row.Production = clip(line)
			}
		}
		if pendingWord.MatchString(line) {
			row.PendingReported = true
			row.Pending = append(row.Pending, clip(line))
		} else if m := bulletRe.FindStringSubmatch(line); inSection && m != nil {
			// Only first-level bullets count: prose, sub-bullets and tables do not.
			bullets = true
			indent := len(strings.ReplaceAll(m[1], "\t", "    "))
			if base < 0 {
				base = indent
			}
			if t := clip(m[2]); indent <= base && !noneRe.MatchString(t) {
				row.Pending = append(row.Pending, t)
			}
		} else if inSection && noneRe.MatchString(clip(line)) {
			bullets = true // a prose "Nenhum." is an explicit declaration of zero
		}
	}
	row.PendingReported = row.PendingReported || bullets
	return row
}

func clip(s string) string {
	s = strings.TrimSpace(markdownJunk.ReplaceAllString(s, ""))
	if r := []rune(s); len(r) > 120 {
		s = string(r[:117]) + "..."
	}
	return s
}

// FormatAcceptRow renders one row of the text output.
func FormatAcceptRow(r AcceptRow) string {
	pend := NotInformed
	if r.PendingReported {
		pend = fmt.Sprintf("%d", len(r.Pending))
		if len(r.Pending) > 0 {
			pend += ": " + strings.Join(r.Pending, " | ")
		}
	}
	return fmt.Sprintf("%s  %s  %s\n  gauntlet: %s\n  production: %s\n  pending: %s\n",
		r.ID, r.ImplementedAt.Format("2006-01-02"), r.Title, r.Gauntlet, r.Production, pend)
}

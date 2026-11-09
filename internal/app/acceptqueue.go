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
	prodLineRe   = regexp.MustCompile(`(?i)^[\s>*_-]*(?:em produ[cç][aã]o|produ[cç][aã]o|prod)\b[^\n]*:`)
	prodHeadRe   = regexp.MustCompile(`(?i)^##+\s[^\n]*produ[cç][aã]o`)
	pendingWord  = regexp.MustCompile(`\bPENDENTE\b`)
	pendingHead  = regexp.MustCompile(`(?i)^##\s+(?:d[ée]bitos|n[aã]o verificado)\b`)
	bulletRe     = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+(.*)$`)
	markdownJunk = regexp.MustCompile("[*_`>#]|^[\\s-]+")
)

// AcceptQueue lists the features in implemented, oldest first (ties by id).
func AcceptQueue(repoDir string) ([]AcceptRow, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	var rows []AcceptRow
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
	inSection := false
	for _, line := range strings.Split(string(result), "\n") {
		switch {
		case strings.HasPrefix(line, "#"):
			inSection = pendingHead.MatchString(line)
			row.PendingReported = row.PendingReported || inSection
			if row.Production == NotInformed && prodHeadRe.MatchString(line) {
				row.Production = clip(line)
			}
		case row.Production == NotInformed && prodLineRe.MatchString(line):
			row.Production = clip(line)
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if pendingWord.MatchString(line) {
			row.PendingReported = true
			row.Pending = append(row.Pending, clip(line))
		} else if inSection {
			if m := bulletRe.FindStringSubmatch(line); m != nil {
				row.Pending = append(row.Pending, clip(m[1]))
			} else if t := strings.TrimSpace(line); !regexp.MustCompile(`(?i)^nenhum`).MatchString(t) && len(row.Pending) == 0 {
				row.Pending = append(row.Pending, clip(t)) // prose without bullets: the paragraph is the one pending
			}
		}
	}
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

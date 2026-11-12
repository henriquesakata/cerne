package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// RuleSpecCLILiteralPT flags a numbered acceptance criterion that quotes a CLI
// output literal reading pt-BR, in a project whose CLI prints English
// (F-0353). Advisory; the caller runs it only when lint.cli_output_language is
// "en", so the lint package never reads settings.
const RuleSpecCLILiteralPT = "spec-cli-literal-pt"

// The fix a finding ends with: where (the line) and what (the literal) come
// first; editing an approved spec stales its approval, so that state says when.
const (
	cliLiteralFixCandidate = " — rewrite the message in English in the criterion (CLI output is English), then 'cerne push'"
	cliLiteralFixApproved  = " — rewrite the message in English in the criterion (CLI output is English); editing an approved spec stales the approval — fix it right after 'cerne feature start'"
)

var (
	backtickSpanRe = regexp.MustCompile("`([^`]+)`")
	// outputTermRe: an output term (criterion 2) must sit right before the
	// literal: only spaces, ":", "," and the listed bridges — "no stderr",
	// "no stdout", "na saída", "a mensagem", "com" and the abbreviations
	// "p. ex.", "ex.", "e.g." (their period is no sentence end) — may come
	// between the term's end and the backtick. A real sentence end between them
	// breaks the match, so no separate sentence cut is needed. Matched only in
	// the prose, never inside backticks.
	outputTermRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:imprime|imprimir|recusa com|mensagem|erro|stderr|stdout|saída)(?:[\s:,]+(?:no stderr|no stdout|na saída|a mensagem|com|p\. ?ex\.|ex\.|e\.g\.))*[\s:,]*$`)
)

// SpecCLILiteralPT returns the spec-cli-literal-pt findings of one spec: every
// backticked literal inside a numbered criterion (the item and its indented
// continuation lines, HTML-comment guidance skipped) that is a CLI output — it
// starts with "cerne:" or follows an output term right before it (outputTermRe) — is not
// a command, path or identifier, and reads pt-BR to doccheck.PTReason, the
// detector the runtime-output guard uses.
func SpecCLILiteralPT(id string, approved bool, spec []byte) []Finding {
	var out []Finding
	var item []criterionLine
	flush := func() {
		out = append(out, itemFindings(id, approved, item)...)
		item = nil
	}
	inComment := false
	for _, l := range criteriaLines(spec) {
		t := strings.TrimSpace(l.Text)
		switch {
		case inComment:
			inComment = !strings.Contains(t, "-->")
			continue
		case strings.HasPrefix(t, "<!--"):
			inComment = !strings.Contains(t, "-->")
			continue
		}
		// A criterion is its numbered line plus the indented lines after it; a
		// blank line or any other line ends it (A2: the Barra:/Saída: block that
		// follows the list is not a criterion).
		switch {
		case numberedItemRe.MatchString(l.Text):
			flush()
			item = []criterionLine{l}
		case item != nil && t != "" && (strings.HasPrefix(l.Text, " ") || strings.HasPrefix(l.Text, "\t")):
			item = append(item, l)
		default:
			flush()
		}
	}
	flush()
	return out
}

// itemFindings scans one criterion — its lines joined into one text, so a term
// at the end of a line still reaches the literal on the next (A3) — and
// reports each finding on the file line that holds the literal.
func itemFindings(id string, approved bool, item []criterionLine) []Finding {
	var out []Finding
	var text strings.Builder
	var starts []int // offset of each line in text
	for _, l := range item {
		starts = append(starts, text.Len())
		text.WriteString(l.Text)
		text.WriteByte(' ')
	}
	joined := text.String()
	fix := cliLiteralFixCandidate
	if approved {
		fix = cliLiteralFixApproved
	}
	for _, m := range backtickSpanRe.FindAllStringSubmatchIndex(joined, -1) {
		lit := joined[m[2]:m[3]]
		if !isCLIOutput(joined[:m[0]], lit) || isCommandPathOrIdentifier(lit) {
			continue
		}
		if why := doccheck.PTReason(lit); why != "" {
			n := item[0].N
			for i, st := range starts {
				if st <= m[0] {
					n = item[i].N
				}
			}
			out = append(out, Finding{
				Rule: RuleSpecCLILiteralPT, Entity: id, File: "content/features/" + id + "/spec.md",
				Detail: fmt.Sprintf("line %d: CLI output literal reads pt-BR (%s): %s%s", n, why, lit, fix),
			})
		}
	}
	return out
}

// isCLIOutput: lit starts with "cerne:", or an output term sits right before
// it (outputTermRe). before is the text up to the literal's opening backtick;
// earlier backtick spans are blanked so a term quoted there is not a term.
func isCLIOutput(before, lit string) bool {
	if strings.HasPrefix(lit, "cerne:") {
		return true
	}
	return outputTermRe.MatchString(backtickSpanRe.ReplaceAllString(before, " "))
}

// isCommandPathOrIdentifier: a literal that is a cerne command, or one token
// that carries a path or identifier character (criterion 4).
func isCommandPathOrIdentifier(lit string) bool {
	if strings.HasPrefix(lit, "cerne ") {
		return true
	}
	return !strings.ContainsAny(lit, " \t") && strings.ContainsAny(lit, `/\_.-:`)
}

// SpecCLILiteralFindings runs SpecCLILiteralPT over every candidate and
// approved feature's spec in the backend at repoDir.
func SpecCLILiteralFindings(repoDir string) ([]Finding, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, f := range features {
		if f.State != domain.StateCandidate && f.State != domain.StateApproved {
			continue
		}
		spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", f.ID, "spec.md"))
		if err != nil {
			continue // no spec yet: nothing to read (specs are optional until the gate)
		}
		out = append(out, SpecCLILiteralPT(f.ID, f.State == domain.StateApproved, spec)...)
	}
	return out, nil
}

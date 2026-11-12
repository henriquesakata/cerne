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

var (
	backtickSpanRe = regexp.MustCompile("`([^`]+)`")
	// outputTermRe: the words that make the literal after them a CLI output
	// (criterion 2). Matched only in the prose, never inside backticks.
	outputTermRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:imprime|imprimir|recusa com|mensagem|erro|stderr|stdout|saída)(?:[^\p{L}]|$)`)
	// sentenceEndRe: a sentence ends at . ! ? followed by a space — "1. " of a
	// numbered item included, so the term must sit in the literal's own sentence.
	sentenceEndRe = regexp.MustCompile(`[.!?]\s`)
)

// SpecCLILiteralPT returns the spec-cli-literal-pt findings of one spec: every
// backticked literal inside a numbered criterion (the lines from the first
// numbered item on, HTML-comment guidance skipped) that is a CLI output — it
// starts with "cerne:" or follows an output term in the same sentence — is not
// a command, path or identifier, and reads pt-BR to doccheck.PTReason, the
// detector the runtime-output guard uses.
func SpecCLILiteralPT(id string, spec []byte) []Finding {
	var out []Finding
	rel := "content/features/" + id + "/spec.md"
	started, inComment := false, false
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
		if numberedItemRe.MatchString(l.Text) {
			started = true
		}
		if !started {
			continue
		}
		for _, m := range backtickSpanRe.FindAllStringSubmatchIndex(l.Text, -1) {
			lit := l.Text[m[2]:m[3]]
			if !isCLIOutput(l.Text[:m[0]], lit) || isCommandPathOrIdentifier(lit) {
				continue
			}
			if why := doccheck.PTReason(lit); why != "" {
				out = append(out, Finding{
					Rule: RuleSpecCLILiteralPT, Entity: id, File: rel,
					Detail: fmt.Sprintf("line %d: CLI output literal reads pt-BR (%s): %s", l.N, why, lit),
				})
			}
		}
	}
	return out
}

// isCLIOutput: lit starts with "cerne:", or an output term sits in its own
// sentence before it. before is the line up to the literal's opening backtick;
// earlier backtick spans are blanked so a term quoted there is not a term.
func isCLIOutput(before, lit string) bool {
	if strings.HasPrefix(lit, "cerne:") {
		return true
	}
	before = backtickSpanRe.ReplaceAllString(before, " ")
	if ends := sentenceEndRe.FindAllStringIndex(before, -1); len(ends) > 0 {
		before = before[ends[len(ends)-1][1]:]
	}
	return outputTermRe.MatchString(before)
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
		out = append(out, SpecCLILiteralPT(f.ID, spec)...)
	}
	return out, nil
}

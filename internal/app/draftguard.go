package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotADocument marks a draft response that is not the document: the model
// answered with something else — most often its opening remarks about what it
// is going to do — and the caller must refuse to stamp it as the spec, plan,
// result, ADR or runbook. The draft-side twin of compile.ErrNotAPage (F-0287).
var ErrNotADocument = errors.New("draft response is not a document")

// minDraftBodyRunes is the floor a drafted document must clear. It is set well
// below any real spec/plan/result (kilobytes each) and above the agent preamble
// that motivated F-0290 — 171 bytes, two lines. The point is to catch a
// category error, not to judge how thorough a document is.
const minDraftBodyRunes = 200

// DraftOutputContract is the output contract every document-draft prompt states,
// the same one BuildPagePrompt carries (F-0287). It is pt-BR on purpose: these
// prompts set the language of the generated document, not of the CLI chrome.
// Exported and reused verbatim — never copied — so the five draft prompts never
// drift, exactly like GauntletContract.
const DraftOutputContract = "Devolva SOMENTE o markdown do documento na saída padrão. " +
	"Não escreva arquivos, não use ferramentas e não relate o que você fez nem o que vai fazer."

// ValidateDraftBody reports whether body may be stamped as an authored document,
// per the contract DraftOutputContract states. It exists because the response
// comes back from a Claude Code session, which can answer with prose about what
// it is doing instead of the artifact — that is exactly what shipped into
// content/features/F-0287/result.md before F-0290.
//
// Two deterministic rules, both loose on purpose (a false positive only leaves
// the slot empty, as it already is when a draft fails; a false negative
// corrupts the document): the body must contain a markdown heading, and it must
// clear minDraftBodyRunes. Unlike compile.ValidatePageBody the heading need not
// be on the first line — a drafted spec legitimately opens with the template's
// **Tipo:** / **Origem:** / **Nível:** metadata lines, and frontmatter the model
// volunteered is harmless here (cerne stamps its own on write anyway).
func ValidateDraftBody(body string) error {
	text := strings.TrimSpace(body)
	if text == "" {
		return fmt.Errorf("%w: empty response", ErrNotADocument)
	}
	if !hasHeading(text) {
		first, _, _ := strings.Cut(text, "\n")
		return fmt.Errorf("%w: no markdown heading, it starts with %q", ErrNotADocument, truncateDraftRunes(first, 60))
	}
	if n := len([]rune(text)); n < minDraftBodyRunes {
		return fmt.Errorf("%w: %d characters is too short to be a document", ErrNotADocument, n)
	}
	return nil
}

// WriteStampedDraft is the single point where a drafted document is written:
// it validates the body, then writes path as cerne's stamped cerne_id
// frontmatter followed by the model's trimmed body. cerne stamps the
// frontmatter itself — the model is never trusted to emit cerne_id. A refused
// draft names the document it would have overwritten and leaves the file
// untouched.
func WriteStampedDraft(path, id, body string) error {
	if err := ValidateDraftBody(body); err != nil {
		return fmt.Errorf("refusing to write %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// hasHeading reports whether text carries an ATX markdown heading ("## Título")
// on any line — the shape every authored document is built from.
func hasHeading(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		rest := strings.TrimLeft(trimmed, "#")
		if rest != trimmed && strings.HasPrefix(rest, " ") {
			return true
		}
	}
	return false
}

// truncateDraftRunes bounds a quoted excerpt in an error message.
func truncateDraftRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoEntityDoc is returned by DriftDraftPrompt when the entity has neither a
// result.md nor a spec.md to ground the errata draft on.
var ErrNoEntityDoc = errors.New("entity has no document to draft from")

// DriftDraftPrompt builds the delegation prompt for a drift-suspect finding
// (F-0114): the host agent reads it and writes an errata draft. It is pure —
// it only reads the entity's current doc from the backend and returns text,
// never calling an LLM (cerne's default runtime mode is delegation) and never
// writing a file. The prompt carries the changed paths, the short baseline, the
// entity's current doc, and the exact command to apply the drafted errata after
// review (D-0004: the prose is drafted by the agent and gated by the human,
// never auto-derived from code).
func DriftDraftPrompt(repoDir, entityID, baseline string, changed []string) (string, error) {
	doc, err := loadEntityDoc(repoDir, entityID)
	if err != nil {
		return "", err
	}
	short := baseline
	if len(short) > 7 {
		short = short[:7]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- cerne drift-draft for %s — write the errata in .docs/.drift-proposals/%s.md -->\n\n", entityID, entityID)
	fmt.Fprintf(&b, "Context: %s is implemented; its module's code has changed since %s.\n\n", entityID, short)
	b.WriteString("Changed files:\n")
	for _, p := range changed {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	b.WriteString("\nEntity's current document:\n---\n")
	b.WriteString(doc)
	if !strings.HasSuffix(doc, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	b.WriteString("Task: write a concise errata — what the code changed, what the doc said, what now holds. ")
	b.WriteString("Do not rewrite the document; the errata is a dated addendum.\n\n")
	fmt.Fprintf(&b, "To apply after review:\ncerne feature errata %s --file .docs/.drift-proposals/%s.md\n", entityID, entityID)
	return b.String(), nil
}

// loadEntityDoc returns the entity's current authored doc, preferring result.md
// (what shipped) and falling back to spec.md. ErrNoEntityDoc when neither exists.
func loadEntityDoc(repoDir, entityID string) (string, error) {
	base := filepath.Join(repoDir, "content", "features", entityID)
	for _, name := range []string{"result.md", "spec.md"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err == nil {
			return string(data), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNoEntityDoc, entityID)
}

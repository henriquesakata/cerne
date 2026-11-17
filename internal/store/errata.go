package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/henriquesakata/cerne/internal/domain"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// errataPath is the backend path of an entity's errata file.
func errataPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "errata", id+".md")
}

// ApprovedSpecPath is the backend path of the spec as feature id's last approve
// saw it (F-0318), byte for byte: content/approved/<id>/spec.md. It lives
// outside content/features/ on purpose — lint, the materializer and the index
// read only the live documents, never this copy.
func ApprovedSpecPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "approved", id, "spec.md")
}

// LoadErrata reads an entity's errata document (content/errata/<id>.md). A
// missing file is "no errata", not an error — most entities have none.
func LoadErrata(repoDir, id string) (string, error) {
	data, err := os.ReadFile(errataPath(repoDir, id))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// AppendErrata appends one dated errata entry for id, creating the file (with
// cerne_id frontmatter) on first write. It is strictly additive — it never reads
// or rewrites earlier entries or any other document — so concluded entities stay
// immutable while still being correctable through the cerne flow.
func AppendErrata(repoDir, id, text, author string, at time.Time) error {
	return appendEntry(errataPath(repoDir, id), id, "errata", text, author, at)
}

// handoffPath is the backend path of a work's session handoffs (F-0339).
func handoffPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "works", id, "handoff.md")
}

// appendEntry appends one `## <kind> <date> — <author>` block to path,
// creating the file (with cerne_id frontmatter) on first write. The text's
// forgeable headings are neutralized (defangHeadings).
func appendEntry(path, id, kind, text, author string, at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	author = domain.ActorLabel(author) // F-0442: no login@host in a stored author
	var entry string
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		entry = "---\ncerne_id: " + id + "\n---\n"
	} else if err != nil {
		return err
	}
	entry += fmt.Sprintf("\n## %s %s — %s\n\n%s\n", kind, at.Format("2006-01-02"), author, defangHeadings(text))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

// defangHeadings escapes the lines of free text that the parser would read as an
// erratum block title (`## errata 2026-11-09 — x` -> `\## errata ...`): such a
// line would otherwise open a forged item (F-0872). Other headings stay as
// written: errata texts carry real sections ("## Gauntlet") that lint reads.
func defangHeadings(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if t := strings.TrimRight(l, "\r"); errataHeaderRe.MatchString(t) || handoffHeaderRe.MatchString(t) {
			lines[i] = "\\" + l
		}
	}
	return strings.Join(lines, "\n")
}

const (
	errataBlockBegin = "<!-- cerne:errata:begin"
	errataBlockEnd   = "<!-- cerne:errata:end -->"
)

// ErrataBlock renders an entity's errata as the generated, read-only block the
// .docs/ projection appends to its file (B-0012). errata is the document body
// without its frontmatter; merged, when non-nil, replaces each already folded
// item by one line (F-0351). It returns "" when there is nothing to show. The
// block is set off by a blank line and ends the file; the projection records its
// exact text (SaveErrataBlocks) so StripErrataBlock removes that and nothing else.
func ErrataBlock(errata string, merged func(ErrataItem) string) string {
	errata = strings.TrimSpace(errata)
	if errata == "" {
		return ""
	}
	if merged != nil {
		errata = ReplaceMerged(errata, merged)
	}
	// a quoted marker must not read as ours: defang it, as headings are
	errata = strings.ReplaceAll(errata, "cerne:errata:", "cerne-errata:")
	return "\n\n" + errataBlockBegin + " — generated from content/errata, read-only: edits here are dropped on push -->\n" +
		"## Errata\n\n" + errata + "\n" + errataBlockEnd + "\n"
}

// ErrBrokenErrataBlock: a document that was projected with an errata block no
// longer holds it verbatim but still carries an errata marker — push refuses it
// rather than send generated text as body.
var ErrBrokenErrataBlock = errors.New("errata block altered")

// errataBlocksFile is the sidecar under .docs/ that records, per materialized
// document, the exact block the pull appended to it (absent: none recorded).
const errataBlocksFile = ".errata-blocks.json"

// SaveErrataBlocks records the blocks of a materialization (rel -> exact text).
// Nothing is written when no document carries one.
func SaveErrataBlocks(docsDir string, blocks map[string]string) error {
	if len(blocks) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(blocks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(docsDir, errataBlocksFile), append(data, '\n'), 0o644)
}

// LoadErrataBlocks reads the sidecar; a missing one is "no block recorded".
func LoadErrataBlocks(docsDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(docsDir, errataBlocksFile))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// StripErrataBlock gives back body without the block the projection appended,
// matched by its recorded text — never guessed. With no block recorded the body
// is returned untouched (an authored marker is the author's). Otherwise one
// verbatim occurrence is cut (CRLF-converted files are matched after LF
// normalization; a block whose final newline an editor dropped at end of file
// still matches) and everything else is kept. A block not found verbatim while a
// line-start errata marker remains is ErrBrokenErrataBlock (fail closed); one
// removed entirely leaves nothing to strip.
func StripErrataBlock(body []byte, block string) ([]byte, error) {
	if block == "" {
		return body, nil
	}
	cut := func(b []byte) ([]byte, bool) {
		for _, cand := range []string{block, strings.TrimSuffix(block, "\n")} {
			i := bytes.Index(b, []byte(cand))
			if i < 0 || (cand != block && !bytes.HasSuffix(b, []byte(cand))) {
				continue
			}
			return append(append([]byte{}, b[:i]...), b[i+len(cand):]...), true
		}
		return nil, false
	}
	if out, ok := cut(body); ok {
		return out, nil
	}
	norm := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	if out, ok := cut(norm); ok {
		return out, nil
	}
	for _, l := range bytes.Split(norm, []byte("\n")) {
		if bytes.HasPrefix(l, []byte("<!-- cerne:errata:")) {
			return nil, ErrBrokenErrataBlock
		}
	}
	return body, nil
}

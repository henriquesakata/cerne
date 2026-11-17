package store

import (
	"bytes"
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
// block is set off by a blank line and ends the file, so StripErrataBlock can
// give the entity body back byte for byte.
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

// ErrBrokenErrataBlock: a file carries the errata begin marker but no block
// that closes it — push refuses it rather than send generated text as body.
var ErrBrokenErrataBlock = errors.New("errata block opened but not closed")

// StripErrataBlock removes the last generated block from body: from the begin
// marker (at the start of a line) through the end marker's line, plus the blank
// line ErrataBlock put before it. Text before and after is kept, so a trailing
// note, CRLF line ends or a missing final newline do not defeat it. A body with
// no begin marker is returned untouched; a begin marker with no end marker
// after it is ErrBrokenErrataBlock (fail closed).
func StripErrataBlock(body []byte) ([]byte, error) {
	begin := -1
	for from := 0; ; {
		j := bytes.Index(body[from:], []byte(errataBlockBegin))
		if j < 0 {
			break
		}
		at := from + j
		if at == 0 || body[at-1] == '\n' {
			begin = at
		}
		from = at + len(errataBlockBegin)
	}
	if begin < 0 {
		return body, nil
	}
	e := bytes.Index(body[begin:], []byte(errataBlockEnd))
	if e < 0 {
		return nil, fmt.Errorf("%w", ErrBrokenErrataBlock)
	}
	after := begin + e + len(errataBlockEnd)
	if k := bytes.IndexByte(body[after:], '\n'); k >= 0 {
		after += k + 1
	} else {
		after = len(body)
	}
	before := body[:begin]
	for _, sep := range []string{"\r\n\r\n", "\n\n"} {
		if bytes.HasSuffix(before, []byte(sep)) {
			before = before[:len(before)-len(sep)]
			break
		}
	}
	return append(append([]byte{}, before...), body[after:]...), nil
}

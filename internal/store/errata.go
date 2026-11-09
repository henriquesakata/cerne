package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	path := errataPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var entry string
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		entry = "---\ncerne_id: " + id + "\n---\n"
	} else if err != nil {
		return err
	}
	entry += fmt.Sprintf("\n## errata %s — %s\n\n%s\n", at.Format("2006-01-02"), author, defangHeadings(text))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

var headingLineRe = regexp.MustCompile(`(?m)^( {0,3})(#{1,6})([ \t]|$)`)

// defangHeadings escapes the Markdown headings of free text (`## x` -> `\## x`):
// a heading line of the text would otherwise open a block the parser reads as
// a new erratum item (F-0872). It renders the same, minus the heading.
func defangHeadings(text string) string {
	return headingLineRe.ReplaceAllString(text, `$1\$2$3`)
}

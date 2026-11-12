package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Runbook revisions (F-0872). The live body (content/runbooks/<id>.md) is the
// newest revision. Each merge of errata first copies the body it replaces to
// content/revisions/<id>/r<N>.md, so r1..rK are the bodies before the K merges
// and the live body is revision K+1. The directory sits outside what lint, the
// materializer and the index read (like content/approved/), and files are never
// rewritten: the history stays as it was. A feature's spec and plan keep their
// replaced bodies the same way, one level down — content/revisions/<id>/<doc>/
// (F-0338, the amend of an approved or in-development feature).

func revisionsDir(repoDir, id, doc string) string {
	return filepath.Join(repoDir, "content", "revisions", id, doc)
}

// SaveErrata writes an entity's errata document back whole (the in-place
// decide of an item); AppendErrata stays the only way to add one.
func SaveErrata(repoDir, id, content string) error {
	return os.WriteFile(errataPath(repoDir, id), []byte(content), 0o644)
}

// SaveRunbookContent writes a runbook's live markdown body.
func SaveRunbookContent(repoDir, id, body string) error {
	return os.WriteFile(filepath.Join(repoDir, "content", "runbooks", id+".md"), []byte(body), 0o644)
}

// CountRevisions is K, the number of archived (replaced) bodies of runbook id.
func CountRevisions(repoDir, id string) (int, error) { return CountDocRevisions(repoDir, id, "") }

// ArchiveRevision stores a runbook body as its next archived revision.
func ArchiveRevision(repoDir, id, body string) (int, error) {
	return ArchiveDocRevision(repoDir, id, "", body)
}

// LoadRevision reads revision n of runbook id.
func LoadRevision(repoDir, id string, n int) (string, error) {
	return LoadDocRevision(repoDir, id, "", n)
}

// CountDocRevisions is K, the number of archived (replaced) bodies of document
// doc of entity id ("" for a runbook's body).
func CountDocRevisions(repoDir, id, doc string) (int, error) {
	ents, err := os.ReadDir(revisionsDir(repoDir, id, doc))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range ents {
		if v, ok := strings.CutSuffix(strings.TrimPrefix(e.Name(), "r"), ".md"); ok {
			if k, err := strconv.Atoi(v); err == nil && k > n {
				n = k
			}
		}
	}
	return n, nil
}

// ArchiveDocRevision stores body as the next archived revision of document doc
// of entity id and returns its number. It refuses to overwrite: history is
// append-only.
func ArchiveDocRevision(repoDir, id, doc, body string) (int, error) {
	k, err := CountDocRevisions(repoDir, id, doc)
	if err != nil {
		return 0, err
	}
	dir := revisionsDir(repoDir, id, doc)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("r%d.md", k+1)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		return 0, err
	}
	return k + 1, nil
}

// LoadDocRevision reads revision n of document doc of entity id: an archived
// body for n <= K, the live one for n == K+1 — a runbook's body when doc is "",
// else content/features/<id>/<doc>.md.
func LoadDocRevision(repoDir, id, doc string, n int) (string, error) {
	k, err := CountDocRevisions(repoDir, id, doc)
	if err != nil {
		return "", err
	}
	switch {
	case n >= 1 && n <= k:
		data, err := os.ReadFile(filepath.Join(revisionsDir(repoDir, id, doc), fmt.Sprintf("r%d.md", n)))
		return string(data), err
	case n == k+1 && doc == "":
		return LoadRunbookContent(repoDir, id)
	case n == k+1:
		data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, doc+".md"))
		return string(data), err
	}
	return "", fmt.Errorf("%s has revisions 1..%d, no revision %d", strings.TrimSpace(id+" "+doc), k+1, n)
}

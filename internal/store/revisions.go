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
// rewritten: the history stays as it was.

func revisionsDir(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "revisions", id)
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
func CountRevisions(repoDir, id string) (int, error) {
	ents, err := os.ReadDir(revisionsDir(repoDir, id))
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

// ArchiveRevision stores body as the next archived revision and returns its
// number. It refuses to overwrite: history is append-only.
func ArchiveRevision(repoDir, id, body string) (int, error) {
	k, err := CountRevisions(repoDir, id)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(revisionsDir(repoDir, id), 0o755); err != nil {
		return 0, err
	}
	path := filepath.Join(revisionsDir(repoDir, id), fmt.Sprintf("r%d.md", k+1))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		return 0, err
	}
	return k + 1, nil
}

// LoadRevision reads revision n of runbook id: an archived body for n <= K, the
// live body for n == K+1.
func LoadRevision(repoDir, id string, n int) (string, error) {
	k, err := CountRevisions(repoDir, id)
	if err != nil {
		return "", err
	}
	switch {
	case n >= 1 && n <= k:
		data, err := os.ReadFile(filepath.Join(revisionsDir(repoDir, id), fmt.Sprintf("r%d.md", n)))
		return string(data), err
	case n == k+1:
		return LoadRunbookContent(repoDir, id)
	}
	return "", fmt.Errorf("%s has revisions 1..%d, no revision %d", id, k+1, n)
}

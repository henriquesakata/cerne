package app

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// notesSeenSubdir holds, per session, the note ids the work-gate already showed
// (<sess>, one per line; F-0366).
const notesSeenSubdir = "notes-seen"

// SeenNotes returns the note ids already shown in session sess. An unsafe
// session id or an unreadable file reads as nothing seen.
func SeenNotes(cacheDir, sess string) map[string]bool {
	seen := map[string]bool{}
	if safeSessionID(sess) != nil {
		return seen
	}
	f, err := os.Open(filepath.Join(cacheDir, notesSeenSubdir, sess))
	if err != nil {
		return seen
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if id := strings.TrimSpace(sc.Text()); id != "" {
			seen[id] = true
		}
	}
	return seen
}

// MarkNotesSeen appends ids to session sess's shown list.
func MarkNotesSeen(cacheDir, sess string, ids []string) error {
	if err := safeSessionID(sess); err != nil {
		return err
	}
	dir := filepath.Join(cacheDir, notesSeenSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, sess), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(strings.Join(ids, "\n") + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	pruneSeen(dir)
	return werr
}

// uniqueBudget bounds the one git call that decides whether a bare file name
// is unique; past it the name-only citations are dropped, path ones still show.
var uniqueBudget = 50 * time.Millisecond // var: tests with a fake git need slack to exec it

// UniqueNames returns whether a bare file name is tracked exactly once in the
// repository at repoRoot, counted with a pathspec for that one name (no full
// listing). Any git failure or timeout answers false.
func UniqueNames(ctx context.Context, repoRoot string) func(string) bool {
	return func(name string) bool {
		ctx, cancel := context.WithTimeout(ctx, uniqueBudget)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-files", "-z", "--",
			":(glob)**/"+globEscaper.Replace(name)).Output()
		if err != nil {
			return false
		}
		n := 0
		for _, p := range bytes.Split(out, []byte{0}) {
			if len(p) > 0 && path.Base(string(p)) == name {
				n++
			}
		}
		return n == 1
	}
}

var globEscaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`)

// pruneSeen removes the shown-lists of sessions untouched for a week.
func pruneSeen(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, e := range ents {
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

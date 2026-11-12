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
)

// notesSeenSubdir holds, per session, the note ids the work-gate already showed
// (<sess>, one per line) and the repository's tracked-file listing
// (<sess>.names, git ls-files -z output) so git runs once per session (F-0366).
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
	return werr
}

// UniqueNames returns whether a bare file name is tracked exactly once in the
// repository at repoRoot. The `git ls-files -z` listing is cached per session;
// with no safe session it runs once per call and caches nothing. A git failure
// yields a func that is always false (name-only citations are then ignored).
func UniqueNames(ctx context.Context, cacheDir, sess, repoRoot string) func(string) bool {
	var listing []byte
	cache := ""
	if safeSessionID(sess) == nil {
		cache = filepath.Join(cacheDir, notesSeenSubdir, sess+".names")
		listing, _ = os.ReadFile(cache)
	}
	if listing == nil {
		out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-files", "-z").Output()
		if err != nil {
			return func(string) bool { return false }
		}
		listing = out
		if cache != "" && os.MkdirAll(filepath.Dir(cache), 0o755) == nil {
			_ = os.WriteFile(cache, listing, 0o644)
		}
	}
	count := map[string]int{}
	for _, p := range bytes.Split(listing, []byte{0}) {
		if len(p) > 0 {
			count[path.Base(string(p))]++
		}
	}
	return func(name string) bool { return count[name] == 1 }
}

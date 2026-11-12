package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func lockIndexExclusive(t *testing.T, dbPath string) func() {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	return func() { _, _ = db.Exec("ROLLBACK"); db.Close() }
}

func TestNotesSeenPerSession(t *testing.T) {
	c := t.TempDir()
	if len(SeenNotes(c, "s1")) != 0 {
		t.Fatal("fresh session must see nothing")
	}
	if err := MarkNotesSeen(c, "s1", []string{"N-0001", "N-0002"}); err != nil {
		t.Fatal(err)
	}
	if err := MarkNotesSeen(c, "s1", []string{"N-0003"}); err != nil {
		t.Fatal(err)
	}
	s := SeenNotes(c, "s1")
	if !s["N-0001"] || !s["N-0002"] || !s["N-0003"] || len(s) != 3 {
		t.Fatalf("seen = %v", s)
	}
	if len(SeenNotes(c, "s2")) != 0 {
		t.Fatal("another session sees them again")
	}
}

func TestNotesSeenUnsafeSession(t *testing.T) {
	c := t.TempDir()
	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		if err := MarkNotesSeen(c, bad, []string{"N-1"}); !errors.Is(err, ErrUnsafeSessionID) {
			t.Fatalf("%q: err = %v", bad, err)
		}
		if len(SeenNotes(c, bad)) != 0 {
			t.Fatalf("%q: must see nothing", bad)
		}
	}
}

// fakeGit puts a git on PATH that prints list (NUL-separated paths) after
// sleeping for sleep seconds ("0" = no sleep), and logs each call.
func fakeGit(t *testing.T, list, sleep string) (log string) {
	t.Helper()
	old := uniqueBudget
	uniqueBudget = 3 * time.Second // exec of a fresh script can be slow under scanners
	t.Cleanup(func() { uniqueBudget = old })
	bin := t.TempDir()
	log = filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\n[ " + sleep + " != 0 ] && exec sleep " + sleep + "\nprintf '" + list + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestUniqueNamesCountsOneName(t *testing.T) {
	log := fakeGit(t, `infra/release.sh\0`, "0")
	if !UniqueNames(context.Background(), t.TempDir())("release.sh") {
		t.Fatal("one tracked file: want unique")
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), ":(glob)**/release.sh") {
		t.Fatalf("pathspec not used: %s", b)
	}
	fakeGit(t, `a/package.json\0b/package.json\0`, "0")
	if UniqueNames(context.Background(), t.TempDir())("package.json") {
		t.Fatal("two tracked files: want not unique")
	}
	fakeGit(t, ``, "0")
	if UniqueNames(context.Background(), t.TempDir())("gone.txt") {
		t.Fatal("none tracked: want not unique")
	}
}

func TestUniqueNamesEscapesGlob(t *testing.T) {
	log := fakeGit(t, `x/[a]*.txt\0`, "0")
	UniqueNames(context.Background(), t.TempDir())("[a]*.txt")
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), `:(glob)**/\[a]\*.txt`) {
		t.Fatalf("name not glob-escaped: %s", b)
	}
}

// A slow git burns only its own sub-budget and answers "not unique".
func TestUniqueNamesSlowGitIsBounded(t *testing.T) {
	fakeGit(t, `x/release.sh\0`, "10")
	uniqueBudget = 300 * time.Millisecond
	start := time.Now()
	if UniqueNames(context.Background(), t.TempDir())("release.sh") {
		t.Fatal("timed out: want false")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestNotesSeenPrunesOldSessions(t *testing.T) {
	c := t.TempDir()
	if err := MarkNotesSeen(c, "old", []string{"N-1"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(c, notesSeenSubdir, "old"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := MarkNotesSeen(c, "new", []string{"N-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c, notesSeenSubdir, "old")); err == nil {
		t.Fatal("old session file not pruned")
	}
	if !SeenNotes(c, "new")["N-2"] {
		t.Fatal("new session lost")
	}
}

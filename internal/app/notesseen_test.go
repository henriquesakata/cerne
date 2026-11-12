package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// fakeGit puts a git on PATH that logs each call and prints a tracked list.
func fakeGit(t *testing.T) (log string) {
	t.Helper()
	bin := t.TempDir()
	log = filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho x >> " + log + "\nprintf 'a/package.json\\0b/package.json\\0infra/release.sh\\0'\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func calls(t *testing.T, log string) int {
	b, _ := os.ReadFile(log)
	return strings.Count(string(b), "x")
}

func TestUniqueNamesRunsGitOncePerSession(t *testing.T) {
	log := fakeGit(t)
	c := t.TempDir()
	u := UniqueNames(context.Background(), c, "s1", t.TempDir())
	if u("package.json") || !u("release.sh") || u("missing.txt") {
		t.Fatal("uniqueness wrong")
	}
	u2 := UniqueNames(context.Background(), c, "s1", t.TempDir())
	if !u2("release.sh") || u2("package.json") {
		t.Fatal("cached uniqueness wrong")
	}
	if n := calls(t, log); n != 1 {
		t.Fatalf("git ran %d times, want 1", n)
	}
}

func TestUniqueNamesNoSessionNoCache(t *testing.T) {
	log := fakeGit(t)
	c := t.TempDir()
	if !UniqueNames(context.Background(), c, "", t.TempDir())("release.sh") {
		t.Fatal("want unique")
	}
	if _, err := os.Stat(filepath.Join(c, "notes-seen")); err == nil {
		t.Fatal("no session must not write a cache")
	}
	if calls(t, log) != 1 {
		t.Fatal("git should run once")
	}
}

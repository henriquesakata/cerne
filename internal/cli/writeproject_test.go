package cli

import (
	"bytes"
	"github.com/henriquesakata/cerne/internal/gitback"
	"path/filepath"
	"strings"
	"testing"
)

// F-0347 crit. 1, 3, 4, 5: a command that writes to the backend ends with one
// stderr line naming the project, the backend and the .cernerc it came from.
func TestWriteNamesProject(t *testing.T) {
	projDir, origin, _ := setupProject(t)
	stdout, stderr, err := execCerneSplit(t, "note", "new", "Gotcha de escrita", "--dir", projDir)
	if err != nil {
		t.Fatalf("note new: %v\n%s%s", err, stdout, stderr)
	}
	want := `cerne: wrote to project "demo" (` + origin + `) — .cernerc at ` + filepath.Join(projDir, ".cernerc")
	if n := strings.Count(stderr, want); n != 1 {
		t.Fatalf("stderr has the line %d times, want 1\nwant: %s\ngot:\n%s", n, want, stderr)
	}
	if strings.Contains(stdout, "wrote to project") { // crit. 3
		t.Fatalf("line leaked to stdout:\n%s", stdout)
	}
}

// push ends with the F-0876 result line; the write line comes right before it,
// still exactly once.
func TestPushNamesProjectBeforeFinalLine(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	mustWrite(t, specPath, realDoc("F-0001", "Edicao que sobe."))
	_, stderr, err := execCerneSplit(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push: %v\n%s", err, stderr)
	}
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	if n := strings.Count(stderr, "wrote to project"); n != 1 || !strings.HasPrefix(lines[len(lines)-2], "cerne: wrote to project") {
		t.Fatalf("want one write line just before the result line, got:\n%s", stderr)
	}
}

// F-0347 crit. 2: a read-only command does not print the line, even after an
// earlier command in the same process wrote.
func TestReadDoesNotNameProject(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, _, err := execCerneSplit(t, "note", "new", "Gotcha de escrita", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	stdout, stderr, err := execCerneSplit(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v\n%s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "wrote to project") {
		t.Fatalf("read command printed the write line:\n%s%s", stdout, stderr)
	}
}

// A backend URL carrying credentials never reaches stderr; the scp form
// has no userinfo to strip and stays as written.
func TestBackendForDisplayDropsCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:tok3n@git.example.com/rockt/docs.git": "https://git.example.com/rockt/docs.git",
		"https://tok3n@git.example.com/rockt/docs.git":      "https://git.example.com/rockt/docs.git",
		"ssh://git@git.example.com/rockt/docs.git":          "ssh://git.example.com/rockt/docs.git",
		"git@github.com:henriquesakata/docs.git":            "git@github.com:henriquesakata/docs.git",
		"https://root:p%zz@git.host/x.git":                  "https://git.host/x.git",
		"https://git.host/x.git?access_token=abc#frag":      "https://git.host/x.git",
		"https://u:p@ss@git.host/x.git":                     "https://git.host/x.git",
		"https://u:pa/ss@git.host/x.git":                    "https://git.host/x.git",
		"https://u:p?ss@git.host/x.git":                     "https://git.host/x.git",
		"https://u:p#ss@git.host/x.git":                     "https://git.host/x.git",
		"/tmp/origin.git":                                   "/tmp/origin.git",
	} {
		if got := backendForDisplay(in); got != want {
			t.Errorf("backendForDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}

// writeTwice pushes twice through gitback, as one command doing several steps.
func writeTwice(t *testing.T) {
	t.Helper()
	origin := newOrigin(t)
	clone := filepath.Join(t.TempDir(), "c")
	if err := gitback.Clone(origin, clone); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b"} {
		mustWrite(t, filepath.Join(clone, n+".txt"), n)
		if _, err := gitback.CommitAndPush(clone, n); err != nil {
			t.Fatal(err)
		}
	}
}

// F-0347 crit. 4 (two pushes, one line) and crit. 1 on the error path: main
// calls NoteWriteAfterError when Execute fails, since cobra skips PostRun then.
func TestNoteWriteOnceAfterTwoPushesAndError(t *testing.T) {
	startWriteNotice()
	lastProject.id, lastProject.backend, lastProject.cernerc = "demo", "b", "/p/.cernerc"
	writeTwice(t)
	var buf bytes.Buffer
	NoteWriteAfterError(&buf)
	noteWrite(&buf) // PostRun after a success path must not repeat it
	if n := strings.Count(buf.String(), "wrote to project"); n != 1 {
		t.Fatalf("line count = %d, want 1:\n%s", n, buf.String())
	}
}

// An error without a write prints nothing (the main.go path).
func TestNoteWriteAfterErrorWithoutWrite(t *testing.T) {
	startWriteNotice()
	lastProject.id, lastProject.backend, lastProject.cernerc = "demo", "b", "/p/.cernerc"
	var buf bytes.Buffer
	NoteWriteAfterError(&buf)
	if buf.Len() != 0 {
		t.Fatalf("printed without a write: %q", buf.String())
	}
}

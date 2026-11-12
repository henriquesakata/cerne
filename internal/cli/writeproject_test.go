package cli

import (
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
		"/tmp/origin.git":                                   "/tmp/origin.git",
	} {
		if got := backendForDisplay(in); got != want {
			t.Errorf("backendForDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}

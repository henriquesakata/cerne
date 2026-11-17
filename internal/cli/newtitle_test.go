package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var newTitleIDRe = regexp.MustCompile(`\b(?:F|B|N|D|RB|R)-\d{4}\b`)

// newTitleCmds are the `new` commands whose title may come from the body.
var newTitleCmds = []struct {
	name string
	args []string // before the optional title; flags after
}{
	{"note", []string{"note", "new"}},
	{"decision", []string{"decision", "new"}},
	{"runbook", []string{"runbook", "new"}},
	{"reference", []string{"reference", "new", "--force", "--url", "https://example.com/ref"}},
	{"feature", []string{"feature", "new"}},
	{"bug", []string{"bug", "new", "--on-behalf", "the human said so"}},
}

func shownTitle(t *testing.T, projDir, created string) string {
	t.Helper()
	id := newTitleIDRe.FindString(created)
	if id == "" {
		t.Fatalf("no id in output:\n%s", created)
	}
	out, err := execCerne(t, "show", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("show %s: %v\n%s", id, err, out)
	}
	return strings.SplitN(out, "\n", 2)[0]
}

// B-0018: with no title argument the title is the body's first H1 — from
// --file and from stdin; the argument wins; no argument and no H1 says what to
// do; an H1 in a code fence is not the title.
func TestNewTitleFromBodyH1(t *testing.T) {
	for _, c := range newTitleCmds {
		t.Run(c.name, func(t *testing.T) {
			projDir, _, _ := setupProject(t)
			withDir := func(extra ...string) []string {
				return append(append(append([]string{}, c.args...), extra...), "--dir", projDir)
			}
			body := func(h1 string) string {
				fm := "---\ntags: demo\n---\n\n"
				if c.name == "feature" || c.name == "bug" {
					fm = "" // a new feature's spec takes no frontmatter block
				}
				return fm + "# " + h1 + "\n\nbody text\n"
			}

			// --file, H1 after frontmatter
			f := filepath.Join(t.TempDir(), "b.md")
			if err := os.WriteFile(f, []byte(body("Title from file "+c.name)), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := execCerne(t, withDir("--file", f)...)
			if err != nil {
				t.Fatalf("--file: %v\n%s", err, out)
			}
			if got := shownTitle(t, projDir, out); !strings.Contains(got, "Title from file "+c.name) {
				t.Fatalf("--file title: %q", got)
			}

			// stdin
			out, err = execCerneStdin(t, body("Title from stdin "+c.name), withDir()...)
			if err != nil {
				t.Fatalf("stdin: %v\n%s", err, out)
			}
			if got := shownTitle(t, projDir, out); !strings.Contains(got, "Title from stdin "+c.name) {
				t.Fatalf("stdin title: %q", got)
			}

			// the argument beats the H1
			out, err = execCerneStdin(t, body("Ignored heading "+c.name), withDir("Explicit argument "+c.name)...)
			if err != nil {
				t.Fatalf("argument: %v\n%s", err, out)
			}
			if got := shownTitle(t, projDir, out); !strings.Contains(got, "Explicit argument "+c.name) || strings.Contains(got, "Ignored heading") {
				t.Fatalf("argument must win: %q", got)
			}

			// no argument, no H1 (also: H1 only inside a code fence, and an H2)
			for _, nobody := range []string{"", "just text\n", "## Sub only\n", "```\n# in a fence\n```\n"} {
				var extra []string
				if c.name == "feature" || c.name == "bug" {
					extra = []string{"--context", "ctx"} // context-only still needs a title
				}
				out, err = execCerneStdin(t, nobody, withDir(extra...)...)
				if !errors.Is(err, errNewTitleRequired) {
					t.Fatalf("body %q: want errNewTitleRequired, got err=%v out=%s", nobody, err, out)
				}
				if !strings.Contains(err.Error(), "give a title or start the body with a `# ` heading") {
					t.Fatalf("message: %v", err)
				}
			}
		})
	}
}

// B-0018: the duplicate guard (F-0324) sees the derived title as if passed.
func TestNewTitleFromBodyH1FeedsDuplicateGuard(t *testing.T) {
	projDir, _, _ := setupProject(t)
	body := "# Gotcha about the shared cache clone\n\nbody\n"
	if out, err := execCerneStdin(t, body, "note", "new", "--dir", projDir); err != nil {
		t.Fatalf("first: %v\n%s", err, out)
	}
	out, err := execCerneStdin(t, body, "note", "new", "--dir", projDir)
	if err == nil || !strings.Contains(out, "similar registration already exists") {
		t.Fatalf("second note with the same H1 must hit the duplicate guard: err=%v\n%s", err, out)
	}
}

// B-0018: with stdin a terminal (a character device) and no --file, a `new`
// command never waits for a body: readOptionalBody returns "" on the
// ModeCharDevice check (stdinbody.go), so the title error comes at once.
func TestNewTitleTerminalStdinDoesNotBlock(t *testing.T) {
	projDir, _, _ := setupProject(t)
	dev, err := os.Open(os.DevNull) // a character device, like a tty
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	old := stdinBody
	stdinBody = dev
	t.Cleanup(func() { stdinBody = old })
	done := make(chan error, 1)
	go func() {
		_, err := execCerne(t, "note", "new", "--dir", projDir)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errNewTitleRequired) {
			t.Fatalf("want errNewTitleRequired, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("note new blocked on a terminal stdin")
	}
}

// B-0018: the argument is trimmed for every command; a blank one is no title.
func TestNewTitleArgumentTrimmedAndBlankRefused(t *testing.T) {
	for _, c := range newTitleCmds {
		t.Run(c.name, func(t *testing.T) {
			projDir, _, _ := setupProject(t)
			extra := []string{"--dir", projDir}
			if c.name == "feature" || c.name == "bug" {
				extra = append(extra, "--context", "ctx")
			}
			out, err := execCerne(t, append(append(append([]string{}, c.args...), "   "), extra...)...)
			if !errors.Is(err, errNewTitleRequired) {
				t.Fatalf("blank argument: want errNewTitleRequired, got err=%v out=%s", err, out)
			}
			out, err = execCerne(t, append(append(append([]string{}, c.args...), "  Padded "+c.name+"  "), extra...)...)
			if err != nil {
				t.Fatalf("padded argument: %v\n%s", err, out)
			}
			if got := shownTitle(t, projDir, out); !strings.HasSuffix(got, "]  Padded "+c.name) {
				t.Fatalf("title not trimmed: %q", got)
			}
		})
	}
}

// B-0018: the H1 that became the title stays in the stored body, and the
// shown/materialized document carries it once.
func TestNewTitleHeadingRendersOnce(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerneStdin(t, "# Heading rendered once\n\nbody text\n", "note", "new", "--dir", projDir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	id := newTitleIDRe.FindString(out)
	shown, err := execCerne(t, "show", id, "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(shown, "Heading rendered once"); n < 1 {
		t.Fatalf("show lost the title:\n%s", shown)
	}
	if n := strings.Count(shown, "# Heading rendered once"); n != 1 {
		t.Fatalf("show renders the heading %d times:\n%s", n, shown)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	files, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "notes", id+"-*.md"))
	if len(files) != 1 {
		t.Fatalf("materialized note files = %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if n := strings.Count(string(raw), "# Heading rendered once"); n != 1 {
		t.Fatalf("materialized doc renders the heading %d times:\n%s", n, raw)
	}
}

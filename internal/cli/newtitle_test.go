package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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

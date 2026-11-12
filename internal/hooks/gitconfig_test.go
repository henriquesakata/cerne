package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitProject lays a .git dir with the given config (none when "") under a
// fresh project dir.
func gitProject(t *testing.T, config string) string {
	t.Helper()
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		if err := os.WriteFile(filepath.Join(proj, ".git", "config"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return proj
}

// TestGitHooksPath (F-0360 crit. 9/11): core.hooksPath is read from the [core]
// section only, key case-insensitive, comments and other sections ignored.
func TestGitHooksPath(t *testing.T) {
	cases := []struct {
		name, config, want string
		set                bool
	}{
		{"no config", "", "", false},
		{"relative", "[core]\n\tbare = false\n\thooksPath = .husky/_\n", ".husky/_", true},
		{"absolute", "[core]\n\thooksPath = /abs/repo/.husky/_\n", "/abs/repo/.husky/_", true},
		{"key case", "[Core]\n\tHooksPath = .husky/_\n", ".husky/_", true},
		{"quoted", "[core]\n\thooksPath = \"my hooks\"\n", "my hooks", true},
		{"inline comment", "[core]\n\thooksPath = .husky/_ # husky 9\n", ".husky/_", true},
		{"last wins", "[core]\n\thooksPath = a\n[core]\n\thooksPath = b\n", "b", true},
		{"comment only", "[core]\n\tbare = false\n# hooksPath = .husky/_\n; hooksPath = x\n", "", false},
		{"other section", "[core]\n\tbare = false\n[alias]\n\thooksPath = x\n[remote \"origin\"]\n\thooksPath = y\n", "", false},
		{"empty value", "[core]\n\thooksPath =\n", "", false},
		{"comment after header", "[core] # husky\n\thooksPath = .husky/_\n", ".husky/_", true},
		{"crlf", "[core]\r\n\thooksPath = .husky/_\r\n", ".husky/_", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, set := GitHooksPath(gitProject(t, tc.config))
			if got != tc.want || set != tc.set {
				t.Fatalf("GitHooksPath = (%q, %v), want (%q, %v)", got, set, tc.want, tc.set)
			}
		})
	}
}

// TestInstallReadsHooksPathFromCore (F-0360 crit. 11, install side): a
// hooksPath mentioned only in a comment no longer makes the install skip the
// git slots; one in [core] still does.
func TestInstallReadsHooksPathFromCore(t *testing.T) {
	proj := gitProject(t, "[core]\n\tbare = false\n# hooksPath was .husky/_ before\n")
	if r := installGuardCommitMsgGitHook(proj); r.Status != StatusCreated {
		t.Errorf("guard with hooksPath in a comment = %s (%s), want created", r.Status, r.Note)
	}
	if r := installCommitTrailerGitHook(proj); r.Status != StatusCreated {
		t.Errorf("trailer with hooksPath in a comment = %s (%s), want created", r.Status, r.Note)
	}

	proj = gitProject(t, "[core]\n\thooksPath = .husky/_\n")
	for _, r := range []Result{installGuardCommitMsgGitHook(proj), installCommitTrailerGitHook(proj)} {
		if r.Status != StatusSkipped || !strings.Contains(r.Note, "custom core.hooksPath") {
			t.Errorf("%s with [core] hooksPath = %s (%s), want skipped", r.Asset, r.Status, r.Note)
		}
	}
}

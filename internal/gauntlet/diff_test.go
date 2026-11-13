package gauntlet

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture: a clone whose origin has branch trunk; a feature branch adds code,
// ui/dist, docs.lock and gen/x. withHead sets origin/HEAD to trunk.
func fixture(t *testing.T, trunk string, withHead bool) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	git(t, root, "init", "-q", "--bare", "-b", trunk, origin)
	dir := filepath.Join(root, "w")
	git(t, root, "clone", "-q", origin, dir)
	git(t, dir, "checkout", "-q", "-b", trunk)
	write(t, dir, "base.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "push", "-q", "origin", trunk)
	if withHead {
		git(t, dir, "remote", "set-head", "origin", trunk)
	}
	git(t, dir, "checkout", "-q", "-b", "feature/x")
	write(t, dir, "code.go", "package x\n")
	write(t, dir, "ui/dist/a.js", "noise\n")
	write(t, dir, "docs.lock", "pin\n")
	write(t, dir, "gen/x.txt", "gen\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "feat")
	return dir
}

func TestDiff(t *testing.T) {
	cases := []struct {
		name     string
		trunk    string
		head     bool
		pin      string
		base     string
		excludes []string
		wantBase string
		has      []string
		hasNot   []string
	}{
		{name: "pin branch wins", trunk: "release", head: false, pin: "release", wantBase: "origin/release", has: []string{"code.go", "gen/x.txt"}, hasNot: []string{"ui/dist", "docs.lock"}},
		{name: "origin/HEAD", trunk: "trunk", head: true, wantBase: "origin/HEAD", has: []string{"code.go"}, hasNot: []string{"ui/dist", "docs.lock"}},
		{name: "local main", trunk: "main", head: false, wantBase: "main", has: []string{"code.go"}},
		{name: "explicit base", trunk: "trunk", head: false, base: "origin/trunk", wantBase: "origin/trunk", has: []string{"code.go"}},
		{name: "extra exclude", trunk: "main", head: false, excludes: []string{"gen/*"}, wantBase: "main", has: []string{"code.go"}, hasNot: []string{"gen/x.txt", "ui/dist"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, tc.trunk, tc.head)
			got, base, err := Diff(dir, tc.base, tc.pin, tc.excludes)
			if err != nil {
				t.Fatal(err)
			}
			if base != tc.wantBase {
				t.Errorf("base = %q, want %q", base, tc.wantBase)
			}
			for _, s := range tc.has {
				if !strings.Contains(got, s) {
					t.Errorf("missing %q:\n%s", s, got)
				}
			}
			for _, s := range tc.hasNot {
				if strings.Contains(got, s) {
					t.Errorf("unexpected %q:\n%s", s, got)
				}
			}
		})
	}
}

func TestDiffNoBase(t *testing.T) {
	dir := fixture(t, "trunk", false) // no origin/HEAD, no pin, no local main
	if _, _, err := Diff(dir, "", "", nil); !errors.Is(err, ErrNoBase) {
		t.Fatalf("err = %v, want ErrNoBase", err)
	}
}

package gauntlet

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func TestDiffPinBeatsOriginHead(t *testing.T) {
	dir := fixture(t, "release", false)
	git(t, dir, "push", "-q", "origin", "feature/x:other")
	git(t, dir, "remote", "set-head", "origin", "other")
	_, base, err := Diff(dir, "", "release", nil)
	if err != nil || base != "origin/release" {
		t.Fatalf("base = %q err = %v, want origin/release", base, err)
	}
}

func TestDiffNotGitRepo(t *testing.T) {
	dir := t.TempDir()
	for _, base := range []string{"", "main"} {
		if _, _, err := Diff(dir, base, "", nil); !errors.Is(err, ErrNotGitRepo) {
			t.Errorf("base %q: err = %v, want ErrNotGitRepo", base, err)
		}
	}
}

func TestDiffBadBase(t *testing.T) {
	dir := fixture(t, "main", false)
	if _, _, err := Diff(dir, "nope", "", nil); !errors.Is(err, ErrBadBase) {
		t.Fatalf("err = %v, want ErrBadBase", err)
	}
}

func commitFile(t *testing.T, dir, rel, msg string) {
	t.Helper()
	write(t, dir, rel, msg+"\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", msg)
}

func TestFeatureCommits(t *testing.T) {
	dir := fixture(t, "main", false)
	git(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, "one.go", "feat: first (F-0001)")
	commitFile(t, dir, "other.go", "feat: other F-00010 thing")
	commitFile(t, dir, "lower.go", "feat: lower f-0001 thing")
	commitFile(t, dir, "pre.go", "feat: prefix XF-0001 thing")
	write(t, dir, "two.go", "two\n")
	write(t, dir, "ui/dist/b.js", "noise\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "fix: second", "-m", "body cites F-0001.")
	git(t, dir, "checkout", "-q", "-b", "side")
	commitFile(t, dir, "side.go", "feat: side F-0001")
	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "merge", "-q", "--no-ff", "-m", "Merge F-0001 side", "side")

	diff, commits, base, err := FeatureCommits(dir, "", "", "F-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if base != "main" || len(commits) != 3 {
		t.Fatalf("base=%q commits=%v", base, commits)
	}
	// the side-branch commit may interleave with "second" (equal timestamps).
	var subjects []string
	for _, c := range commits {
		subjects = append(subjects, c.Subject)
	}
	if subjects[0] != "feat: first (F-0001)" || !slices.Contains(subjects, "fix: second") || !slices.Contains(subjects, "feat: side F-0001") {
		t.Errorf("wrong commits/order: %v", commits)
	}
	pos := 0
	for _, s := range []string{"commit " + commits[0].Sha + " feat: first", "one.go"} {
		i := strings.Index(diff[pos:], s)
		if i < 0 {
			t.Fatalf("%q out of order/missing:\n%s", s, diff)
		}
		pos += i + len(s)
	}
	if !strings.Contains(diff, "two.go") || !strings.Contains(diff, "side.go") {
		t.Errorf("missing second/side diffs:\n%s", diff)
	}
	for _, s := range []string{"other.go", "lower.go", "pre.go", "ui/dist", "Merge F-0001"} {
		if strings.Contains(diff, s) {
			t.Errorf("unexpected %q:\n%s", s, diff)
		}
	}
}

func TestFeatureCommitsNone(t *testing.T) {
	dir := fixture(t, "main", false)
	_, _, _, err := FeatureCommits(dir, "", "", "F-0001", nil)
	if !errors.Is(err, ErrNoFeatureCommits) || err.Error() != "no commit on main cites F-0001 in its message" {
		t.Fatalf("err = %v", err)
	}
}

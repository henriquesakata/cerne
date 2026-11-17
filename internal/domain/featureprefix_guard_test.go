package domain

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// handWritten are the shapes of a hand-listed entity-id prefix family:
// a character class before the dash ([FT]-, [fte]-), an alternation ((F|T)-),
// a chain of HasPrefix on prefix literals, and a literal list of 1-2 letter
// prefixes ("F", "D", "R" in a case or a composite). The feature family is
// listed once, in gates.go (B-0021); everything else asks it.
var handWritten = []*regexp.Regexp{
	regexp.MustCompile(`\[[A-Za-z]{2,}\]-`),
	regexp.MustCompile(`\((?:\?:)?[A-Z]{1,2}(?:\|[A-Z]{1,2})+\)-`),
	regexp.MustCompile(`HasPrefix\([^()]*,\s*"[A-Z]{1,2}-"\)[^\n]*HasPrefix\([^()]*,\s*"[A-Z]{1,2}-"\)`),
	regexp.MustCompile(`"[A-Z]{1,2}",\s*"[A-Z]{1,2}"`),
	regexp.MustCompile(`HasPrefix\([^()]*,\s*"[FETB]-"\)`), // a lone check of one feature prefix
}

// handWrittenAllowed are the lines that may keep such a pattern, each by file
// and a substring of the line, with the reason it is not the feature family
// being re-listed. Per line, so a new hand-written list in the same file fails.
var handWrittenAllowed = []struct{ file, contains, reason string }{
	{"internal/domain/gates.go", "", "the single source of truth for the feature-family prefixes"},
	{"internal/index/index.go", `[]string{"F", "D", "R", "RB", "N", "H"}`, "the entity-kind order of the incremental rebuild, not a feature prefix list; E-/T-/B- ride the F case via IsFeatureID"},
	{"internal/app/work.go", `strings.HasPrefix(id, "W-"), strings.HasPrefix(id, "N-")`, "W-/N-/RB- are three non-feature kinds checked by existence (B-0019)"},
	{"internal/app/approvereadiness.go", `strings.HasPrefix(id, "B-")`, "tests one kind of the family (a bug or a task), not the family"},
	{"internal/app/bargate.go", `strings.HasPrefix(id, "B-")`, "tests one kind of the family (a bug or a task), not the family"},
	{"internal/app/taskbar.go", `strings.HasPrefix(id, "T-")`, "tests one kind of the family (a bug or a task), not the family"},
	{"internal/cli/epictaskcmd.go", `strings.HasPrefix(id, "T-")`, "tests one kind of the family (a bug or a task), not the family"},
	{"internal/cli/transitioncmd.go", `strings.HasPrefix(id, "T-")`, "tests one kind of the family (a bug or a task), not the family"},
	{"internal/lint/lint.go", `strings.HasPrefix(f.ID, "B-")`, "tests one kind of the family (a bug or a task), not the family"},
	{"internal/lint/resultgauntlet.go", `strings.HasPrefix(f.ID, "T-")`, "tests one kind of the family (a bug or a task), not the family"},
	{"internal/lint/resultgauntlet.go", `strings.HasPrefix(c.ID, "T-")`, "tests one kind of the family (a bug or a task), not the family"},
}

// TestNoHandWrittenFeaturePrefixLists (B-0021): non-test Go source must not
// list feature-id prefixes by hand; use IsFeatureID / FeatureIDPrefix.
func TestNoHandWrittenFeaturePrefixLists(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[int]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(src), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				for _, re := range handWritten {
					if !re.MatchString(line) {
						continue
					}
					if i := allowedLine(rel, line); i >= 0 {
						used[i] = true
						continue
					}
					t.Errorf("%s:%d hand-written id-prefix list (%s): %s", rel, i+1, re, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, a := range handWrittenAllowed {
		if !used[i] {
			t.Errorf("allowlist entry %s %q no longer needed; remove it", a.file, a.contains)
		}
	}
}

func allowedLine(rel, line string) int {
	for i, a := range handWrittenAllowed {
		if a.file == rel && strings.Contains(line, a.contains) {
			return i
		}
	}
	return -1
}

// TestFeatureIDPrefixMatchesTheList (B-0021): the regexp group and IsFeatureID
// agree, and neither takes a runbook (RB-), a work or a decision for a feature.
func TestFeatureIDPrefixMatchesTheList(t *testing.T) {
	re := regexp.MustCompile(`^` + FeatureIDPrefix() + `-\d{4}$`)
	for _, id := range []string{"F-0001", "E-0001", "T-0001", "B-0001", "RB-0001", "W-0001", "D-0001", "N-0001", "P-0001", "X-0001"} {
		if got, want := re.MatchString(id), IsFeatureID(id); got != want {
			t.Errorf("%s: pattern %v, IsFeatureID %v", id, got, want)
		}
	}
	for id, want := range map[string]bool{"E-0001": true, "F-0001": false, "B-0001": false, "RB-0001": false} {
		if IsEpicID(id) != want {
			t.Errorf("IsEpicID(%s) = %v", id, !want)
		}
	}
}

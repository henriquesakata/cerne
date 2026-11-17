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
}

// handWrittenAllowed are the files that may keep such a pattern, each with the
// reason it is not the feature family being re-listed.
var handWrittenAllowed = map[string]string{
	"internal/domain/gates.go": "the single source of truth for the feature-family prefixes",
	"internal/index/index.go":  "the entity-kind order of the incremental rebuild (F, D, R, RB, N, H), not a feature prefix list; E-/T-/B- ride the F case via IsFeatureID",
	"internal/app/work.go":     "W-/N-/RB- are three non-feature kinds checked by existence (B-0019), not the feature family",
}

// TestNoHandWrittenFeaturePrefixLists (B-0021): non-test Go source must not
// list feature-id prefixes by hand; use IsFeatureID / FeatureIDPrefix.
func TestNoHandWrittenFeaturePrefixLists(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
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
					if _, ok := handWrittenAllowed[rel]; ok {
						used[rel] = true
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
	for f := range handWrittenAllowed {
		if !used[f] {
			t.Errorf("allowlist entry %s no longer needed; remove it", f)
		}
	}
}

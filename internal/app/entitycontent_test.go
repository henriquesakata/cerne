package app

import (
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
)

// TestContentHash pins the hash contract of F-0464: sha256 of the exact
// bytes, lowercase hex, 64 characters; empty bytes (a missing file) hash too.
func TestContentHash(t *testing.T) {
	const empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := ContentHash(nil); got != empty {
		t.Fatalf("ContentHash(nil) = %q, want %q", got, empty)
	}
	if got := ContentHash([]byte("x")); !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) || got == empty {
		t.Fatalf("ContentHash(x) = %q, want another 64-char lowercase hex", got)
	}
}

// TestSaveFeatureDocConflict (F-0464 crit. 6): a write carrying the hash of
// what the editor loaded is refused with ErrContentConflict when the backend's
// document hashes otherwise — compared against origin, not a trailing clone —
// and nothing reaches origin; no base writes as before; a frozen feature
// refuses on state first.
func TestSaveFeatureDocConflict(t *testing.T) {
	const spec = "content/features/F-0001/spec.md"
	const theirs = "# Spec\n\nVersão de outro agente.\n"
	cases := []struct {
		name         string
		advance      bool                          // another agent pushes theirs to origin first
		base         func(seed string) string      // the hash the editor sends
		content      string                        // the editor's draft
		wantConflict bool
		wantOrigin   func(seed, mine string) string // spec.md on origin afterwards
	}{
		{"stale base", true, func(seed string) string { return ContentHash([]byte(seed)) }, "# Spec\n\nMinha versão.\n", true,
			func(_, _ string) string { return theirs }},
		{"base of origin, clone trailing", true, func(string) string { return ContentHash([]byte(theirs)) }, "# Spec\n\nMinha versão.\n", false,
			func(_, mine string) string { return mine }},
		{"stale base, draft equal to origin", true, func(seed string) string { return ContentHash([]byte(seed)) }, theirs, true,
			func(_, _ string) string { return theirs }},
		{"no base", false, func(string) string { return "" }, "# Spec\n\nMinha versão.\n", false,
			func(_, mine string) string { return mine }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repoDir, projDir, origin := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
			seed, err := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
			if err != nil {
				t.Fatal(err)
			}
			if c.advance {
				advanceRemote(t, origin, spec, theirs)
			}
			head0, _ := gitback.HeadSHA(origin)

			err = SaveFeatureDocBy(repoDir, projDir, "F-0001", "spec", c.content, c.base(string(seed)), HumanAuthor())

			if c.wantConflict {
				if !errors.Is(err, ErrContentConflict) {
					t.Fatalf("err = %v, want ErrContentConflict", err)
				}
				var ce *ContentConflictError
				if !errors.As(err, &ce) || ce.Current != theirs {
					t.Fatalf("errors.As = %+v, want Current = origin's body %q", ce, theirs)
				}
				if head1, _ := gitback.HeadSHA(origin); head1 != head0 {
					t.Fatalf("origin moved on a refused write: %s -> %s", head0, head1)
				}
			} else if err != nil {
				t.Fatalf("SaveFeatureDocBy: %v", err)
			}
			if got, _ := originHas(t, origin, spec); got != c.wantOrigin(string(seed), c.content) {
				t.Fatalf("origin spec = %q, want %q", got, c.wantOrigin(string(seed), c.content))
			}
		})
	}

	t.Run("frozen feature refuses on state", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateDone, "spec.md")
		seed, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
		for _, base := range []string{"", "stale", ContentHash(seed)} {
			if err := SaveFeatureDocBy(repoDir, projDir, "F-0001", "spec", "x", base, HumanAuthor()); !errors.Is(err, ErrFeatureFrozen) {
				t.Fatalf("base %q: err = %v, want ErrFeatureFrozen", base, err)
			}
		}
	})
}

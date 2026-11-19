package cli

import (
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0449 (audit finding 5): a forged stored link never adds a line to the
// commit message; the hook stays fail-open.
func TestHookCommitTrailerForgedLinkAddsNothing(t *testing.T) {
	for _, m := range []store.MirrorRef{
		{Tracker: "pub", Kind: "fake", ID: "12\nCloses: #1", URL: "https://fake.test/pub/issues/12\nCloses: #1"},
		{Tracker: "pub", Kind: "fake", ID: "1", URL: "https://fake.test/pub/issues/1\nCo-authored-by: Mallory <m@evil.example>"},
	} {
		projDir, cacheDir := refsProject(t, "", false)
		repoDir := filepath.Join(cacheDir, "repo")
		f, err := store.LoadFeature(repoDir, "F-0001")
		if err != nil {
			t.Fatal(err)
		}
		f.Mirror = &m
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatal(err)
		}
		const msg = "fix: y\n\nCerne: F-0001\n"
		if got := runTrailerHook(t, writeMsgFile(t, projDir, msg)); got != msg {
			t.Fatalf("message changed by a forged link:\n%q", got)
		}
	}
}

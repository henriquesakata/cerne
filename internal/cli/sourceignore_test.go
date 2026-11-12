package cli

import (
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
)

// F-0355 crit. 9: a URL under the repository of [delivery] commit_url (commit
// or PR) is not an unregistered source; the same path on another host is.
func TestOwnRepoURLsAreNotUnregisteredSources(t *testing.T) {
	cfg := project.Config{Delivery: project.DeliveryConfig{CommitURL: "https://github.com/henriquesakata/cerne/commit/{sha}"}}
	text := "Entrega: [c](https://github.com/henriquesakata/cerne/commit/abc1234) e " +
		"[PR](https://github.com/henriquesakata/cerne/pull/12) e " +
		"[outro](https://github.example/henriquesakata/cerne/pull/12) e [x](https://github.com/other/repo/pull/1)"
	got := lint.OrphanSourceURLs("F-0001", text, nil, cfg.SourceIgnore())
	want := []string{"https://github.example/henriquesakata/cerne/pull/12", "https://github.com/other/repo/pull/1"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("orphans = %v, want %v", got, want)
	}
	// Without a [delivery] block the own repo is flagged, as before.
	if got := lint.OrphanSourceURLs("F-0001", text, nil, project.Config{}.SourceIgnore()); len(got) != 4 {
		t.Fatalf("no [delivery]: orphans = %v, want all 4", got)
	}
}

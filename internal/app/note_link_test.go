package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// E-0005 criterion 14 (owner's adjustment): cerne note link adds and removes
// what an active note informs, the mirror of reference link (F-0389) — no
// duplicate, the events logged, an unknown target or an id in both lists
// refused, a no-op committing nothing, a superseded note refused.
func TestLinkNote(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, _ := CreateFeature(repoDir, projDir, "Uma feature", "", "", nil, nil)
	n, err := CreateNote(repoDir, projDir, "Uma regra", "", nil, nil, nil, nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := LinkNote(repoDir, projDir, n.ID, []string{f.ID, f.ID, " "}, nil)
	if err != nil || !slices.Equal(got.Informs, []string{f.ID}) {
		t.Fatalf("link: %v %v", got.Informs, err)
	}
	if p := createdPayload(t, repoDir, n.ID, "note.linked"); p["informs"] != f.ID {
		t.Fatalf("note.linked payload = %v", p)
	}
	before, _ := gitback.HeadSHA(repoDir)
	if _, absent, err := LinkNote(repoDir, projDir, n.ID, []string{f.ID}, []string{"F-0042"}); err != nil || !slices.Equal(absent, []string{"F-0042"}) {
		t.Fatalf("no-op: absent %v, %v", absent, err)
	}
	if after, _ := gitback.HeadSHA(repoDir); after != before {
		t.Fatal("a no-op link committed")
	}
	if _, _, err := LinkNote(repoDir, projDir, n.ID, []string{"F-0999"}, nil); err == nil || !strings.Contains(err.Error(), "F-0999 not found") {
		t.Fatalf("unknown target: %v", err)
	}
	if _, _, err := LinkNote(repoDir, projDir, n.ID, []string{f.ID}, []string{f.ID}); err == nil || !strings.Contains(err.Error(), "both name") {
		t.Fatalf("both lists: %v", err)
	}
	got, _, err = LinkNote(repoDir, projDir, n.ID, nil, []string{f.ID})
	if err != nil || len(got.Informs) != 0 {
		t.Fatalf("unlink: %v %v", got.Informs, err)
	}
	if p := createdPayload(t, repoDir, n.ID, "note.unlinked"); p["informs"] != f.ID {
		t.Fatalf("note.unlinked payload = %v", p)
	}
	if _, err := TransitionNote(repoDir, projDir, n.ID, "", "obsoleta"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LinkNote(repoDir, projDir, n.ID, []string{f.ID}, nil); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("superseded note: %v", err)
	}
}

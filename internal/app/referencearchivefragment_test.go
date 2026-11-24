package app

import (
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0503 crit. 3: the fragment of a reference URL never reaches the archive
// service, in the capture list or in Save; the reference keeps its own URL.
func TestArchiveReferencesNeverSendTheFragment(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 1, true)
	const withFragment = "https://example.com/1#private-note"
	setRef(t, repoDir, projDir, "R-0001", func(r *store.Reference) { r.URL = withFragment })
	c := &scriptClient{save: saves, captures: noCaptures}
	res, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"captures https://example.com/1", "save https://example.com/1"}; !slices.Equal(c.asked, want) {
		t.Errorf("asked %v, want %v", c.asked, want)
	}
	if len(res) != 1 || res[0].Status != "archived" || loadRef(t, repoDir, "R-0001").URL != withFragment {
		t.Errorf("res = %+v; want archived, the reference keeping its URL", res)
	}
}

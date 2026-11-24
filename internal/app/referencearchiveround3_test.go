package app

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0503 crit. 10: a failed capture list for a reference registered before
// today saves nothing: it is pending, so a later batch retries it; a reference
// registered today still asks for a new copy.
func TestArchiveReferencesCaptureListFailure(t *testing.T) {
	u := "https://example.com/1"
	failing := func(string) ([]Snapshot, error) { return nil, httpStatusError(http.StatusServiceUnavailable) }
	for _, tc := range []struct {
		name    string
		created time.Time
		asked   []string
		status  string
		reason  string
	}{
		{"older", savedAt.Add(-48 * time.Hour), []string{"captures " + u}, "pending", "capture list failed: 503"},
		{"today", savedAt.Add(-2 * time.Hour), []string{"captures " + u, "save " + u}, "archived", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir := archiveBackend(t, 1, true)
			setRef(t, repoDir, projDir, "R-0001", func(r *store.Reference) { r.CreatedAt = tc.created })
			c := &scriptClient{save: saves, captures: failing}
			if _, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false); err != nil {
				t.Fatal(err)
			}
			got := loadRef(t, repoDir, "R-0001").Archive
			if got == nil || got.Status != tc.status || got.Reason != tc.reason {
				t.Fatalf("stored %+v, want %s (%s)", got, tc.status, tc.reason)
			}
			if !slices.Equal(c.asked, tc.asked) {
				t.Errorf("asked %v, want %v", c.asked, tc.asked)
			}
		})
	}
}

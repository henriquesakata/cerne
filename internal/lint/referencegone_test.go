package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0501 crit. 7: reference-gone reads the recorded check only, lists the
// informed entities, and never fires for changed, obsolete or unchecked ones.
func TestReferenceGone(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	at := now.Add(-48 * time.Hour)
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Uses sources", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	mk := func(id string, state domain.ReferenceState, c *store.ReferenceCheck) {
		r := store.Reference{ID: id, Title: "Src " + id, State: state, URL: "https://example.com/" + id, External: true,
			Informs: []string{"F-0001"}, Check: c, CreatedAt: now, UpdatedAt: now}
		if err := store.SaveReference(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	mk("R-0001", domain.ReferenceCurrent, &store.ReferenceCheck{CheckedAt: at, HTTPStatus: 404, Status: "gone"})
	mk("R-0002", domain.ReferenceCurrent, &store.ReferenceCheck{CheckedAt: at, HTTPStatus: 200, TextHash: "b", BaselineHash: "a", Status: "changed"})
	mk("R-0003", domain.ReferenceObsolete, &store.ReferenceCheck{CheckedAt: at, HTTPStatus: 404, Status: "gone"})
	mk("R-0004", domain.ReferenceCurrent, nil)
	mk("R-0005", domain.ReferenceCurrent, &store.ReferenceCheck{CheckedAt: at, Status: "gone"}) // domain not found

	findings, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Finding{}
	for _, f := range findings {
		if f.Rule == RuleReferenceGone {
			got[f.Entity] = f
		}
	}
	if len(got) != 2 {
		t.Fatalf("reference-gone on %v, want R-0001 and R-0005:\n%+v", got, findings)
	}
	if d := got["R-0001"].Detail; !strings.Contains(d, "HTTP 404") || !strings.Contains(d, "F-0001") || !strings.Contains(d, at.Format("2006-01-02")) {
		t.Errorf("R-0001 detail = %q", d)
	}
	if d := got["R-0005"].Detail; !strings.Contains(d, "domain not found") {
		t.Errorf("R-0005 detail = %q", d)
	}
	if !Blocking(got["R-0001"]) {
		t.Error("reference-gone must be blocking")
	}
}

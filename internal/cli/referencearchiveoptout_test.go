package cli

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// F-0503 crit. 7: `reference archive --opt-out R-…` keeps an existing
// reference out of the archive for good: it needs ids, works with the setting
// off, reaches no network, and the mark holds for a later batch.
func TestReferenceArchiveOptOut(t *testing.T) {
	f := archiveFixture(t, false, http.StatusFound)
	f.cerne(t, "reference", "new", "Guide", "--url", "https://docs.publicsite.org/guide")
	if out, err := execCerne(t, "reference", "archive", "--opt-out", "--dir", f.projDir); !errors.Is(err, app.ErrOptOutNeedsIDs) {
		t.Errorf("no ids: err = %v, want ErrOptOutNeedsIDs\n%s", err, out)
	}
	if out, err := execCerne(t, "reference", "archive", "--opt-out", "--dry-run", "R-0001", "--dir", f.projDir); err == nil {
		t.Errorf("--opt-out with --dry-run should be refused\n%s", out)
	}
	out := f.cerne(t, "reference", "archive", "--opt-out", "R-0001")
	if !strings.Contains(out, "R-0001  no-archive\n") {
		t.Errorf("output:\n%s", out)
	}
	if !f.ref(t, "R-0001").NoArchive {
		t.Error("the reference was not marked no-archive")
	}
	f.turnOn(t)
	if out := f.cerne(t, "reference", "archive"); !strings.Contains(out, "nothing to archive") {
		t.Errorf("batch after the opt-out:\n%s", out)
	}
	if f.waybackHits.Load()+f.pageHits.Load() != 0 {
		t.Errorf("%d archive and %d page requests, want none", f.waybackHits.Load(), f.pageHits.Load())
	}
}

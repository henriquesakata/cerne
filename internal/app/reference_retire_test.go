package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0324: RetireReference moves a current reference to obsolete, records the
// reason on the event, requires a reason and refuses an already-obsolete one.
func TestRetireReference(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	r, err := CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RetireReference(repoDir, projDir, r.ID, " "); err == nil {
		t.Fatal("empty reason must be refused")
	}
	if _, err := RetireReference(repoDir, projDir, "R-9999", "x"); err == nil {
		t.Fatal("missing reference must error")
	}
	out, err := RetireReference(repoDir, projDir, r.ID, "duplicate of R-0002")
	if err != nil {
		t.Fatal(err)
	}
	if out.From != domain.ReferenceCurrent || out.To != domain.ReferenceObsolete {
		t.Fatalf("outcome = %+v", out)
	}
	got, _ := store.LoadReference(repoDir, r.ID)
	if got.State != domain.ReferenceObsolete {
		t.Fatalf("state = %s", got.State)
	}
	if p := createdPayload(t, repoDir, r.ID, "reference.obsoleted"); p["reason"] != "duplicate of R-0002" {
		t.Fatalf("payload = %v", p)
	}
	if _, err := RetireReference(repoDir, projDir, r.ID, "again"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("second retire err = %v, want ErrInvalidTransition", err)
	}
}

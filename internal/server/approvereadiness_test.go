package server

import (
	"net/http"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestFeatureDetailApproveReadiness (F-0308 crit. 7, server side): a
// candidate's detail carries its approve impediments and the bar the approve
// would accept, so the page can tell before any click; past candidacy there
// is nothing to approve and neither field is sent.
func TestFeatureDetailApproveReadiness(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Sem barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", "# t\n\nProblema real. [ESCLARECER: qual o limite?]\n")
	seedFeature(t, repoDir, "F-0002", "Com barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0002", "spec.md", "# t\n\nProblema real.\n\nBarra: cerne push --dry-run — ensaio com gate\n")
	// Complete cards (F-0336): only the spec impediments show.
	for _, id := range []string{"F-0001", "F-0002"} {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			t.Fatal(err)
		}
		f.Components = []string{"cli"}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatal(err)
		}
	}
	seedFeature(t, repoDir, "F-0003", "Aprovada", domain.StateApproved)
	writeDoc(t, repoDir, "F-0003", "spec.md", "# t\n\nProblema real.\n")

	type imp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hard    bool   `json:"hard"`
	}
	get := func(id string) (imps []imp, bar string, present bool) {
		t.Helper()
		var got struct {
			Readiness *[]imp `json:"approve_readiness"`
			Bar       string `json:"bar"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/"+id, "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", id, rec.Code, rec.Body.String())
		}
		if got.Readiness == nil {
			return nil, got.Bar, false
		}
		return *got.Readiness, got.Bar, true
	}

	imps, bar, _ := get("F-0001")
	if len(imps) != 2 || imps[0].Code != "bar-missing" || !imps[0].Hard ||
		imps[1].Code != "open-clarification" || imps[1].Hard || imps[1].Message != "qual o limite?" || bar != "" {
		t.Fatalf("F-0001 readiness = %+v bar %q, want hard bar-missing + soft open-clarification, no bar", imps, bar)
	}
	if imps, bar, _ := get("F-0002"); len(imps) != 0 || bar != "Barra: cerne push --dry-run — ensaio com gate" {
		t.Fatalf("F-0002 readiness = %+v bar %q, want none and the bar", imps, bar)
	}
	if imps, bar, present := get("F-0003"); present || bar != "" {
		t.Fatalf("F-0003 (approved) readiness = %+v bar %q, want neither field", imps, bar)
	}
}

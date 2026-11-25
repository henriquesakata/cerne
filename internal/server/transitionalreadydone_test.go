package server

import (
	"net/http"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0506 crit. 5: an accept on a done card answers 200 with from = to = done
// and writes no event.
func TestTransitionAcceptDoneIsNoOp(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	f.State = domain.StateDone
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	before, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition", `{"action":"accept"}`, &got)
	if rec.Code != http.StatusOK || got["from"] != "done" || got["to"] != "done" {
		t.Fatalf("status = %d body = %s, want 200 with from = to = done", rec.Code, rec.Body.String())
	}
	after, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("events %d → %d: the repeated accept wrote", len(before), len(after))
	}
}

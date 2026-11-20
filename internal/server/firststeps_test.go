package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

type firstStepsBody struct {
	FirstSteps []struct {
		Key  string `json:"key"`
		Done bool   `json:"done"`
	} `json:"first_steps"`
}

// TestState_FirstSteps (T-0011): /api/state lists the first steps from the
// event log until a feature is accepted, then an empty list.
func TestState_FirstSteps(t *testing.T) {
	repoDir, projDir := newBackend(t)
	get := func() firstStepsBody {
		var got firstStepsBody
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		return got
	}
	emit := func(id, typ string) {
		ev := domain.Event{ID: id, At: time.Now().UTC(), EntityID: "F-0001", Type: typ, Actor: "human"}
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}

	got := get()
	s := got.FirstSteps
	if len(s) != 3 || s[0].Key != "ask" || s[1].Key != "approve" || s[2].Key != "accept" || s[0].Done {
		t.Fatalf("fresh project: %+v", s)
	}
	emit("e1", "feature.created")
	if s = get().FirstSteps; !s[0].Done || s[1].Done {
		t.Fatalf("after created: %+v", s)
	}
	emit("e2", "feature.accepted")
	if s = get().FirstSteps; len(s) != 0 {
		t.Fatalf("after accept want empty, got %+v", s)
	}
}

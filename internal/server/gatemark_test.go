package server

import (
	"net/http"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// lastPayload is the payload of entity id's last event of type typ.
func lastPayload(t *testing.T, repoDir, id, typ string) map[string]string {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EntityID == id && evs[i].Type == typ {
			return evs[i].Payload
		}
	}
	t.Fatalf("%s has no %s event", id, typ)
	return nil
}

func postTransition(t *testing.T, repoDir, projDir, id, action string) {
	t.Helper()
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/"+id+"/transition", `{"action":"`+action+`"}`, &map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s: status = %d; body: %s", action, id, rec.Code, rec.Body.String())
	}
}

// TestTransitionFromUINeverMarksAuto: the person at the UI form is the human,
// whatever the server process's environment declares. Under the auto gates, a
// server started from an agent's shell approves and accepts with no mark, and
// a re-approval through the form clears a mark an agent's approve left.
// F-0515 crit. 6, 7
func TestTransitionFromUINeverMarksAuto(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CLAUDECODE", "1")
	repoDir, projDir, _ := newGitBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Approve: project.GateAuto, Accept: project.GateAuto}}); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real descrito aqui."))
	seedFeature(t, repoDir, "F-0002", "Ready card", domain.StateImplemented)
	commitBackend(t, repoDir)

	postTransition(t, repoDir, projDir, "F-0001", "approve")
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateApproved || f.ApprovedVia != "" {
		t.Fatalf("after the form's approve: %s, approved_via %q; want approved, empty", f.State, f.ApprovedVia)
	}
	if via := lastPayload(t, repoDir, "F-0001", "feature.approved")["via"]; via != "" {
		t.Errorf("approve event via = %q, want empty", via)
	}

	// An agent's CLI approve left the mark; the spec changed; the form re-approves.
	f, _ := store.LoadFeature(repoDir, "F-0001")
	f.ApprovedVia = project.GateAuto
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real descrito aqui, com um critério novo."))
	commitBackend(t, repoDir)
	postTransition(t, repoDir, projDir, "F-0001", "approve")
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.ApprovedVia != "" {
		t.Errorf("after the form's re-approval: approved_via %q, want empty", f.ApprovedVia)
	}

	postTransition(t, repoDir, projDir, "F-0002", "accept")
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.State != domain.StateDone || f.AcceptedVia != "" {
		t.Errorf("after the form's accept: %s, accepted_via %q; want done, empty", f.State, f.AcceptedVia)
	}
}

package server

import (
	"net/http"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0510 crit. 8: the promote route keeps its contract. In a project with a
// capability taxonomy an untagged idea still promotes (200, untagged card: the
// incomplete-card refusal is the CLI's), and tag fields in the body are
// ignored — the route grew no field.
func TestPromoteRouteKeepsItsContract(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	if _, err := app.CreateCapability(repoDir, projDir, "Gestão de demandas", ""); err != nil {
		t.Fatal(err)
	}
	h := New(repoDir, projDir, "")
	for _, title := range []string{"primeira", "segunda"} {
		if _, err := app.CreateIdea(repoDir, projDir, title, "", nil, nil, "", false); err != nil {
			t.Fatal(err)
		}
	}
	for id, body := range map[string]string{
		"I-0001": `{}`,
		"I-0002": `{"facets":["code"],"components":["cli"],"capabilities":["C-0001"]}`,
	} {
		var got struct {
			Feature store.Feature `json:"feature"`
		}
		rec := doJSON(t, h, http.MethodPost, "/api/ideas/"+id+"/promote", body, &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("promote %s %s = %d: %s", id, body, rec.Code, rec.Body.String())
		}
		f := got.Feature
		if f.State != "candidate" || len(f.Facets)+len(f.Components)+len(f.Capabilities) != 0 {
			t.Fatalf("promote %s %s = %+v; want an untagged candidate with no capability", id, body, f)
		}
	}
}

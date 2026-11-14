package server

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// F-0419 crit. 3: GET /ideas?state=&component=&search= is app.ListIdeas with
// the same filters, in the same order; an invalid state is a 400.
func TestIdeasAPIFilters(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	for _, body := range []string{
		`{"title":"zebra do kanban","components":["kanban"]}`,
		`{"title":"zebra do lint","components":["lint"]}`,
		`{"title":"outra do kanban","body":"zebra no corpo","components":["kanban"]}`,
	} {
		if rec := doJSON(t, h, http.MethodPost, "/api/ideas", body, &map[string]any{}); rec.Code != http.StatusCreated {
			t.Fatalf("POST %s = %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	if _, err := app.ResolveIdea(repoDir, projDir, "I-0002", "feito"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []app.IdeaFilter{
		{},
		{State: "all"},
		{State: "resolved"},
		{State: "all", Component: "kanban"},
		{State: "all", Search: "zebra"},
		{State: "active", Component: "kanban", Search: "zebra corpo"},
	} {
		want, err := app.ListIdeas(repoDir, f)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Ideas []struct {
				ID string `json:"id"`
			} `json:"ideas"`
		}
		path := "/api/ideas?state=" + f.State + "&component=" + f.Component + "&search=" + url.QueryEscape(f.Search)
		if rec := doJSON(t, h, http.MethodGet, path, "", &got); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
		}
		var gotIDs, wantIDs []string
		for _, i := range got.Ideas {
			gotIDs = append(gotIDs, i.ID)
		}
		for _, i := range want {
			wantIDs = append(wantIDs, i.ID)
		}
		if !slices.Equal(gotIDs, wantIDs) {
			t.Fatalf("GET %s = %v, want %v (app.ListIdeas %+v)", path, gotIDs, wantIDs, f)
		}
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/ideas?state=open", "", &map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET ?state=open = %d, want 400", rec.Code)
	}
}

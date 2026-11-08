package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0334 crit. 7: GET /pendings is app.ListPendings over query params — open
// by default, the filters, an empty selection as [] and a bad state as 400.
func TestPendingsRoute(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	if _, err := app.CreateFeature(repoDir, projDir, "Cobrir o lint", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	for _, c := range []struct {
		title, from string
		components  []string
	}{
		{"Cobrir links quebrados", "F-0001", []string{"lint"}},
		{"Documentar rota nova", "F-0002", nil},
		{"Ajustar cache do browser", "F-0001", nil},
	} {
		if _, err := app.CreatePending(repoDir, projDir, c.title, "", c.from, nil, c.components, false); err != nil {
			t.Fatalf("CreatePending %q: %v", c.title, err)
		}
	}
	if _, err := app.ResolvePending(repoDir, projDir, "P-0003"); err != nil {
		t.Fatal(err)
	}
	h := New(repoDir, projDir, "")
	for _, tc := range []struct {
		target string
		want   []string
	}{
		{"/api/pendings", []string{"P-0001", "P-0002"}},
		{"/api/pendings?state=resolved", []string{"P-0003"}},
		{"/api/pendings?state=all", []string{"P-0001", "P-0002", "P-0003"}},
		{"/api/pendings?from=f-0002", []string{"P-0002"}},
		{"/api/pendings?component=Lint", []string{"P-0001"}},
		{"/api/pendings?from=F-0099", []string{}},
	} {
		var doc struct {
			Pendings []store.Pending `json:"pendings"`
		}
		rec := doJSON(t, h, http.MethodGet, tc.target, "", &doc)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", tc.target, rec.Code, rec.Body.String())
		}
		ids := []string{}
		for _, p := range doc.Pendings {
			ids = append(ids, p.ID)
		}
		if !slices.Equal(ids, tc.want) {
			t.Fatalf("GET %s = %v, want %v", tc.target, ids, tc.want)
		}
		if len(tc.want) == 0 && !strings.Contains(rec.Body.String(), `"pendings": []`) { // writeJSON indents
			t.Fatalf("GET %s body = %s, want \"pendings\": []", tc.target, rec.Body.String())
		}
	}
	var bad struct {
		Error string `json:"error"`
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/pendings?state=bogus", "", &bad); rec.Code != http.StatusBadRequest || !strings.Contains(bad.Error, "bogus") {
		t.Fatalf("state=bogus = %d %q, want 400 naming bogus", rec.Code, bad.Error)
	}
}

// F-0334 crit. 7: like /ideas, the route lives on the local `cerne ui` and
// never on the remote `cerne serve`.
func TestPendingsRouteLocalOnly(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(dir string) (string, string, string, string, error) {
		if dir == "A" {
			return repoA, projA, "proj-a", "Projeto A", nil
		}
		return "", "", "", "", fmt.Errorf("dir desconhecido: %s", dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ui, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(ui)
	defer srv.Close()
	var doc struct {
		Pendings []store.Pending `json:"pendings"`
	}
	getJSON(t, srv.URL+"/api/projects/proj-a/pendings", &doc)
	if doc.Pendings == nil {
		t.Errorf("ui /pendings: want a pendings list, got none")
	}

	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	serve := NewMultiProject(mpSecret)
	doMP(t, serve, http.MethodPut, "/api/projects/proj-a", regBody(t, seedBareBackend(t, "Projeto A")), mpSecret, nil)
	if rec := doMP(t, serve, http.MethodGet, "/api/projects/proj-a/pendings", "", mpSecret, nil); rec.Code != http.StatusNotFound {
		t.Errorf("serve /pendings: status %d, want 404", rec.Code)
	}
}

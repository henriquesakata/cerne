package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestIdeasLeaveAPIOutputsUnchanged (F-0312 crit. 3, UI surfaces): /api/state
// (kanban, Backlog of candidates, mindmap, F-0233 gates), /api/events (F-0145
// heatmap, History, notifications), /api/stats, /api/lint, /api/graph,
// /api/work and /api/savings answer byte-identically before and after ideas.
// Teeth: reverting store.ReadEvents to the raw log fails on /api/events.
func TestIdeasLeaveAPIOutputsUnchanged(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	if _, err := app.CreateFeature(repoDir, projDir, "Feature B", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	h := New(repoDir, projDir, "")
	paths := []string{"/api/state", "/api/events", "/api/stats", "/api/lint", "/api/graph", "/api/work", "/api/savings"}
	snap := func() map[string]string {
		t.Helper()
		got := map[string]string{}
		for _, p := range paths {
			// /api/lint is stale-while-revalidate per backend HEAD (F-0153):
			// right after any commit it serves the PREVIOUS head's findings
			// with "stale": true. Compare the revalidated answer — the one
			// computed over the ideas — never the cached one.
			for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s = %d: %s", p, rec.Code, rec.Body.String())
				}
				got[p] = rec.Body.String()
				if p != "/api/lint" || !strings.Contains(got[p], `"stale": true`) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("GET /api/lint never revalidated: %s", got[p])
				}
			}
		}
		return got
	}
	diff := func(label string, a, b map[string]string) {
		t.Helper()
		for _, p := range paths {
			if a[p] != b[p] {
				t.Errorf("%s: GET %s changed\nbefore: %s\nafter:  %s", label, p, a[p], b[p])
			}
		}
	}
	control := snap()
	diff("control (no ideas yet)", control, snap())
	if t.Failed() {
		t.FailNow()
	}
	for _, text := range []string{"zebra listrada", "conciliação do OFX", "terceira"} {
		if _, err := app.CreateIdea(repoDir, projDir, text, "", nil, []string{"lint"}, ""); err != nil {
			t.Fatalf("CreateIdea: %v", err)
		}
	}
	if _, err := app.DropIdea(repoDir, projDir, "I-0003", "teste"); err != nil {
		t.Fatalf("DropIdea: %v", err)
	}
	after := snap()
	diff("after ideas", control, after)
	for p, body := range after {
		if strings.Contains(body, "I-0") || strings.Contains(body, "idea.") {
			t.Errorf("GET %s mentions an idea: %s", p, body)
		}
	}
}

// TestIdeasAPIRoundTrip (F-0312 crit. 10, server): capture, list (active,
// newest first), drop, promote — and the refusals.
func TestIdeasAPIRoundTrip(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	var created struct {
		Idea store.Idea `json:"idea"`
	}
	for _, text := range []string{"primeira", "segunda", "terceira"} {
		if rec := doJSON(t, h, http.MethodPost, "/api/ideas", fmt.Sprintf(`{"title":%q,"components":["kanban"]}`, text), &created); rec.Code != http.StatusCreated {
			t.Fatalf("POST %q = %d: %s", text, rec.Code, rec.Body.String())
		}
	}
	if created.Idea.ID != "I-0003" || created.Idea.Components[0] != "kanban" {
		t.Fatalf("created = %+v, want I-0003 with component kanban", created.Idea)
	}
	var e map[string]any
	if rec := doJSON(t, h, http.MethodPost, "/api/ideas", `{"title":" "}`, &e); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank text = %d, want 422", rec.Code)
	}
	var list struct {
		Ideas []store.Idea `json:"ideas"`
	}
	doJSON(t, h, http.MethodGet, "/api/ideas", "", &list)
	if len(list.Ideas) != 3 || list.Ideas[0].ID != "I-0003" || list.Ideas[2].ID != "I-0001" {
		t.Fatalf("GET = %+v, want I-0003..I-0001", list.Ideas)
	}
	var dropped struct {
		Idea store.Idea `json:"idea"`
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/ideas/I-0002/drop", `{}`, &dropped); rec.Code != http.StatusOK || dropped.Idea.State != "dropped" {
		t.Fatalf("drop = %d %+v", rec.Code, dropped.Idea)
	}
	doJSON(t, h, http.MethodGet, "/api/ideas", "", &list)
	if len(list.Ideas) != 2 || list.Ideas[0].ID != "I-0003" || list.Ideas[1].ID != "I-0001" {
		t.Fatalf("GET after drop = %+v, want I-0003, I-0001", list.Ideas)
	}
	var promoted struct {
		Idea    store.Idea    `json:"idea"`
		Feature store.Feature `json:"feature"`
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/ideas/I-0001/promote", `{}`, &promoted); rec.Code != http.StatusOK ||
		promoted.Feature.State != "candidate" || promoted.Idea.PromotedTo != promoted.Feature.ID {
		t.Fatalf("promote = %d %+v", rec.Code, promoted)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if !strings.Contains(rec.Body.String(), promoted.Feature.ID) {
		t.Fatalf("/api/state lacks the promoted candidate %s", promoted.Feature.ID)
	}
	rec = doJSON(t, h, http.MethodPost, "/api/ideas/I-0001/promote", `{}`, &e)
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "promoted") || !strings.Contains(body, promoted.Feature.ID) {
		t.Fatalf("second promote = %d %s, want 422 naming promoted and %s", rec.Code, body, promoted.Feature.ID)
	}
	for _, path := range []string{"/api/ideas/I-0099/drop", "/api/ideas/I-0099/promote"} {
		if rec := doJSON(t, h, http.MethodPost, path, `{}`, &e); rec.Code != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want 404", path, rec.Code)
		}
	}
}

// TestIdeaCaptureConcurrentAcrossSurfaces (F-0312 crit. 12): captures racing
// from the UI route and from the CLI's app call on one clone serialize on the
// backend lock — distinct ids, every text on origin, nothing corrupted.
func TestIdeaCaptureConcurrentAcrossSurfaces(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	h := New(repoDir, projDir, "")
	const n = 6
	ids, errs := make([]string, n), make([]error, n)
	var wg sync.WaitGroup
	for k := 0; k < n; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			text := fmt.Sprintf("corpo %d", k)
			if k%2 == 0 {
				i, err := app.CreateIdea(repoDir, projDir, text, "", nil, nil, "")
				ids[k], errs[k] = i.ID, err
				return
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ideas", strings.NewReader(fmt.Sprintf(`{"title":%q}`, text))))
			var got struct {
				Idea store.Idea `json:"idea"`
			}
			if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
				errs[k] = fmt.Errorf("POST = %d: %s", rec.Code, rec.Body.String())
				return
			}
			ids[k] = got.Idea.ID
		}(k)
	}
	wg.Wait()
	seen := map[string]int{}
	for k := 0; k < n; k++ {
		if errs[k] != nil {
			t.Fatalf("capture %d: %v", k, errs[k])
		}
		if _, dup := seen[ids[k]]; dup {
			t.Fatalf("duplicate id minted: %s", ids[k])
		}
		seen[ids[k]] = k
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(verify, "entities", "ideas", "*.json"))
	if len(files) != n {
		t.Fatalf("origin has %d idea files, want %d", len(files), n)
	}
	for id, k := range seen {
		var i store.Idea
		data, err := os.ReadFile(filepath.Join(verify, "entities", "ideas", id+".json"))
		if err != nil || json.Unmarshal(data, &i) != nil || i.Title != fmt.Sprintf("corpo %d", k) {
			t.Fatalf("%s on origin = %q, %v; want text 'corpo %d'", id, data, err, k)
		}
	}
}

// C6: an encoded traversal in the path is refused before any disk access.
func TestIdeaRoutesRefuseNonIdeaIDs(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	var e map[string]any
	for _, path := range []string{"/api/ideas/..%2Fnotes%2FN-0001/drop", "/api/ideas/F-0001/promote"} {
		rec := doJSON(t, h, http.MethodPost, path, `{}`, &e)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not an idea id") {
			t.Fatalf("POST %s = %d %s, want 422 not an idea id", path, rec.Code, rec.Body.String())
		}
	}
	if files, _ := filepath.Glob(filepath.Join(repoDir, "entities", "ideas", "*.json")); len(files) != 0 {
		t.Fatalf("a refused id planted %v", files)
	}
}

// C2: the route takes the capture's token — a re-POST of the same submission
// returns the idea already captured.
func TestIdeaCaptureRouteIsIdempotentByToken(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	var got struct {
		Idea store.Idea `json:"idea"`
	}
	for k := 0; k < 2; k++ {
		if rec := doJSON(t, h, http.MethodPost, "/api/ideas", `{"title":"uma vez só","token":"t-1"}`, &got); rec.Code != http.StatusCreated || got.Idea.ID != "I-0001" {
			t.Fatalf("POST #%d = %d %+v, want 201 I-0001", k+1, rec.Code, got.Idea)
		}
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 1 {
		t.Fatalf("the same token minted %d ideas", len(all))
	}
}

// C5: the idea write routes refuse a cross-origin or non-JSON request (the
// F-0316/F-0323 guard) — a page the user visits cannot plant ideas or mint
// candidates. Nothing is written.
func TestIdeaWriteRoutesRefuseCrossOrigin(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	if _, err := app.CreateIdea(repoDir, projDir, "alvo", "", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/ideas", "/api/ideas/I-0001/drop", "/api/ideas/I-0001/promote"} {
		for _, hdr := range []map[string]string{
			{"Origin": "https://evil.example", "Content-Type": "application/json"},
			{"Content-Type": "text/plain"},
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"title":"plantada"}`))
			for k, v := range hdr {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("POST %s %v = %d, want 403", path, hdr, rec.Code)
			}
		}
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 1 || all[0].State != "active" {
		t.Fatalf("a refused request wrote: %+v", all)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 {
		t.Fatalf("a refused promote minted a candidate: %d features", len(fs))
	}
}

// F-0389 crit. 5: GET /api/ideas carries the references informing each idea.
func TestIdeasAPIInformedBy(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	i, err := app.CreateIdea(repoDir, projDir, "com fonte", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateIdea(repoDir, projDir, "sem fonte", "", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateExternalReference(repoDir, projDir, "Artigo", "http://a", "c", []string{i.ID}, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Ideas []struct {
			ID         string            `json:"id"`
			InformedBy []store.Reference `json:"informed_by"`
		} `json:"ideas"`
	}
	doJSON(t, h, http.MethodGet, "/api/ideas", "", &list)
	if len(list.Ideas) != 2 {
		t.Fatalf("ideas = %+v", list.Ideas)
	}
	for _, it := range list.Ideas {
		want := 0
		if it.ID == i.ID {
			want = 1
		}
		if len(it.InformedBy) != want || (want == 1 && it.InformedBy[0].URL != "http://a") {
			t.Fatalf("%s informed_by = %+v, want %d", it.ID, it.InformedBy, want)
		}
	}
	if rec := httptest.NewRecorder(); true {
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ideas", nil))
		if strings.Contains(rec.Body.String(), `"informed_by": null`) {
			t.Fatal("informed_by must be [] not null")
		}
	}
}

// TestIdeasStateFilter (F-0416 crit. 9): no parameter keeps today's answer
// (active only); state=all adds the dropped and the promoted, in the order of
// app.ListIdeas(…, true, "") and with their sources; anything else is a 400.
func TestIdeasStateFilter(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	for _, text := range []string{"primeira", "segunda", "terceira"} {
		if rec := doJSON(t, h, http.MethodPost, "/api/ideas", fmt.Sprintf(`{"title":%q}`, text), &map[string]any{}); rec.Code != http.StatusCreated {
			t.Fatalf("POST %q = %d: %s", text, rec.Code, rec.Body.String())
		}
	}
	// A source on the idea that gets dropped: state=all must carry the
	// sources of the closed ones too, not only an empty key (critic F-0416 #4).
	if _, err := app.CreateExternalReference(repoDir, projDir, "Artigo", "http://a", "c", []string{"I-0002"}, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	doJSON(t, h, http.MethodPost, "/api/ideas/I-0002/drop", `{}`, &map[string]any{})
	doJSON(t, h, http.MethodPost, "/api/ideas/I-0001/promote", `{}`, &map[string]any{})
	type row struct {
		ID         string            `json:"id"`
		State      string            `json:"state"`
		InformedBy []store.Reference `json:"informed_by"`
	}
	get := func(path string) (int, []row) {
		var list struct {
			Ideas []row `json:"ideas"`
		}
		rec := doJSON(t, h, http.MethodGet, path, "", &list)
		return rec.Code, list.Ideas
	}
	ids := func(rs []row) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.ID+":"+r.State)
		}
		return strings.Join(out, ",")
	}
	if code, rs := get("/api/ideas"); code != http.StatusOK || ids(rs) != "I-0003:active" {
		t.Fatalf("GET /ideas = %d %s, want only I-0003:active", code, ids(rs))
	}
	code, rs := get("/api/ideas?state=all")
	if code != http.StatusOK || ids(rs) != "I-0003:active,I-0002:dropped,I-0001:promoted" {
		t.Fatalf("GET ?state=all = %d %s", code, ids(rs))
	}
	want, err := app.ListIdeas(repoDir, app.IdeaFilter{State: "all"})
	if err != nil || len(want) != len(rs) {
		t.Fatalf("app.ListIdeas(all) = %d ideas, %v", len(want), err)
	}
	for n, i := range want {
		sources := 0
		if i.ID == "I-0002" {
			sources = 1
		}
		if rs[n].ID != i.ID || rs[n].InformedBy == nil || len(rs[n].InformedBy) != sources {
			t.Fatalf("row %d = %+v, want %s with %d source(s)", n, rs[n], i.ID, sources)
		}
	}
	if rs[1].InformedBy[0].URL != "http://a" {
		t.Fatalf("the dropped idea's source = %+v, want http://a", rs[1].InformedBy[0])
	}
	if code, _ := get("/api/ideas?state=xyz"); code != http.StatusBadRequest {
		t.Fatalf("GET ?state=xyz = %d, want 400", code)
	}
}

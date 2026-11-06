package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
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
		if _, err := app.CreateIdea(repoDir, projDir, text, nil, []string{"lint"}); err != nil {
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

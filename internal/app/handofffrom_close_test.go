package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/henriquesakata/cerne/internal/notify"
	"github.com/henriquesakata/cerne/internal/store"
)

// Round 2, B: --from closes the origin the way `work done` does — its runbook
// run closes in the same commit and the webhook fires.
func TestStartWorkFromClosesRunAndFiresWebhook(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	var mu sync.Mutex
	var got []notify.Event
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var ev notify.Event
		_ = json.NewDecoder(r.Body).Decode(&ev)
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}))
	defer ts.Close()
	withWebhook(t, projDir, ts.URL)

	a := startW(t, repoDir, projDir, "Execução de runbook", nil)
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		return "seed run", store.SaveRun(repoDir, store.Run{ID: a.ID, Runbook: "RB-0001", State: store.RunOpen})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandoffWork(repoDir, projDir, a.ID, "brief", "n", nil, nil); err != nil {
		t.Fatal(err)
	}
	b, err := StartWork(repoDir, projDir, "Retomada da execução", nil, "agent-x", false, nil, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := store.LoadRun(repoDir, a.ID); err != nil || r.State != store.RunClosed {
		t.Fatalf("run of %s = %+v, %v; want closed", a.ID, r, err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, ev := range got {
		if ev.ID == a.ID && ev.Kind == "work" && ev.Action == "complete" && ev.From == "active" && ev.To == "done" {
			return
		}
	}
	t.Fatalf("no complete webhook for %s (new work %s): %+v", a.ID, b.ID, got)
}

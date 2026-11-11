package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0873 r1: an active run that stopped (last entry PARE) or finished (every
// step ok) without 'work done' is pointed at; a done run, one still going and
// a plain work are not. Advisory.
func TestRunStalledFindings(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	ok := func(s string) store.RunEntry { return store.RunEntry{Step: s, State: "ok"} }
	pare := store.RunEntry{Step: "S2", State: "pare", Output: "x"}
	mk := func(id string, st domain.WorkState, entries ...store.RunEntry) {
		if err := store.SaveWork(dir, store.Work{ID: id, Title: id, State: st, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveRun(dir, store.Run{ID: id, Runbook: "RB-0001", Window: "J" + id, Steps: []string{"S1", "S2"}, Entries: entries}); err != nil {
			t.Fatal(err)
		}
	}
	mk("W-0001", domain.WorkActive, ok("S1"), pare)     // stopped
	mk("W-0002", domain.WorkActive, ok("S1"), ok("S2")) // finished, not closed
	mk("W-0003", domain.WorkActive, ok("S1"))           // going on
	mk("W-0004", domain.WorkDone, ok("S1"), pare)       // closed
	mk("W-0005", domain.WorkActive)                     // just opened
	// F-0873 r2: the run's own state decides, not only the entries.
	mk("W-0006", domain.WorkActive, ok("S1"))
	setState := func(id, st string) {
		r, _ := store.LoadRun(dir, id)
		r.State = st
		if err := store.SaveRun(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	setState("W-0006", store.RunClosed) // concluida: never flagged, even with every step ok
	mk("W-0007", domain.WorkActive, ok("S1"), ok("S2"))
	setState("W-0007", store.RunClosed)
	works, err := store.LoadAllWorks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := runStalledFindings(dir, works)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, f := range got {
		if f.Rule != RuleRunStalled {
			t.Fatalf("rule = %q", f.Rule)
		}
		ids[f.Entity] = f.Detail
	}
	if len(ids) != 2 || ids["W-0001"] == "" || ids["W-0002"] == "" {
		t.Fatalf("findings = %+v", got)
	}
	if !strings.Contains(ids["W-0001"], "cerne work done W-0001") || !Advisory(RuleRunStalled) {
		t.Fatalf("detail/advisory: %q", ids["W-0001"])
	}
}

package app

import (
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// passBaton opens W-0001 on F-0001 and leaves HO-0001 on it.
func passBaton(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md")
	w := startW(t, repoDir, projDir, "Operação longa", []string{"F-0001"})
	if id, err := HandoffWork(repoDir, projDir, w.ID, "brief um", "rodar a suíte", nil); err != nil || id != "HO-0001" {
		t.Fatalf("HandoffWork = %q, %v", id, err)
	}
	return repoDir, projDir
}

// assertCloneUntouched: no commit since before and nothing dirty in the clone.
func assertCloneUntouched(t *testing.T, repoDir, before string) {
	t.Helper()
	if after, _ := gitback.HeadSHA(repoDir); after != before {
		t.Fatalf("HEAD moved %s -> %s", before, after)
	}
	out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Fatalf("clone diff not empty:\n%s", s)
	}
}

// F-0368 crit. 3: take opens a new work bound to this session, marks the HO
// taken with every field, closes the origin "via HO-", in one commit.
func TestTakeHandoff(t *testing.T) {
	repoDir, projDir := passBaton(t)
	cacheDir := filepath.Dir(repoDir)
	before, _ := gitback.HeadSHA(repoDir)
	r, err := TakeHandoff(repoDir, projDir, cacheDir, "sess-recv", "ho-0001", "", "li o brief")
	if err != nil {
		t.Fatalf("TakeHandoff: %v", err)
	}
	if r.ID != "W-0002" || r.Title != "Operação longa" || r.State != domain.WorkActive || !slices.Equal(r.Refs, []string{"F-0001"}) {
		t.Fatalf("new work = %+v", r.Work)
	}
	if !r.OriginClosed || !strings.Contains(r.Brief, "brief um") || !strings.Contains(r.Brief, "Próximo passo: rodar a suíte") {
		t.Fatalf("result = %+v", r)
	}
	h, _ := store.LoadHandoff(repoDir, "HO-0001")
	if h.State != domain.HandoffTaken || h.TakenBy == "" || h.TakenWork != "W-0002" || h.TakenSession != "sess-recv" ||
		h.TakenAt == nil || h.TakenNote != "li o brief" {
		t.Fatalf("handoff = %+v", h)
	}
	assertClosedByFrom(t, repoDir, "W-0001", "handed off to W-0002 via HO-0001")
	assertOneCommit(t, repoDir, before, "entities/handoffs/HO-0001.json", "entities/works/W-0001.json", "entities/works/W-0002.json")
	if n := countEvents(t, repoDir, "HO-0001", "handoff.taken"); n != 1 {
		t.Fatalf("handoff.taken events = %d", n)
	}
	if n := countEvents(t, repoDir, "W-0002", "work.started"); n != 1 {
		t.Fatalf("work.started events = %d", n)
	}
	if id, ok, err := CurrentBinding(cacheDir, repoDir, "sess-recv"); err != nil || !ok || id != "W-0002" {
		t.Fatalf("binding = %q %v %v, want W-0002", id, ok, err)
	}
}

// F-0368 crit. 4: a handoff that is not pending is refused with the why, and
// nothing is written — not the clone, not the session binding.
func TestTakeHandoffRefusals(t *testing.T) {
	repoDir, projDir := passBaton(t)
	cacheDir := filepath.Dir(repoDir)
	if _, err := TakeHandoff(repoDir, projDir, cacheDir, "sess-a", "HO-0001", "", ""); err != nil {
		t.Fatal(err)
	}
	w := startW(t, repoDir, projDir, "Outra frente bem distinta", nil)
	if _, err := HandoffWork(repoDir, projDir, w.ID, "b", "n", nil); err != nil { // HO-0002
		t.Fatal(err)
	}
	if _, err := HandoffWork(repoDir, projDir, w.ID, "b2", "n2", nil); err != nil { // HO-0003 supersedes HO-0002
		t.Fatal(err)
	}
	c := startW(t, repoDir, projDir, "Terceira coisa sem relação", nil)
	if _, err := HandoffWork(repoDir, projDir, c.ID, "b", "n", nil); err != nil { // HO-0004
		t.Fatal(err)
	}
	if err := CancelHandoff(repoDir, projDir, "HO-0004", "plano mudou"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id       string
		sentinel error
		want     []string
	}{
		{"HO-0001", ErrHandoffNotPending, []string{"taken", "W-0002"}},
		{"HO-0002", ErrHandoffNotPending, []string{"superseded", "HO-0003"}},
		{"HO-0004", ErrHandoffNotPending, []string{"cancelled", "plano mudou"}},
		{"HO-0099", store.ErrNotFound, []string{"not found"}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			before, _ := gitback.HeadSHA(repoDir)
			_, err := TakeHandoff(repoDir, projDir, cacheDir, "sess-b", tc.id, "", "")
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("err = %v, want %v", err, tc.sentinel)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("err %q does not say %q", err, w)
				}
			}
			assertCloneUntouched(t, repoDir, before)
			if _, ok, _ := CurrentBinding(cacheDir, repoDir, "sess-b"); ok {
				t.Fatalf("a refused take bound the session")
			}
		})
	}
}

// F-0368 crit. 5: an origin that is no longer active is left as it is.
func TestTakeHandoffOriginAlreadyClosed(t *testing.T) {
	repoDir, projDir := passBaton(t)
	if _, err := FinishWork(repoDir, projDir, "W-0001", "fechei antes", false, ""); err != nil {
		t.Fatal(err)
	}
	r, err := TakeHandoff(repoDir, projDir, filepath.Dir(repoDir), "", "HO-0001", "Retomada", "")
	if err != nil {
		t.Fatalf("TakeHandoff: %v", err)
	}
	if r.OriginClosed || r.Title != "Retomada" || !slices.Equal(r.Refs, []string{"F-0001"}) {
		t.Fatalf("result = %+v", r)
	}
	if w, _ := store.LoadWork(repoDir, "W-0001"); w.Note != "fechei antes" || w.State != domain.WorkDone {
		t.Fatalf("origin touched: %+v", w)
	}
	if h, _ := store.LoadHandoff(repoDir, "HO-0001"); h.State != domain.HandoffTaken || h.TakenWork != r.ID {
		t.Fatalf("handoff = %+v", h)
	}
}

// F-0368 crit. 6: work start --from takes the origin's pending HO with the
// given title; without one, it closes the origin as before.
func TestStartWorkFromTakesPendingHandoff(t *testing.T) {
	t.Setenv("CERNE_SESSION", "sess-from-start")
	repoDir, projDir := passBaton(t)
	w, err := StartWork(repoDir, projDir, "Retomada da operação", nil, "agent-x", false, nil, "W-0001")
	if err != nil {
		t.Fatal(err)
	}
	if w.Taken.ID != "HO-0001" {
		t.Fatalf("taken = %+v, want HO-0001", w.Taken)
	}
	h, _ := store.LoadHandoff(repoDir, "HO-0001")
	if h.State != domain.HandoffTaken || h.TakenWork != w.ID || h.TakenSession != "sess-from-start" {
		t.Fatalf("handoff = %+v", h)
	}
	assertClosedByFrom(t, repoDir, "W-0001", "handed off to "+w.ID+" via HO-0001")

	plain := startW(t, repoDir, projDir, "Sem bastão algum aqui", nil)
	t.Setenv("CERNE_SESSION", "")
	n, err := StartWork(repoDir, projDir, "Continuação simples", nil, "agent-x", false, nil, plain.ID)
	if err != nil || n.Taken.ID != "" {
		t.Fatalf("--from without HO: %+v, %v", n.Taken, err)
	}
	assertClosedByFrom(t, repoDir, plain.ID, "handed off to "+n.ID)
}

// F-0368 crit. 8: cancel needs a reason and a pending handoff.
func TestCancelHandoff(t *testing.T) {
	repoDir, projDir := passBaton(t)
	before, _ := gitback.HeadSHA(repoDir)
	if err := CancelHandoff(repoDir, projDir, "HO-0001", " "); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("no reason: err = %v", err)
	}
	assertCloneUntouched(t, repoDir, before)
	if err := CancelHandoff(repoDir, projDir, "ho-0001", "plano mudou"); err != nil {
		t.Fatal(err)
	}
	h, _ := store.LoadHandoff(repoDir, "HO-0001")
	if h.State != domain.HandoffCancelled || h.CancelReason != "plano mudou" {
		t.Fatalf("handoff = %+v", h)
	}
	if n := countEvents(t, repoDir, "HO-0001", "handoff.cancelled"); n != 1 {
		t.Fatalf("handoff.cancelled events = %d", n)
	}
	before, _ = gitback.HeadSHA(repoDir)
	if err := CancelHandoff(repoDir, projDir, "HO-0001", "de novo"); !errors.Is(err, ErrHandoffNotPending) {
		t.Fatalf("second cancel err = %v", err)
	}
	if err := CancelHandoff(repoDir, projDir, "HO-0042", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing cancel err = %v", err)
	}
	assertCloneUntouched(t, repoDir, before)
}

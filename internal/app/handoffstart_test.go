package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

func handoffIDs(hs []WorkHandoff) []string {
	var ids []string
	for _, h := range hs {
		ids = append(ids, h.ID)
	}
	return ids
}

// F-0339 crit. 8: --from inherits the refs, lets the guard pass against the
// origin, hands over its last entry and closes it as done — all in one commit.
func TestStartWorkFrom(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md")
	a := startW(t, repoDir, projDir, "Operação longa", []string{"F-0001"})
	if _, err := HandoffWork(repoDir, projDir, a.ID, "brief um", "rodar a suíte", nil, nil); err != nil {
		t.Fatal(err)
	}
	// Without --from the same title is refused by the guard (crit. 7: with the handoff).
	_, err := StartWork(repoDir, projDir, "Operação longa", nil, "agent-x", false, nil, "")
	if !errors.Is(err, ErrSimilarExists) || !strings.Contains(err.Error(), "Próximo passo: rodar a suíte") {
		t.Fatalf("guard refusal = %v, want ErrSimilarExists carrying the handoff", err)
	}
	var see *SimilarExistsError
	if !errors.As(err, &see) {
		t.Fatalf("not a SimilarExistsError: %v", err)
	}

	before, _ := gitback.HeadSHA(repoDir)
	b, err := StartWork(repoDir, projDir, "Operação longa", nil, "agent-x", false, nil, a.ID)
	if err != nil {
		t.Fatalf("StartWork --from: %v", err)
	}
	if !slices.Equal(b.Refs, []string{"F-0001"}) {
		t.Fatalf("refs = %v, want inherited [F-0001]", b.Refs)
	}
	if len(b.Handoffs) != 1 || b.Handoffs[0].ID != a.ID || b.Handoffs[0].Hint || !strings.Contains(b.Handoffs[0].Entry.Text, "brief um") {
		t.Fatalf("handoffs = %+v, want A's entry without hint", b.Handoffs)
	}
	assertClosedByFrom(t, repoDir, a.ID, "handed off to "+b.ID+" via HO-0001") // F-0368 crit. 6
	assertOneCommit(t, repoDir, before, "entities/works/"+a.ID+".json", "entities/works/"+b.ID+".json")
	if len(b.Superseded) != 0 {
		t.Fatalf("superseded = %v, want none", b.Superseded)
	}
}

// F-0339 crit. 8: an unknown or inactive origin refuses and creates nothing.
func TestStartWorkFromRefuses(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	done := startW(t, repoDir, projDir, "Já terminou", nil)
	if _, err := FinishWork(repoDir, projDir, done.ID, "", false, ""); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"W-9999", done.ID} {
		if _, err := StartWork(repoDir, projDir, "Algo novo e diferente", nil, "agent-x", false, nil, from); err == nil {
			t.Fatalf("--from %s accepted, want refusal", from)
		}
	}
	if _, err := store.LoadWork(repoDir, "W-0002"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused --from created W-0002: %v", err)
	}
}

// F-0339 crit. 6: a new work whose refs cross an active work with handoff gets
// that last entry, with the --from hint.
func TestStartWorkPrintsCrossingHandoff(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateInDevelopment, "spec.md", "plan.md")
	a := startW(t, repoDir, projDir, "Operação longa", []string{"F-0001"})
	if _, err := HandoffWork(repoDir, projDir, a.ID, "brief", "n1", nil, nil); err != nil {
		t.Fatal(err)
	}
	startW(t, repoDir, projDir, "Sem passagem alguma aqui", []string{"F-0001"})
	cross, err := StartWork(repoDir, projDir, "Algo diferente em paralelo", []string{"F-0001"}, "agent-x", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(handoffIDs(cross.Handoffs), []string{a.ID}) || !cross.Handoffs[0].Hint {
		t.Fatalf("handoffs = %+v, want only %s with hint", cross.Handoffs, a.ID)
	}
	other, err := StartWork(repoDir, projDir, "Assunto totalmente alheio", []string{"F-0002"}, "agent-x", false, nil, "")
	if err != nil || len(other.Handoffs) != 0 {
		t.Fatalf("no crossing: handoffs = %+v, err %v", other.Handoffs, err)
	}
}

// F-0339 + F-0341 seam, rules (a) to (c) of the TL contract.
func TestStartWorkHandoffSeamsWithPlanning(t *testing.T) {
	setup := func(t *testing.T, bound bool) (repoDir, projDir string) {
		t.Setenv("CERNE_SESSION", "sess-me")
		repoDir, projDir, _ = newFeatureBackend(t)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
		seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)
		if bound {
			if err := BindSession(filepath.Dir(repoDir), "sess-me", "W-0001"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := HandoffWork(repoDir, projDir, "W-0001", "brief", "n1", nil, nil); err != nil {
			t.Fatal(err)
		}
		return repoDir, projDir
	}

	t.Run("a: --from takes precedence over F-0341, closed once as handed off", func(t *testing.T) {
		repoDir, projDir := setup(t, true)
		w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001"}, "", false, nil, "W-0001")
		if err != nil {
			t.Fatal(err)
		}
		if len(w.Superseded) != 0 || len(w.Kept) != 0 {
			t.Fatalf("superseded %v kept %v, want none: --from owns W-0001", w.Superseded, w.Kept)
		}
		assertClosedByFrom(t, repoDir, "W-0001", "handed off to W-0002 via HO-0001") // F-0368 crit. 6
		if n := countEvents(t, repoDir, "W-0001", "work.done"); n != 1 {
			t.Fatalf("work.done events on W-0001 = %d, want 1", n)
		}
	})

	t.Run("b: closed by F-0341 in this call, handoff without hint", func(t *testing.T) {
		repoDir, projDir := setup(t, true)
		w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001"}, "", false, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(w.Superseded, []string{"W-0001"}) {
			t.Fatalf("superseded = %v, want [W-0001]", w.Superseded)
		}
		if len(w.Handoffs) != 1 || w.Handoffs[0].ID != "W-0001" || w.Handoffs[0].Hint {
			t.Fatalf("handoffs = %+v, want W-0001 without hint", w.Handoffs)
		}
	})

	t.Run("c: held by another session, handoff with hint", func(t *testing.T) {
		repoDir, projDir := setup(t, false)
		if err := BindSession(filepath.Dir(repoDir), "sess-other", "W-0001"); err != nil {
			t.Fatal(err)
		}
		w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001"}, "", false, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(w.Held, []string{"W-0001"}) {
			t.Fatalf("held = %v, want [W-0001]", w.Held)
		}
		if len(w.Handoffs) != 1 || w.Handoffs[0].ID != "W-0001" || !w.Handoffs[0].Hint {
			t.Fatalf("handoffs = %+v, want W-0001 with hint", w.Handoffs)
		}
	})

	t.Run("c: kept active, handoff with hint", func(t *testing.T) {
		repoDir, projDir := setup(t, false)
		w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001"}, "", false, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(w.Kept, []string{"W-0001"}) {
			t.Fatalf("kept = %v, want [W-0001]", w.Kept)
		}
		if len(w.Handoffs) != 1 || w.Handoffs[0].ID != "W-0001" || !w.Handoffs[0].Hint {
			t.Fatalf("handoffs = %+v, want W-0001 with hint", w.Handoffs)
		}
	})
}

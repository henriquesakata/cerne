package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestPlanningWorkRealCase (F-0341 crit. 9): the rockt-hub shape end to end on
// a temporary backend. The TL (<user>@<host>) opens the planning work, an
// agent process (CERNE_ACTOR=agent) starts the feature: the planning closes,
// the auto work opens and closes on complete, and CONTEXT.md lists neither.
// Then a second work start on the same feature supersedes the first when the
// same session re-registers, and only warns when it is not bound to it.
func TestPlanningWorkRealCase(t *testing.T) {
	t.Run("start closes the TL's planning", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		t.Setenv("USER", "u")
		t.Setenv("CERNE_SESSION", "")
		id := newFeature(t, projDir, "Feature A", 1)
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
		commitBackend(t, repoDir)
		mustCerne(t, "feature", "approve", id, "--dir", projDir)
		mustCerne(t, "work", "start", "F-0001: plano — planejamento", "--ref", id, "--dir", projDir)
		writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
		commitBackend(t, repoDir)

		t.Setenv("CERNE_ACTOR", "agent")
		mustCerne(t, "feature", "start", id, "--dir", projDir)
		wantWork(t, repoDir, "W-0001", domain.WorkDone, "planning closed: feature start")
		auto := wantWork(t, repoDir, "W-0002", domain.WorkActive, "")
		host, _ := os.Hostname() // the agent identity carries user and machine (F-0350)
		if !auto.Auto || auto.Actor != "agent:u@"+host {
			t.Fatalf("W-0002 = %+v, want the auto work of agent:u@%s", auto, host)
		}
		// CONTEXT.md lists active works by id: only the auto one now.
		if ctx := pulledContext(t, projDir); !strings.Contains(ctx, "W-0002") || strings.Contains(ctx, "W-0001") {
			t.Fatalf("CONTEXT.md after start, want W-0002 and not W-0001:\n%s", ctx)
		}

		writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue conforme o plano."))
		commitBackend(t, repoDir)
		mustCerne(t, "feature", "complete", id, "--dir", projDir)
		wantWork(t, repoDir, "W-0002", domain.WorkDone, "feature complete")

		ctx := pulledContext(t, projDir)
		for _, w := range []string{"W-0001", "W-0002"} {
			if strings.Contains(ctx, w) {
				t.Fatalf("CONTEXT.md still lists %s:\n%s", w, ctx)
			}
		}
	})

	t.Run("the same session's second work start supersedes the first", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		t.Setenv("USER", "u")
		t.Setenv("CERNE_SESSION", "sess-1")
		id := newFeature(t, projDir, "Feature A", 1)
		mustCerne(t, "work", "start", "F-0001: plano — planejamento", "--ref", id, "--dir", projDir)
		// What the PostToolUse hook does after the work start (F-0151).
		if err := app.BindSession(filepath.Dir(repoDir), "sess-1", "W-0001"); err != nil {
			t.Fatalf("BindSession: %v", err)
		}

		out := mustCerne(t, "work", "start", "Retomada do plano", "--ref", id, "--dir", projDir)
		if !strings.Contains(out, "closed W-0001 (superseded by W-0002)\n") {
			t.Fatalf("second work start output misses the closed line:\n%s", out)
		}
		wantWork(t, repoDir, "W-0001", domain.WorkDone, "superseded by W-0002")
		wantWork(t, repoDir, "W-0002", domain.WorkActive, "")
	})

	t.Run("an unbound planning work is only warned about", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		t.Setenv("USER", "u")
		t.Setenv("CERNE_SESSION", "sess-1")
		id := newFeature(t, projDir, "Feature A", 1)
		mustCerne(t, "work", "start", "F-0001: plano — planejamento", "--ref", id, "--dir", projDir)

		out := mustCerne(t, "work", "start", "Retomada do plano noutra sessão", "--ref", id, "--dir", projDir)
		if !strings.Contains(out, "W-0001 left active (not bound to this session); if it is finished: cerne work done W-0001\n") {
			t.Fatalf("second work start output misses the warning:\n%s", out)
		}
		wantWork(t, repoDir, "W-0001", domain.WorkActive, "")
		wantWork(t, repoDir, "W-0002", domain.WorkActive, "")
	})
}

// TestBatchWorkRealCase (F-0422 crit. 8): the TL's batch work on two features
// stays active while one of them is live and closes in the commit of the
// last one's exit, with the work-outlived-feature lint left silent. F-0002 is
// a candidate: the FSM rejects from candidate only.
func TestBatchWorkRealCase(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	t.Setenv("USER", "u")
	t.Setenv("CERNE_SESSION", "")
	a := newFeature(t, projDir, "Feature A", 1)
	b := newFeature(t, projDir, "Feature B", 2)
	writeDoc(t, repoDir, a, "spec.md", realDoc(a, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	mustCerne(t, "feature", "approve", a, "--dir", projDir)
	mustCerne(t, "work", "start", "Lote A e B — planejamento", "--ref", a+","+b, "--dir", projDir)
	writeDoc(t, repoDir, a, "plan.md", realDoc(a, "Passo 1: implementar."))
	commitBackend(t, repoDir)

	t.Setenv("CERNE_ACTOR", "agent")
	mustCerne(t, "feature", "start", a, "--dir", projDir)
	writeDoc(t, repoDir, a, "result.md", realDoc(a, "Entregue conforme o plano."))
	commitBackend(t, repoDir)
	mustCerne(t, "feature", "complete", a, "--dir", projDir)
	wantWork(t, repoDir, "W-0001", domain.WorkActive, "") // B still live

	mustCerne(t, "feature", "reject", b, "--reason", "x", "--dir", projDir)
	wantWork(t, repoDir, "W-0001", domain.WorkDone, "all refs closed (feature implemented, rejected)")
	out, _ := execCerne(t, "lint", "--dir", projDir)
	if strings.Contains(out, "W-0001") {
		t.Fatalf("lint still names W-0001:\n%s", out)
	}
}

// mustCerne runs a cerne command and fails the test on error.
func mustCerne(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execCerne(t, args...)
	if err != nil {
		t.Fatalf("cerne %s: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return out
}

// pulledContext runs `cerne pull` and returns the regenerated CONTEXT.md.
func pulledContext(t *testing.T, projDir string) string {
	t.Helper()
	mustCerne(t, "pull", "--dir", projDir)
	ctx, err := os.ReadFile(filepath.Join(projDir, ".docs", "CONTEXT.md"))
	if err != nil {
		t.Fatalf("read CONTEXT.md: %v", err)
	}
	return string(ctx)
}

// wantWork checks work id's state and note, returning it.
func wantWork(t *testing.T, repoDir, id string, state domain.WorkState, note string) store.Work {
	t.Helper()
	w, err := store.LoadWork(repoDir, id)
	if err != nil {
		t.Fatalf("LoadWork %s: %v", id, err)
	}
	if w.State != state || w.Note != note {
		t.Fatalf("%s = %s %q, want %s %q", id, w.State, w.Note, state, note)
	}
	return w
}

// TestPlanningWorkStartRepeatsFeatureTitle (F-0423 crit. 7): the exact F-0208
// pickup command, whose title repeats the approved feature's, opens the
// planning work; the same title without --ref (another agent, a similar
// registration) is still refused by the F-0147 guard.
func TestPlanningWorkStartRepeatsFeatureTitle(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	t.Setenv("USER", "u")
	t.Setenv("CERNE_SESSION", "")
	id := newFeature(t, projDir, "Painel de custo por feature", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	mustCerne(t, "feature", "approve", id, "--dir", projDir)

	title := "F-0001: Painel de custo por feature — planejamento"
	mustCerne(t, "work", "start", title, "--ref", id, "--dir", projDir)
	wantWork(t, repoDir, "W-0001", domain.WorkActive, "")

	out, err := execCerne(t, "work", "start", title, "--dir", projDir)
	if err == nil || !strings.Contains(err.Error()+out, "a similar registration already exists") {
		t.Fatalf("work start without --ref: err = %v, out = %s; want the duplicate refusal", err, out)
	}
}

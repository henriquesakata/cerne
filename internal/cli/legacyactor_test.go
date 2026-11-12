package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestShowAndLintWithAgentActors (F-0350 crit. 5): works and events whose
// actor is the legacy bare "agent" or the new "agent:<user>@<host>" keep
// `cerne show` working (the actor printed literally) and add no `cerne lint`
// finding of their own.
func TestShowAndLintWithAgentActors(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	baseline, _ := execCerne(t, "lint", "--dir", projDir)

	for i, a := range []string{"agent", "agent:alice@m1"} {
		mustCerne(t, "work", "start", "Atividade "+a, "--agent", a, "--dir", projDir)
		ev := domain.NewEvent(id, "feature.set", map[string]string{"k": "v"}, a)
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		wid := "W-000" + string(rune('1'+i))
		out := mustCerne(t, "show", wid, "--dir", projDir)
		if !strings.Contains(out, "actor: "+a+"\n") {
			t.Fatalf("show %s lacks the literal actor %q:\n%s", wid, a, out)
		}
	}
	commitBackend(t, repoDir)
	mustCerne(t, "show", id, "--dir", projDir)
	mustCerne(t, "show", id, "--json", "--dir", projDir)

	after, _ := execCerne(t, "lint", "--dir", projDir)
	if after != baseline {
		t.Fatalf("lint changed because of agent actors:\n--- before\n%s\n--- after\n%s", baseline, after)
	}
}

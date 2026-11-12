package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
)

// pinMachine fixes the user and host the actor identities derive from, so no
// test depends on the machine it runs on.
func pinMachine(t *testing.T, user, host string) {
	t.Helper()
	t.Setenv("USER", user)
	old := hostname
	hostname = func() (string, error) { return host, nil }
	t.Cleanup(func() { hostname = old })
}

// TestResolveActor (F-0350 crit. 1, 3): "agent" gains the machine identity,
// another explicit value is untouched, unset keeps each caller's fallback.
func TestResolveActor(t *testing.T) {
	pinMachine(t, "alice", "m1")
	for _, tc := range []struct{ env, wantEvent, wantWork string }{
		{"agent", "agent:alice@m1", "agent:alice@m1"},
		{"drift-auto", "drift-auto", "drift-auto"},
		{"agent:bob@m9", "agent:bob@m9", "agent:bob@m9"},
		{"", "local", "alice@m1"},
	} {
		t.Setenv("CERNE_ACTOR", tc.env)
		if got := EventActor(); got != tc.wantEvent {
			t.Errorf("CERNE_ACTOR=%q: EventActor = %q, want %q", tc.env, got, tc.wantEvent)
		}
		if got := autoWorkActor(); got != tc.wantWork {
			t.Errorf("CERNE_ACTOR=%q: autoWorkActor = %q, want %q", tc.env, got, tc.wantWork)
		}
	}
	t.Setenv("CERNE_ACTOR", "agent")
	if got := myActors(); len(got) != 2 || got[0] != "alice@m1" || got[1] != "agent:alice@m1" {
		t.Fatalf("myActors = %v", got)
	}
}

// TestHumanGateRecognizesEveryAgentForm (crit. 2): the env keeps meaning agent
// whether it is the stamped "agent" or the "agent:<…>" form; a human or another
// explicit actor passes.
func TestHumanGateRecognizesEveryAgentForm(t *testing.T) {
	for env, refused := range map[string]bool{"agent": true, "agent:alice@m1": true, "": false, "drift-auto": false, "agentx": false} {
		t.Setenv("CERNE_ACTOR", env)
		err := CheckHumanGate(project.GateHuman)
		if refused != errors.Is(err, ErrHumanGate) {
			t.Errorf("CERNE_ACTOR=%q: err = %v, refused want %v", env, err, refused)
		}
	}
}

// TestParallelGuardAgentIdentities (crit. 4 and the legacy rule): the same
// user on another machine is foreign; a pre-F-0350 bare "agent" work only
// warns, so `feature start` proceeds.
func TestParallelGuardAgentIdentities(t *testing.T) {
	pinMachine(t, "alice", "m1")
	t.Setenv("CERNE_ACTOR", "agent")
	for _, tc := range []struct {
		actor   string
		blocked bool
	}{
		{"agent:alice@m1", false},
		{"agent:alice@m2", true},
		{"agent", false},
	} {
		repoDir, projDir, _ := newFeatureBackend(t)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
		seedActiveWork(t, repoDir, "W-0001", tc.actor, "F-0001", false)
		_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
		if tc.blocked != errors.Is(err, ErrActiveWorkElsewhere) {
			t.Errorf("work by %q: start err = %v, want blocked=%v", tc.actor, err, tc.blocked)
		}
		if !tc.blocked && err != nil {
			t.Errorf("work by %q: start failed: %v", tc.actor, err)
		}
	}
}

// TestAgentIdentitiesNeverMine (F-0350 crit. 4, "só a mesma sessão"): the two
// paths that close a work BECAUSE it is "mine" — the adoption on complete and
// the supersede by `work start` of the session's bound planning — treat
// agent:alice@m1 as mine and agent:alice@m2 and the legacy bare "agent" as
// somebody else's, even when the work is bound to this very session. (The
// start's planning rule closes the planning of any actor by design, F-0341, so
// it cannot tell identities apart and is not what this pins.)
func TestAgentIdentitiesNeverMine(t *testing.T) {
	pinMachine(t, "alice", "m1")
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_SESSION", "sess-me")
	cases := []struct {
		actor string
		mine  bool
	}{{"agent:alice@m1", true}, {"agent:alice@m2", false}, {"agent", false}}
	for _, tc := range cases {
		t.Run("complete adoption/"+tc.actor, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
			seedStartEvent(t, repoDir, "F-0001", time.Now().UTC().Add(-time.Hour))
			seedActiveWork(t, repoDir, "W-0001", tc.actor, "F-0001", false)
			if err := BindSession(filepath.Dir(repoDir), "sess-me", "W-0001"); err != nil {
				t.Fatalf("BindSession: %v", err)
			}
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if !tc.mine {
				assertActive(t, repoDir, "W-0001")
				return
			}
			assertClosedAuto(t, repoDir, "W-0001", domain.WorkDone, "feature complete (adotado)")
		})
		t.Run("work start supersede/"+tc.actor, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
			seedActiveWork(t, repoDir, "W-0001", tc.actor, "F-0001", false)
			if err := BindSession(filepath.Dir(repoDir), "sess-me", "W-0001"); err != nil {
				t.Fatalf("BindSession: %v", err)
			}
			w, err := StartWork(repoDir, projDir, "Retomada do plano", []string{"F-0001"}, "", false, nil, "")
			if err != nil {
				t.Fatalf("StartWork: %v", err)
			}
			if got := len(w.Superseded) == 1; got != tc.mine {
				t.Fatalf("superseded %v kept %v held %v, mine = %v", w.Superseded, w.Kept, w.Held, tc.mine)
			}
			if !tc.mine {
				assertActive(t, repoDir, "W-0001")
			}
		})
	}
}

package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
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
	m1 := domain.MachinePseudonym("alice@m1")
	m9 := domain.MachinePseudonym("bob@m9")
	for _, tc := range []struct{ env, wantEvent, wantWork string }{
		{"agent", "agent:" + m1, "agent:" + m1},
		{"drift-auto", "drift-auto", "drift-auto"},
		{"agent:bob@m9", "agent:" + m9, "agent:" + m9},
		{"", "local", m1},
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
	if got := myActors(); len(got) != 2 || got[0] != m1 || got[1] != "agent:"+m1 {
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
		{"agent:alice@m1", false}, // legacy record, read as the pseudonym (F-0442 crit. 5)
		{"alice@m1", false},
		{"agent:" + domain.MachinePseudonym("alice@m1"), false}, // new record
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

// TestCursorAgentShellIsAgent (F-0428 crit. 6): Cursor's agent shells carry
// CURSOR_AGENT=1 and never the settings.json env block, so with no CERNE_ACTOR
// the marker makes the process an agent; an explicit CERNE_ACTOR still decides.
func TestCursorAgentShellIsAgent(t *testing.T) {
	pinMachine(t, "alice", "m1")
	t.Setenv("CURSOR_AGENT", "1")
	t.Setenv("CERNE_ACTOR", "")
	want := "agent:" + domain.MachinePseudonym("alice@m1")
	if got := EventActor(); got != want {
		t.Errorf("EventActor = %q, want %s", got, want)
	}
	if got := autoWorkActor(); got != want {
		t.Errorf("autoWorkActor = %q, want %s", got, want)
	}
	if err := CheckHumanGate(project.GateHuman); !errors.Is(err, ErrHumanGate) {
		t.Errorf("CheckHumanGate = %v, want ErrHumanGate", err)
	}
	if err := CheckHumanSetting("gates.gauntlet"); !errors.Is(err, ErrHumanGate) {
		t.Errorf("CheckHumanSetting = %v, want ErrHumanGate", err)
	}

	t.Setenv("CERNE_ACTOR", "fulano")
	if got := EventActor(); got != "fulano" {
		t.Errorf("explicit actor: EventActor = %q, want fulano", got)
	}
	if err := CheckHumanGate(project.GateHuman); err != nil {
		t.Errorf("explicit human actor: CheckHumanGate = %v, want nil", err)
	}

	t.Setenv("CERNE_ACTOR", "")
	t.Setenv("CURSOR_AGENT", "0")
	if err := CheckHumanGate(project.GateHuman); err != nil {
		t.Errorf("CURSOR_AGENT=0: CheckHumanGate = %v, want nil", err)
	}
}

// TestCursorAgentShellOrigin (F-0428 crit. 6): an entity a Cursor agent creates
// without --on-behalf is the agent's own request, like one from Claude Code.
func TestCursorAgentShellOrigin(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	t.Setenv("CURSOR_AGENT", "1")
	if origin, _, err := resolveOrigin(""); err != nil || origin != "agent" {
		t.Fatalf("resolveOrigin = %q, %v; want agent", origin, err)
	}
}

// TestWorkStartRecordsNoPersonalActor (F-0442 crit. 1, 3, 8): whatever the
// environment declares, `work start` records an actor without "@" that
// refusePersonal accepts, and an agent actor still cannot approve or accept.
func TestWorkStartRecordsNoPersonalActor(t *testing.T) {
	pinMachine(t, "alice", "mac.local")
	m := domain.MachinePseudonym("alice@mac.local")
	for _, tc := range []struct{ name, env, cursor, flag, want string }{
		{"agent env", "agent", "", "", "agent:" + m},
		{"cursor", "", "1", "", "agent:" + m},
		{"no actor", "", "", "", m},
		{"explicit name", "fulano", "", "", "fulano"},
		{"flag with @", "", "", "bob@x", domain.MachinePseudonym("bob@x")},
		{"agent flag with @", "", "", "agent:bob@x", "agent:" + domain.MachinePseudonym("bob@x")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CERNE_ACTOR", tc.env)
			t.Setenv("CURSOR_AGENT", tc.cursor)
			repoDir, projDir, _ := newFeatureBackend(t)
			out, err := StartWork(repoDir, projDir, "Investigar", nil, tc.flag, false, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			// the raw file, not LoadWork: the store would mask a bad write
			raw, err := os.ReadFile(filepath.Join(repoDir, "entities", "works", out.ID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var w struct{ Actor string }
			if err := json.Unmarshal(raw, &w); err != nil {
				t.Fatal(err)
			}
			if w.Actor != tc.want {
				t.Errorf("actor = %q, want %q", w.Actor, tc.want)
			}
			if err := refusePersonal("actor", w.Actor); err != nil {
				t.Errorf("refusePersonal(%q) = %v", w.Actor, err)
			}
		})
	}
	for _, env := range []string{"agent:" + m} {
		t.Setenv("CERNE_ACTOR", env)
		if err := CheckHumanGate(project.GateHuman); !errors.Is(err, ErrHumanGate) {
			t.Errorf("CERNE_ACTOR=%q: gate = %v, want ErrHumanGate", env, err)
		}
	}
}

// TestStoredLegacyWorkIsTheHouses (F-0442 crit. 5): works STORED as
// `agent:alice@m1` / `alice@m1` (the pre-F-0442 files) are the house's when the
// process is alice@m1 — myActors (the dupguard's and the parallel guard's
// test), the channel's session work and closeAutoWorks — and another actor's
// when it is bob@m1.
func TestStoredLegacyWorkIsTheHouses(t *testing.T) {
	for _, tc := range []struct {
		user string
		mine bool
	}{{"alice", true}, {"bob", false}} {
		for _, stored := range []string{"agent:alice@m1", "alice@m1"} {
			t.Run(tc.user+"/"+stored, func(t *testing.T) {
				pinMachine(t, tc.user, "m1")
				t.Setenv("CERNE_ACTOR", "agent")
				t.Setenv("CERNE_SESSION", "sess-me")
				repoDir, projDir, _ := newFeatureBackend(t)
				seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
				seedStartEvent(t, repoDir, "F-0001", time.Now().UTC().Add(-time.Hour))
				seedActiveWork(t, repoDir, "W-0001", stored, "F-0001", false)
				seedActiveWork(t, repoDir, "W-0002", stored, "F-0001", true)
				raw, _ := os.ReadFile(filepath.Join(repoDir, "entities", "works", "W-0001.json"))
				if !strings.Contains(string(raw), "alice@m1") {
					t.Fatalf("fixture is not a stored legacy work: %s", raw)
				}
				w, err := store.LoadWork(repoDir, "W-0001")
				if err != nil {
					t.Fatal(err)
				}
				if got := slices.Contains(myActors(), w.Actor); got != tc.mine {
					t.Errorf("myActors contains %q = %v, want %v", w.Actor, got, tc.mine)
				}
				cacheDir := filepath.Dir(repoDir)
				if err := BindSession(cacheDir, "sess-me", "F-0001"); err != nil {
					t.Fatal(err)
				}
				if _, ok := sessionWork(cacheDir, repoDir, "sess-me"); ok != tc.mine {
					// the channel resolves the auto work of any identity of this process
					t.Errorf("sessionWork resolved = %v for %s as %s", ok, stored, tc.user)
				}
				// the adoption on complete: only the house's own manual work, bound here
				if err := BindSession(cacheDir, "sess-me", "W-0001"); err != nil {
					t.Fatal(err)
				}
				if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
					t.Fatalf("complete: %v", err)
				}
				after, _ := store.LoadWork(repoDir, "W-0001")
				if closed := after.State != domain.WorkActive; closed != tc.mine {
					t.Errorf("manual work %s as %s: closed = %v, want %v", stored, tc.user, closed, tc.mine)
				}
			})
		}
	}
}

// TestLegacyAgentActorStillRefusedAtHumanGates (F-0442 crit. 8): an old
// environment that still says CERNE_ACTOR=agent:<login>@<host> is an agent: the
// human gates (approve, accept) refuse it, and the event the transition records
// carries the pseudonym, never the "@".
func TestLegacyAgentActorStillRefusedAtHumanGates(t *testing.T) {
	pinMachine(t, "alice", "mac.local")
	t.Setenv("CERNE_ACTOR", "agent:alice@mac.local")
	if err := CheckHumanGate(project.GateHuman); !errors.Is(err, ErrHumanGate) {
		t.Errorf("CheckHumanGate = %v, want ErrHumanGate", err)
	}
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented, "spec.md", "plan.md", "result.md")
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "ok", nil); err != nil {
		t.Fatal(err)
	}
	scanForActorAt(t, repoDir)
	evs, _ := store.ReadEvents(repoDir)
	if n := len(evs); n == 0 || evs[n-1].Actor != "agent:"+domain.MachinePseudonym("alice@mac.local") {
		t.Errorf("event actor = %+v", evs)
	}
}

// TestCopilotCLIShellIsAgent (F-0444): Copilot CLI marks the shell of its bash
// tool with COPILOT_CLI=1 (probed on 1.0.112), and the hooks' env block does
// not reach that shell — so the marker makes the process an agent; an explicit
// CERNE_ACTOR still decides.
func TestCopilotCLIShellIsAgent(t *testing.T) {
	pinMachine(t, "alice", "m1")
	t.Setenv("COPILOT_CLI", "1")
	t.Setenv("CERNE_ACTOR", "")
	want := "agent:" + domain.MachinePseudonym("alice@m1")
	if got := EventActor(); got != want || strings.Contains(got, "@") {
		t.Errorf("EventActor = %q, want %s (pseudonym, no login@host)", got, want)
	}
	if got := autoWorkActor(); got != want {
		t.Errorf("autoWorkActor = %q, want %s", got, want)
	}
	if err := CheckHumanGate(project.GateHuman); !errors.Is(err, ErrHumanGate) {
		t.Errorf("CheckHumanGate = %v, want ErrHumanGate", err)
	}
	t.Setenv("CERNE_ACTOR", "human")
	if err := CheckHumanGate(project.GateHuman); errors.Is(err, ErrHumanGate) {
		t.Errorf("explicit CERNE_ACTOR=human must decide, got %v", err)
	}
}

// agentShell sets the agent-shell markers for one test: claudeCode is the
// CLAUDECODE value, actor the CERNE_ACTOR one, and unset drops CERNE_ACTOR
// (an empty value and an absent variable are both "no explicit actor"). The
// Cursor/Copilot markers the invoking shell may carry are cleared (N-0058).
func agentShell(t *testing.T, claudeCode, actor string, unset bool) {
	t.Helper()
	t.Setenv("CURSOR_AGENT", "")
	t.Setenv("COPILOT_CLI", "")
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CLAUDECODE", claudeCode)
	t.Setenv("CERNE_ACTOR", actor)
	if unset {
		os.Unsetenv("CERNE_ACTOR")
	}
}

// TestActorEnvClaudeCode (B-0030): Claude Code exports CLAUDECODE=1 in every
// shell it runs, so with no explicit CERNE_ACTOR the marker makes the process
// the legacy agent; CERNE_ACTOR=human is the human's way out of such a shell.
func TestActorEnvClaudeCode(t *testing.T) {
	for _, tc := range []struct {
		name, claudeCode, actor string
		unset                   bool
		want                    string
	}{
		{"marker alone", "1", "", true, legacyAgentActor},
		{"marker with empty CERNE_ACTOR", "1", "", false, legacyAgentActor},
		{"marker with CERNE_ACTOR=human", "1", "human", false, "human"},
		{"no marker", "", "", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentShell(t, tc.claudeCode, tc.actor, tc.unset)
			if got := actorEnv(); got != tc.want {
				t.Errorf("actorEnv = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClaudeCodeShellHitsHumanGates (B-0030): with only CLAUDECODE=1 the human
// gates refuse — a human-gated transition, a human-only setting — and a new
// card's origin is the agent's; CERNE_ACTOR=human passes them.
func TestClaudeCodeShellHitsHumanGates(t *testing.T) {
	agentShell(t, "1", "", true)
	if err := CheckHumanGate(project.GateHuman); !errors.Is(err, ErrHumanGate) {
		t.Errorf("CheckHumanGate = %v, want ErrHumanGate", err)
	}
	if err := CheckHumanSetting("gates.gauntlet"); !errors.Is(err, ErrHumanGate) {
		t.Errorf("CheckHumanSetting(gates.gauntlet) = %v, want ErrHumanGate", err)
	}
	if origin, _, err := resolveOrigin(""); err != nil || origin != "agent" {
		t.Errorf("resolveOrigin = %q, %v; want agent", origin, err)
	}

	t.Setenv("CERNE_ACTOR", "human")
	if err := CheckHumanGate(project.GateHuman); err != nil {
		t.Errorf("CERNE_ACTOR=human: CheckHumanGate = %v, want nil", err)
	}
	if err := CheckHumanSetting("gates.gauntlet"); err != nil {
		t.Errorf("CERNE_ACTOR=human: CheckHumanSetting = %v, want nil", err)
	}
}

// TestCodexShellHitsHumanGates (B-0051): Codex exports CODEX_THREAD_ID (an id,
// not "1") in its tool shells; without the project config that stamps
// CERNE_ACTOR, the marker alone makes the shell an agent's.
func TestCodexShellHitsHumanGates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		unset bool
	}{{"no CERNE_ACTOR", true}, {"empty CERNE_ACTOR", false}} {
		t.Run(tc.name, func(t *testing.T) {
			agentShell(t, "", "", tc.unset)
			t.Setenv("CODEX_THREAD_ID", "019e-thread")
			if got := actorEnv(); got != legacyAgentActor {
				t.Errorf("actorEnv = %q, want %q", got, legacyAgentActor)
			}
			if err := CheckHumanGate(project.GateHuman); !errors.Is(err, ErrHumanGate) {
				t.Errorf("CheckHumanGate = %v, want ErrHumanGate", err)
			}
			if err := CheckHumanSetting("gates.gauntlet"); !errors.Is(err, ErrHumanGate) {
				t.Errorf("CheckHumanSetting = %v, want ErrHumanGate", err)
			}
			if origin, _, err := resolveOrigin(""); err != nil || origin != "agent" {
				t.Errorf("resolveOrigin = %q, %v; want agent", origin, err)
			}
		})
	}
}

func TestCodexMarkerYieldsToExplicitHuman(t *testing.T) {
	agentShell(t, "", "human", false)
	t.Setenv("CODEX_THREAD_ID", "019e-thread")
	if err := CheckHumanGate(project.GateHuman); err != nil {
		t.Errorf("CERNE_ACTOR=human + marker: CheckHumanGate = %v, want nil", err)
	}
	if err := CheckHumanSetting("gates.gauntlet"); err != nil {
		t.Errorf("CERNE_ACTOR=human + marker: CheckHumanSetting = %v, want nil", err)
	}
}

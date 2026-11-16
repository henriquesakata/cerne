package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// T-0007: tasks inherit their parent's gates — approve and accept cascade,
// and each task declares its own bar. The tests are named per criterion.

const (
	taskBody      = "# Doc\n\n## Problema\n\nCorpo real do documento, não um stub.\n\n## Verificação\n\n"
	namedTaskBar  = "Barra: fixture de teste — referência nomeada\nPergunta ao juiz: qual versão é mais clara?\n"
	exemptTaskBar = "Barra: não aplicável (regra de gate) — padrão alternativo: testes\n"
)

// seedWithSpec persists f with spec as its spec.md (no bar appended) and
// commits, like seedFeatureInState.
func seedWithSpec(t *testing.T, repoDir string, f store.Feature, spec string) {
	t.Helper()
	if f.Title == "" {
		f.Title = "Feature " + f.ID
	}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	path := FeatureDocPath(repoDir, f.ID, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(completeSpec(spec)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+f.ID); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
}

// TestStartTaskRefusesWithoutBar (crit. 4): a task whose spec declares no
// valid bar is refused at start, before any write; an inherited bar must name
// the task's own parent, whose bar must be a named one. Light mode charges no
// bar, so the start passes there.
func TestStartTaskRefusesWithoutBar(t *testing.T) {
	for _, tc := range []struct {
		name, parentBar, taskBar string
	}{
		{"no bar", namedTaskBar, "Só testes.\n"},
		{"malformed bar", namedTaskBar, "Barra: não aplicável\n"},
		{"inherited from another feature", namedTaskBar, "Barra: herdada de F-0099\n"},
		{"inherited from a parent with no named bar", exemptTaskBar, "Barra: herdada de E-0001\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedWithSpec(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateInDevelopment}, taskBody+tc.parentBar)
			seedWithSpec(t, repoDir, store.Feature{ID: "T-0001", Parent: "E-0001", State: domain.StateApproved}, taskBody+tc.taskBar)
			before, err := store.ReadEvents(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			_, err = TransitionFeature(repoDir, projDir, "T-0001", domain.ActionStart, "", "", nil)
			if !errors.Is(err, ErrTaskBarRequired) {
				t.Fatalf("start err = %v, want ErrTaskBarRequired", err)
			}
			if f := reload(t, repoDir, "T-0001"); f.State != domain.StateApproved {
				t.Fatalf("state = %s, want approved (nothing written)", f.State)
			}
			if after, _ := store.ReadEvents(repoDir); len(after) != len(before) {
				t.Fatalf("a refused start appended %d event(s)", len(after)-len(before))
			}
		})
	}
	t.Run("light mode", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		saveGauntlet(t, projDir, "off")
		seedWithSpec(t, repoDir, store.Feature{ID: "T-0001", State: domain.StateApproved}, taskBody+"Só testes.\n")
		if _, err := TransitionFeature(repoDir, projDir, "T-0001", domain.ActionStart, "", "", nil); err != nil {
			t.Fatalf("light mode charges no bar, start err = %v", err)
		}
	})
}

// TestNextStepTaskBar (crit. 4, F-0371): NextStep calls the task's bar gate,
// so it never sends the agent to a start that refuses.
func TestNextStepTaskBar(t *testing.T) {
	proj, repo := nextStepRepo(t)
	if err := store.SaveFeature(repo, store.Feature{ID: "T-0001", Title: "t", State: domain.StateApproved}); err != nil {
		t.Fatal(err)
	}
	nextStepDoc(t, repo, "T-0001", "spec.md", taskBody+"Só testes.\n")
	s, err := NextStep(repo, proj, "T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if s.Command != "" || !strings.Contains(s.Step, "the start would refuse") || !strings.Contains(s.Step, "spec.md") {
		t.Fatalf("step = %+v, want the start's bar refusal pointing at spec.md", s)
	}
}

// TestApproveRefusesInheritedBarOnFeature (crit. 4): the inherited form is a
// task's only — an F-/E- spec carrying it has no bar of its own, refused by the
// approve gate and listed by the check as a malformed bar.
func TestApproveRefusesInheritedBarOnFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedWithSpec(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateApproved}, taskBody+namedTaskBar)
	seedWithSpec(t, repoDir, store.Feature{ID: "F-0001", Parent: "E-0001", State: domain.StateCandidate,
		Facets: []string{"code"}, Components: []string{"app"}}, taskBody+"Barra: herdada de E-0001\n")
	imps, err := ApproveReadiness(repoDir, projDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(imps) != 1 || imps[0].Code != ImpedimentBarMalformed || !strings.Contains(imps[0].Message, "only a task") {
		t.Fatalf("impediments = %+v, want one bar-malformed naming the task-only form", imps)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); !errors.Is(err, ErrSpecMissingBar) {
		t.Fatalf("approve err = %v, want ErrSpecMissingBar", err)
	}
}

// tagged is the card a candidate needs to pass the approve check (F-0336).
func tagged(id, parent string, s domain.FeatureState) store.Feature {
	return store.Feature{ID: id, Parent: parent, State: s, Facets: []string{"code"}, Components: []string{"app"}}
}

// entityEvents returns the events of entity id of type typ.
func entityEvents(t *testing.T, repoDir, id, typ string) []domain.Event {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Event
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// seedEpicWithChildren seeds candidate E-0001 and its children: F-0002 ready,
// F-0003 untagged, F-0004 with no bar, F-0005 already approved.
func seedEpicWithChildren(t *testing.T, repoDir string) {
	t.Helper()
	seedWithSpec(t, repoDir, tagged("E-0001", "", domain.StateCandidate), taskBody+namedTaskBar)
	seedWithSpec(t, repoDir, tagged("F-0002", "E-0001", domain.StateCandidate), taskBody+namedTaskBar)
	seedWithSpec(t, repoDir, store.Feature{ID: "F-0003", Parent: "E-0001", State: domain.StateCandidate}, taskBody+namedTaskBar)
	seedWithSpec(t, repoDir, tagged("F-0004", "E-0001", domain.StateCandidate), taskBody+"Só testes.\n")
	seedWithSpec(t, repoDir, tagged("F-0005", "E-0001", domain.StateApproved), taskBody+namedTaskBar)
}

// TestApproveCascadesToReadyChildren (crit. 1): a human approving the parent
// approves, in the same transition, each candidate child the approve check
// passes — one event per child carrying cascade_from, its approved spec kept
// and its plan scaffolded — and lists the others with the reason, left in
// candidate.
func TestApproveCascadesToReadyChildren(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedEpicWithChildren(t, repoDir)

	out, err := TransitionFeature(repoDir, projDir, "E-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("approve E-0001: %v", err)
	}
	for id, want := range map[string]domain.FeatureState{"E-0001": domain.StateApproved, "F-0002": domain.StateApproved,
		"F-0003": domain.StateCandidate, "F-0004": domain.StateCandidate, "F-0005": domain.StateApproved} {
		if got := reload(t, repoDir, id).State; got != want {
			t.Errorf("%s state = %s, want %s", id, got, want)
		}
	}
	evs := entityEvents(t, repoDir, "F-0002", "feature.approved")
	if len(evs) != 1 || evs[0].Payload["cascade_from"] != "E-0001" || evs[0].Payload["bar"] == "" {
		t.Fatalf("F-0002 approve events = %+v, want one carrying cascade_from E-0001 and the bar", evs)
	}
	for _, id := range []string{"F-0003", "F-0004", "F-0005"} {
		if evs := entityEvents(t, repoDir, id, "feature.approved"); len(evs) != 0 {
			t.Errorf("%s got approve events %+v, want none", id, evs)
		}
	}
	if _, err := os.Stat(store.ApprovedSpecPath(repoDir, "F-0002")); err != nil {
		t.Errorf("F-0002 approved spec not kept: %v", err)
	}
	if _, err := os.Stat(FeatureDocPath(repoDir, "F-0002", "plan.md")); err != nil {
		t.Errorf("F-0002 plan not scaffolded: %v", err)
	}
	lines := strings.Join(out.Cascade, "\n")
	for _, want := range []string{"F-0002: candidate → approved along with E-0001", "F-0003 stays in candidate: untagged", "F-0004 stays in candidate: bar-missing", "cerne feature approve F-0004 --check"} {
		if !strings.Contains(lines, want) {
			t.Errorf("cascade lines lack %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "F-0005") {
		t.Errorf("an already approved child is no part of the cascade:\n%s", lines)
	}
}

// TestApproveCascadeRespectsHumanGate (crit. 2): an agent that may not approve
// the parent approves no child. The CLI refuses the parent before the core;
// the core, which the UI calls with no gate of its own, checks the gate before
// cascading, so even a parent it lets through takes no child along.
func TestApproveCascadeRespectsHumanGate(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedEpicWithChildren(t, repoDir)

	out, err := TransitionFeature(repoDir, projDir, "E-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("approve E-0001: %v", err)
	}
	if got := reload(t, repoDir, "F-0002").State; got != domain.StateCandidate {
		t.Fatalf("F-0002 state = %s, want candidate: an agent approves no child", got)
	}
	if evs := entityEvents(t, repoDir, "F-0002", "feature.approved"); len(evs) != 0 {
		t.Fatalf("F-0002 approve events = %+v, want none", evs)
	}
	// The skipped cascade says so, for the CLI and the UI to show.
	if len(out.Cascade) != 1 || !strings.Contains(out.Cascade[0], "children not carried along with E-0001: the human gate") {
		t.Fatalf("cascade lines = %q, want one saying the human gate kept the children", out.Cascade)
	}
}

// TestCascadeHumanGateSilentWithoutChildren: a refused cascade with nothing to
// carry adds no line — a childless approve reads as before.
func TestCascadeHumanGateSilentWithoutChildren(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedWithSpec(t, repoDir, tagged("F-0001", "", domain.StateCandidate), taskBody+namedTaskBar)
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("approve F-0001: %v", err)
	}
	if len(out.Cascade) != 0 {
		t.Fatalf("cascade lines = %q, want none for a feature with no children", out.Cascade)
	}
}

// TestAcceptCascadesToTasks (crit. 3): a human accepting the parent accepts,
// in the same transition, each implemented task under it — one event per task
// carrying cascade_from; a task still in development stays.
func TestAcceptCascadesToTasks(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "E-0001", domain.StateImplemented)
	for id, s := range map[string]domain.FeatureState{"T-0002": domain.StateImplemented, "T-0003": domain.StateInDevelopment} {
		seedWithSpec(t, repoDir, store.Feature{ID: id, Parent: "E-0001", State: s}, taskBody+exemptTaskBar)
	}

	out, err := TransitionFeature(repoDir, projDir, "E-0001", domain.ActionAccept, "", "", nil)
	if err != nil {
		t.Fatalf("accept E-0001: %v", err)
	}
	for id, want := range map[string]domain.FeatureState{"E-0001": domain.StateDone, "T-0002": domain.StateDone, "T-0003": domain.StateInDevelopment} {
		if got := reload(t, repoDir, id).State; got != want {
			t.Errorf("%s state = %s, want %s", id, got, want)
		}
	}
	if evs := entityEvents(t, repoDir, "T-0002", "feature.accepted"); len(evs) != 1 || evs[0].Payload["cascade_from"] != "E-0001" {
		t.Fatalf("T-0002 accept events = %+v, want one carrying cascade_from E-0001", evs)
	}
	if lines := strings.Join(out.Cascade, "\n"); !strings.Contains(lines, "T-0002: implemented → done along with E-0001") || strings.Contains(lines, "T-0003") {
		t.Fatalf("cascade lines = %q, want T-0002 accepted and T-0003 absent", lines)
	}
}

// TestAcceptCascadeSkipsFeatureChildren (crit. 3): an F- child had its own
// approve and keeps its own accept; an agent actor takes no task along either
// (the accept's human gate, as crit. 2 for the approve).
func TestAcceptCascadeSkipsFeatureChildren(t *testing.T) {
	for _, actor := range []string{"", "agent"} {
		t.Run("actor="+actor, func(t *testing.T) {
			t.Setenv("CERNE_ACTOR", actor)
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "E-0001", domain.StateImplemented)
			seedWithSpec(t, repoDir, store.Feature{ID: "F-0002", Parent: "E-0001", State: domain.StateImplemented}, taskBody+namedTaskBar)
			seedWithSpec(t, repoDir, store.Feature{ID: "T-0003", Parent: "E-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)
			out, err := TransitionFeature(repoDir, projDir, "E-0001", domain.ActionAccept, "", "", nil)
			if err != nil {
				t.Fatalf("accept E-0001: %v", err)
			}
			if gated := strings.Contains(strings.Join(out.Cascade, "\n"), "children not carried along with E-0001: the human gate"); gated != (actor == "agent") {
				t.Fatalf("cascade lines = %q: the human-gate line must show exactly when the gate refused", out.Cascade)
			}
			if got := reload(t, repoDir, "F-0002").State; got != domain.StateImplemented {
				t.Fatalf("F-0002 state = %s, want implemented: an F- child keeps its own accept", got)
			}
			want := domain.StateDone
			if actor == "agent" {
				want = domain.StateImplemented
			}
			if got := reload(t, repoDir, "T-0003").State; got != want {
				t.Fatalf("T-0003 state = %s, want %s", got, want)
			}
		})
	}
}

// TestEpicCloseRosterListsChildBars (crit. 7): the epic's closing scaffold
// lists each child with the bar its spec declares and its outcome — the
// rounds of its result, or none for an exempt or inherited bar — so the
// parent's accept reads every choice at once.
func TestEpicCloseRosterListsChildBars(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedWithSpec(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateInDevelopment}, taskBody+namedTaskBar)
	seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateImplemented}, taskBody+namedTaskBar)
	seedWithSpec(t, repoDir, store.Feature{ID: "T-0003", Parent: "E-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)
	seedWithSpec(t, repoDir, store.Feature{ID: "T-0004", Parent: "E-0001", State: domain.StateInDevelopment}, taskBody+"Barra: herdada de E-0001\n")
	for id, body := range map[string]string{
		"T-0002": "# R\n\n## Resultado\n\nEntregue.\n\n## Gauntlet\n\nBarra: fixture de teste\nRodada 1 — derrota: \"B cobre o vazio\"\nRodada 2 — vitória: \"A cobre tudo\"\n",
		"T-0004": "# R\n\n## Resultado\n\nEntregue.\n",
	} {
		if err := os.WriteFile(FeatureDocPath(repoDir, id, "result.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "results"); err != nil {
		t.Fatal(err)
	}

	out, err := TransitionFeature(repoDir, projDir, "T-0004", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete T-0004: %v", err)
	}
	if out.EpicReadyToClose != "E-0001" {
		t.Fatalf("EpicReadyToClose = %q, want E-0001", out.EpicReadyToClose)
	}
	data, err := os.ReadFile(FeatureDocPath(repoDir, "E-0001", "result.md"))
	if err != nil {
		t.Fatal(err)
	}
	roster := string(data)
	for _, want := range []string{
		"- T-0002: Feature T-0002 — Barra: fixture de teste — referência nomeada — Rodada 1: derrota; Rodada 2: vitória — result: content/features/T-0002/result.md",
		"- T-0003: Feature T-0003 — Barra: não aplicável (regra de gate) — padrão alternativo: testes — result:",
		"- T-0004: Feature T-0004 — Barra: herdada de E-0001 — result:",
	} {
		if !strings.Contains(roster, want) {
			t.Errorf("roster lacks %q:\n%s", want, roster)
		}
	}
}

// TestTaskGauntletRulesQuietInLightMode (crit. 5, F-0364): the task's bar
// rules are result-missing-gauntlet findings, so light mode drops them like
// the rest — at the complete gate and in the project lint — while with the
// Gauntlet on the same backend is charged.
func TestTaskGauntletRulesQuietInLightMode(t *testing.T) {
	for _, mode := range []string{"", "off"} {
		t.Run("gauntlet="+mode, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			saveGauntlet(t, projDir, mode)
			// A parent with no named bar, a task inheriting from it: both charged.
			seedWithSpec(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)
			seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateImplemented}, taskBody+"Barra: herdada de E-0001\n")
			for _, id := range []string{"E-0001", "T-0002"} {
				if err := store.AppendEvent(repoDir, domain.NewEvent(id, "feature.completed", nil, "local")); err != nil {
					t.Fatal(err)
				}
			}
			cfg, _, err := project.LoadConfig(projDir)
			if err != nil {
				t.Fatal(err)
			}
			gateErr := CheckResultRecord(repoDir, reload(t, repoDir, "T-0002"), cfg.Gates)
			findings, err := ProjectLint(repoDir, projDir, cfg)
			if err != nil {
				t.Fatal(err)
			}
			var charged []string
			for _, f := range findings {
				if f.Rule == lint.RuleResultMissingGauntlet {
					charged = append(charged, f.Entity)
				}
			}
			if mode == "off" && (gateErr != nil || len(charged) != 0) {
				t.Fatalf("light mode: gate err = %v, lint charged %v; want both quiet", gateErr, charged)
			}
			if mode == "" && (gateErr == nil || len(charged) != 2) {
				t.Fatalf("Gauntlet on: gate err = %v, lint charged %v; want both charged", gateErr, charged)
			}
		})
	}
}

// TestStartTaskAcceptsInheritedBar (crit. 4): each of the four forms starts a
// task — the inherited one when it names the parent and the parent's bar is
// named.
func TestStartTaskAcceptsInheritedBar(t *testing.T) {
	for _, bar := range []string{
		namedTaskBar,
		exemptTaskBar,
		"Barra: dispensada por decisão humana (\"pode seguir\")\n",
		"Barra: herdada de E-0001\n",
		"Bar: inherited from e-0001\n",
	} {
		t.Run(bar, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedWithSpec(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateInDevelopment}, taskBody+namedTaskBar)
			seedWithSpec(t, repoDir, store.Feature{ID: "T-0001", Parent: "E-0001", State: domain.StateApproved}, taskBody+bar)
			out, err := TransitionFeature(repoDir, projDir, "T-0001", domain.ActionStart, "", "", nil)
			if err != nil {
				t.Fatalf("start err = %v, want the task started", err)
			}
			if out.To != domain.StateInDevelopment {
				t.Fatalf("state = %s, want in_development", out.To)
			}
			// The result scaffold carries the task's bar line, inside a guide
			// comment: still a stub to the complete gate.
			res, err := os.ReadFile(FeatureDocPath(repoDir, "T-0001", "result.md"))
			if err != nil {
				t.Fatal(err)
			}
			line, _, _ := strings.Cut(bar, "\n")
			if !strings.Contains(string(res), line+" -->") {
				t.Errorf("result scaffold lacks the spec's bar line %q:\n%s", line, res)
			}
			if docs, _ := FeatureDocSet(repoDir, "T-0001"); docs.HasResult {
				t.Error("the bar note made the scaffold read as a written result")
			}
		})
	}
}

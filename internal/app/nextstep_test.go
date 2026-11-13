package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

func nextStepRepo(t *testing.T) (proj, repo string) {
	t.Helper()
	proj = t.TempDir()
	repo = filepath.Join(proj, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	return proj, repo
}

func nextStepDoc(t *testing.T, repo, id, name, body string) {
	t.Helper()
	p := FeatureDocPath(repo, id, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// treeDigest is every file under dir with its bytes: a read-only call leaves it equal.
func treeDigest(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		b.WriteString(p + "\x00" + string(raw) + "\x00")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

const (
	nextStepGoodSpec = "# t\n\nProblema real.\n\n## Verificação\n\nBarra: referência nomeada — acesso\nPergunta ao juiz: qual é mais claro?\n"
	nextStepGoodRes  = "# r\n\n## Resultado\n\nok\n\n## Aceitação\n\nSem critérios.\n\n## O que mudou\n\nx\n\n## Débitos e follow-ups\n\nNenhum.\n\n## Aprendizados e desvios\n\nN/A.\n\n## Gauntlet\n\nBarra: não aplicável (teste)\n"
)

// TestNextStep (F-0371 crit. 2-10): one row per state, each answer the result
// of the gate the transition runs. An edit step has no command and names the
// file; the backend is untouched on every row (crit. 12).
func TestNextStep(t *testing.T) {
	tag := func(f *store.Feature) { f.Facets, f.Components = []string{"code"}, []string{"app"} }
	deliver := func(f *store.Feature) { f.DeliveryMRURL = "https://example.com/mr/1" }
	plan := "# plano\n\nPassos reais do plano.\n"
	cases := []struct {
		name    string
		state   domain.FeatureState
		mut     func(*store.Feature)
		docs    map[string]string
		cerne   string // .cernerc
		stale   bool
		pending bool
		actor   string
		stepHas []string
		command string
	}{
		{name: "candidate stub spec", state: domain.StateCandidate, docs: map[string]string{"spec.md": spectmpl.Stub("t")}, actor: ActorAgent, stepHas: []string{"template stub", "edit content/features/F-0001/spec.md", "cerne push"}},
		{name: "candidate no bar", state: domain.StateCandidate, docs: map[string]string{"spec.md": "# t\n\nProblema real.\n"}, actor: ActorAgent, stepHas: []string{"bar-missing", "Barra:", "edit "}},
		{name: "candidate card impediment", state: domain.StateCandidate, docs: map[string]string{"spec.md": nextStepGoodSpec}, actor: ActorAgent, stepHas: []string{"untagged", "cerne feature retag F-0001", "yours to fix"}},
		{name: "candidate ready", state: domain.StateCandidate, mut: tag, docs: map[string]string{"spec.md": nextStepGoodSpec}, actor: ActorHuman, stepHas: []string{"a human runs 'cerne feature approve F-0001'"}},
		{name: "candidate ready with clarification", state: domain.StateCandidate, mut: tag, docs: map[string]string{"spec.md": nextStepGoodSpec + "\nX [ESCLARECER: qual o limite?]\n"}, actor: ActorHuman, stepHas: []string{"qual o limite?"}},
		{name: "candidate ready, approve auto", state: domain.StateCandidate, mut: tag, docs: map[string]string{"spec.md": nextStepGoodSpec}, cerne: "[gates]\napprove = \"auto\"\n", actor: ActorAgent, command: "cerne feature approve F-0001"},
		{name: "approved stale", state: domain.StateApproved, stale: true, docs: map[string]string{"spec.md": "# novo\n"}, actor: ActorHuman, stepHas: []string{"re-approval", "cerne feature approve F-0001"}},
		{name: "approved stub plan", state: domain.StateApproved, docs: map[string]string{"spec.md": nextStepGoodSpec}, actor: ActorAgent, stepHas: []string{"edit content/features/F-0001/plan.md", "cerne push"}},
		{name: "approved real plan", state: domain.StateApproved, docs: map[string]string{"spec.md": nextStepGoodSpec, "plan.md": plan}, actor: ActorAgent, command: "cerne feature start F-0001"},
		{name: "in development no result", state: domain.StateInDevelopment, docs: map[string]string{"spec.md": nextStepGoodSpec, "plan.md": plan}, actor: ActorAgent, stepHas: []string{"complete would refuse", "edit content/features/F-0001/result.md"}},
		{name: "in development result missing evidence", state: domain.StateInDevelopment, docs: map[string]string{"spec.md": "# t\n\n## Objetivo e critérios de aceitação\n\n1. QUANDO a O SISTEMA DEVE b.\n2. QUANDO c O SISTEMA DEVE d.\n\n## Verificação\n\nBarra: ref — acesso\nPergunta ao juiz: qual é mais claro?\n", "plan.md": plan, "result.md": strings.Replace(nextStepGoodRes, "Sem critérios.", "critério 1 — ATENDIDO: teste", 1)}, actor: ActorAgent, stepHas: []string{"critério(s) 2", "edit content/features/F-0001/result.md"}},
		{name: "in development pending uncited", state: domain.StateInDevelopment, pending: true, docs: map[string]string{"spec.md": nextStepGoodSpec, "plan.md": plan, "result.md": nextStepGoodRes}, actor: ActorAgent, stepHas: []string{"P-0001", "edit content/features/F-0001/result.md"}},
		{name: "in development valid result", state: domain.StateInDevelopment, docs: map[string]string{"spec.md": nextStepGoodSpec, "plan.md": plan, "result.md": nextStepGoodRes}, actor: ActorAgent, command: "cerne feature complete F-0001"},
		{name: "implemented no delivery", state: domain.StateImplemented, actor: ActorAgent, stepHas: []string{"cerne feature deliver F-0001 --mr <url>"}, command: "cerne feature deliver F-0001 --mr <url>"},
		{name: "implemented complete", state: domain.StateImplemented, mut: deliver, actor: ActorHuman, stepHas: []string{"a human runs 'cerne feature accept F-0001'"}},
		{name: "implemented complete, accept auto", state: domain.StateImplemented, mut: deliver, cerne: "[gates]\naccept = \"auto\"\n", actor: ActorAgent, command: "cerne feature accept F-0001"},
		{name: "done", state: domain.StateDone, actor: ActorNone, stepHas: []string{"no next step"}},
		{name: "rejected", state: domain.StateRejected, actor: ActorNone, stepHas: []string{"no next step"}},
		{name: "superseded", state: domain.StateSuperseded, actor: ActorNone, stepHas: []string{"no next step"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj, repo := nextStepRepo(t)
			if tc.cerne != "" {
				if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \"b\"\n\n"+tc.cerne), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			f := store.Feature{ID: "F-0001", Title: "t", State: tc.state}
			if tc.mut != nil {
				tc.mut(&f)
			}
			if err := store.SaveFeature(repo, f); err != nil {
				t.Fatal(err)
			}
			for name, body := range tc.docs {
				nextStepDoc(t, repo, "F-0001", name, body)
			}
			if tc.stale {
				if err := os.MkdirAll(filepath.Dir(store.ApprovedSpecPath(repo, "F-0001")), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(store.ApprovedSpecPath(repo, "F-0001"), []byte("# velho\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.pending {
				if err := store.SavePending(repo, store.Pending{ID: "P-0001", Title: "p", State: domain.PendingPromoted, PromotedTo: "F-0001"}); err != nil {
					t.Fatal(err)
				}
			}
			before := treeDigest(t, proj)
			got, err := NextStep(repo, proj, "f-0001")
			if err != nil {
				t.Fatal(err)
			}
			if treeDigest(t, proj) != before {
				t.Error("NextStep wrote to the project")
			}
			if got.State != string(tc.state) || got.Actor != tc.actor || got.Command != tc.command {
				t.Errorf("NextStep = %+v; want actor %q command %q", got, tc.actor, tc.command)
			}
			for _, sub := range tc.stepHas {
				if !strings.Contains(got.Step, sub) {
					t.Errorf("step %q lacks %q", got.Step, sub)
				}
			}
			// Parity: the command the step names runs through the very checks the
			// transition runs, and an edit step is exactly where one of them refuses.
			switch {
			case got.Command == "cerne feature start F-0001":
				if err := CheckStartNotStale(repo, f); err != nil {
					t.Errorf("start suggested but the stale gate refuses: %v", err)
				}
				docs, _ := FeatureDocSet(repo, "F-0001")
				if err := checkActionDocs("F-0001", domain.ActionStart, docs); err != nil {
					t.Errorf("start suggested but the doc gate refuses: %v", err)
				}
			case got.Command == "cerne feature complete F-0001":
				docs, _ := FeatureDocSet(repo, "F-0001")
				if err := checkActionDocs("F-0001", domain.ActionComplete, docs); err != nil {
					t.Errorf("complete suggested but the doc gate refuses: %v", err)
				}
				if err := CheckCompleteResult(repo, f, projectGates(proj)); err != nil {
					t.Errorf("complete suggested but the result gate refuses: %v", err)
				}
			case tc.state == domain.StateInDevelopment:
				if CheckCompleteResult(repo, f, projectGates(proj)) == nil {
					docs, _ := FeatureDocSet(repo, "F-0001")
					if checkActionDocs("F-0001", domain.ActionComplete, docs) == nil {
						t.Error("an edit step was returned but no complete gate refuses")
					}
				}
			}
		})
	}
}

// TestNextStepBadIDs (crit. 11): an id that is not a feature — absent, another
// kind or malformed — is an error naming it.
func TestNextStepBadIDs(t *testing.T) {
	proj, repo := nextStepRepo(t)
	for _, id := range []string{"F-9999", "D-0001", "N-0001", "../x", "nonsense"} {
		if _, err := NextStep(repo, proj, id); err == nil || !strings.Contains(err.Error(), id) {
			t.Errorf("NextStep(%q) err = %v; want an error naming it", id, err)
		}
	}
}

// TestNextStepUnknownState: a state off the table is an error, never a silent default.
func TestNextStepUnknownState(t *testing.T) {
	proj, repo := nextStepRepo(t)
	if err := store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "t", State: "banana"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NextStep(repo, proj, "F-0001"); err == nil || !strings.Contains(err.Error(), "banana") {
		t.Errorf("err = %v", err)
	}
}

// TestNextStepLedgerUnreadable (critic 5): a cost ledger that cannot be read
// shows in the step; it never falls through to the human accept.
func TestNextStepLedgerUnreadable(t *testing.T) {
	proj, repo := nextStepRepo(t)
	if err := store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "t", State: domain.StateImplemented, DeliveryMRURL: "https://example.com/mr/1"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(proj, "costbind.ndjson"), 0o755); err != nil { // a directory where the ledger file goes
		t.Fatal(err)
	}
	got, err := NextStep(repo, proj, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Actor != ActorAgent || got.Command != "" || !strings.Contains(got.Step, "cost check failed") {
		t.Errorf("NextStep = %+v", got)
	}
}

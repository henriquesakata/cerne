package app

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

func backendHead(t *testing.T, repoDir string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return ref.Hash().String()
}

// pendingOrigin creates a feature to hang pendings from.
func pendingOrigin(t *testing.T, repoDir, projDir, title string, facets, components []string) store.Feature {
	t.Helper()
	f, err := CreateFeature(repoDir, projDir, title, "", "", facets, components)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	return f
}

// Crit. 1: an open P- with origin and creation time persisted.
func TestCreatePendingPersistsOpen(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Feature de origem", nil, nil)
	p, err := CreatePending(repoDir, projDir, "  Tratar o caso Y  ", "contexto", f.ID, nil, nil, false)
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	if p.ID != "P-0001" || p.Title != "Tratar o caso Y" || p.State != domain.PendingOpen || p.From != f.ID {
		t.Fatalf("pending = %+v, want P-0001 open from %s", p, f.ID)
	}
	got, err := store.LoadPending(repoDir, p.ID)
	if err != nil {
		t.Fatalf("LoadPending: %v", err)
	}
	if got.From != f.ID || got.Body != "contexto" || got.CreatedAt.IsZero() || got.ClosedAt != nil {
		t.Fatalf("persisted = %+v, want from %s, body, created_at set, closed_at nil", got, f.ID)
	}
	createdPayload(t, repoDir, p.ID, "pending.created")
}

// Crit. 2: no --from, or an unknown origin, refuses naming the field / id and
// writes nothing.
func TestCreatePendingRefusesMissingOrUnknownFrom(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	before := backendHead(t, repoDir)
	cases := []struct {
		name, from string
		wantErr    error
		wantMsg    string
	}{
		{"missing", "  ", ErrPendingFromRequired, "--from"},
		{"unknown", "F-0999", store.ErrNotFound, "F-0999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", tc.from, nil, nil, false)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("message %q does not name %q", err.Error(), tc.wantMsg)
			}
		})
	}
	if ps, _ := store.LoadAllPendings(repoDir); len(ps) != 0 {
		t.Fatalf("refused creations persisted %d pending(s)", len(ps))
	}
	if after := backendHead(t, repoDir); after != before {
		t.Fatalf("backend HEAD moved %s -> %s on refused creations", before, after)
	}
}

// Crit. 3: explicit tags win; absent ones are inherited from the origin per
// axis; an untagged origin yields an untagged pending.
func TestCreatePendingTagsExplicitInheritedOrNone(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	tagged := pendingOrigin(t, repoDir, projDir, "Origem com tags", []string{"code"}, []string{"lint"})
	bare := pendingOrigin(t, repoDir, projDir, "Origem sem tags", nil, nil)
	cases := []struct {
		name, from        string
		facets, comps     []string
		wantFacets, wantC []string
	}{
		{"explicit", tagged.ID, []string{"Data"}, []string{"Store"}, []string{"data"}, []string{"store"}},
		{"inherited", tagged.ID, nil, nil, []string{"code"}, []string{"lint"}},
		{"inherited per axis", tagged.ID, []string{"infra"}, nil, []string{"infra"}, []string{"lint"}},
		{"none", bare.ID, nil, nil, nil, nil},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title := []string{"Primeira coisa", "Segundo assunto", "Terceiro tema", "Quarto ponto"}[i]
			p, err := CreatePending(repoDir, projDir, title, "", tc.from, tc.facets, tc.comps, false)
			if err != nil {
				t.Fatalf("CreatePending: %v", err)
			}
			if !slices.Equal(p.Facets, tc.wantFacets) || !slices.Equal(p.Components, tc.wantC) {
				t.Fatalf("tags = %v / %v, want %v / %v", p.Facets, p.Components, tc.wantFacets, tc.wantC)
			}
		})
	}
}

// Crit. 14: a similar title against an OPEN pending of the SAME origin refuses
// naming the existing id; another origin or a closed pending does not block.
func TestCreatePendingRefusesSimilarOpenSameOrigin(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a := pendingOrigin(t, repoDir, projDir, "Origem A", nil, nil)
	b := pendingOrigin(t, repoDir, projDir, "Origem B", nil, nil)
	const title = "Tratar o caso Y do lint"
	first, err := CreatePending(repoDir, projDir, title, "", a.ID, nil, nil, false)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err = CreatePending(repoDir, projDir, title, "", a.ID, nil, nil, false)
	if !errors.Is(err, ErrSimilarExists) || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("retry err = %v, want ErrSimilarExists naming %s", err, first.ID)
	}
	countFrom := func(from string) int {
		ps, _ := store.LoadAllPendings(repoDir)
		n := 0
		for _, p := range ps {
			if p.From == from {
				n++
			}
		}
		return n
	}
	if n := countFrom(a.ID); n != 1 {
		t.Fatalf("origin %s has %d pendings after the refused retry, want 1", a.ID, n)
	}
	if _, err := CreatePending(repoDir, projDir, title, "", b.ID, nil, nil, false); err != nil {
		t.Fatalf("same title, other origin: %v", err)
	}
	forced, err := CreatePending(repoDir, projDir, title, "", a.ID, nil, nil, true)
	if err != nil {
		t.Fatalf("--force: %v", err)
	}
	if createdPayload(t, repoDir, forced.ID, "pending.created")["guard"] != "forced" {
		t.Fatal("a forced creation must record guard=forced")
	}
}

// Crit. 8: resolve moves an open pending to resolved and stamps closed_at; a
// terminal pending refuses; once closed it no longer blocks a similar title.
func TestResolvePendingClosesIt(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	p, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", f.ID, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePending(repoDir, projDir, p.ID)
	if err != nil {
		t.Fatalf("ResolvePending: %v", err)
	}
	if got.State != domain.PendingResolved || got.ClosedAt == nil {
		t.Fatalf("resolved = %+v, want resolved with closed_at", got)
	}
	if stored, _ := store.LoadPending(repoDir, p.ID); stored.State != domain.PendingResolved || stored.ClosedAt == nil {
		t.Fatalf("persisted = %+v, want resolved with closed_at", stored)
	}
	if _, err := ResolvePending(repoDir, projDir, p.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("resolve twice err = %v, want ErrInvalidTransition", err)
	}
	if _, err := ResolvePending(repoDir, projDir, "P-0999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
	if _, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", f.ID, nil, nil, false); err != nil {
		t.Fatalf("a resolved pending must not block a similar title: %v", err)
	}
}

// Crit. 9: drop needs a reason and persists it.
func TestDropPendingRequiresReason(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	p, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", f.ID, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DropPending(repoDir, projDir, p.ID, "  "); !errors.Is(err, ErrPendingReasonRequired) {
		t.Fatalf("blank reason err = %v, want ErrPendingReasonRequired", err)
	}
	if stored, _ := store.LoadPending(repoDir, p.ID); stored.State != domain.PendingOpen {
		t.Fatalf("a refused drop changed the state to %s", stored.State)
	}
	got, err := DropPending(repoDir, projDir, p.ID, "não vale o custo")
	if err != nil {
		t.Fatalf("DropPending: %v", err)
	}
	stored, _ := store.LoadPending(repoDir, p.ID)
	if got.State != domain.PendingDropped || stored.State != domain.PendingDropped ||
		stored.Reason != "não vale o custo" || stored.ClosedAt == nil {
		t.Fatalf("dropped = %+v, want dropped with reason and closed_at", stored)
	}
}

// Crit. 15 (invariant): an open pending never blocks a lifecycle transition —
// a feature, epic or task with open pendings walks approve, start, complete
// and accept as if it had none.
func TestOpenPendingsNeverBlockTransitions(t *testing.T) {
	for _, id := range []string{"F-0001", "E-0001", "T-0001"} {
		t.Run(id, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, id, domain.StateCandidate, "spec.md", "plan.md", "result.md")
			for _, title := range []string{"Primeiro débito aberto", "Outro assunto pendente"} {
				if _, err := CreatePending(repoDir, projDir, title, "", id, nil, nil, false); err != nil {
					t.Fatalf("CreatePending: %v", err)
				}
			}
			for _, a := range []domain.Action{domain.ActionApprove, domain.ActionStart, domain.ActionComplete, domain.ActionAccept} {
				if _, err := TransitionFeature(repoDir, projDir, id, a, "", "", nil); err != nil {
					t.Fatalf("%s %s with open pendings: %v", a, id, err)
				}
			}
			if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateDone {
				t.Fatalf("%s state = %s, want done", id, f.State)
			}
		})
	}
}

// promotedSpec reads the generated spec of a promoted entity.
func promotedSpec(t *testing.T, repoDir, id string) string {
	t.Helper()
	data, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		t.Fatalf("read spec of %s: %v", id, err)
	}
	return string(data)
}

// Crit. 10: promote --as feature creates a candidate inheriting title, tags and
// context; the pending goes to promoted with promoted_to, and the spec carries
// the back-link line that resolves as an id wiki-link.
func TestPromotePendingToFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Origem", []string{"code"}, []string{"lint"})
	p, err := CreatePending(repoDir, projDir, "Configurar o limiar de envelhecimento", "hoje é constante", f.ID, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got, created, err := PromotePending(repoDir, projDir, p.ID, "feature", false)
	if err != nil {
		t.Fatalf("PromotePending: %v", err)
	}
	if !strings.HasPrefix(created.ID, "F-") || created.State != domain.StateCandidate || created.Title != p.Title ||
		!slices.Equal(created.Facets, []string{"code"}) || !slices.Equal(created.Components, []string{"lint"}) {
		t.Fatalf("created = %+v, want a candidate F- with the pending's title and tags", created)
	}
	if got.State != domain.PendingPromoted || got.PromotedTo != created.ID || got.ClosedAt == nil {
		t.Fatalf("pending = %+v, want promoted to %s", got, created.ID)
	}
	spec := promotedSpec(t, repoDir, created.ID)
	for _, want := range []string{"**Origem:** pendência [[" + p.ID + "]] (de " + f.ID + ")", "hoje é constante"} {
		if !strings.Contains(spec, want) {
			t.Fatalf("spec lacks %q:\n%s", want, spec)
		}
	}
}

// Crit. 10: promote --as task files the task under the origin (born approved),
// linked both ways, and the back-link is not a broken wiki-link.
func TestPromotePendingToTask(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	p, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", "F-0001", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got, task, err := PromotePending(repoDir, projDir, p.ID, "task", false)
	if err != nil {
		t.Fatalf("PromotePending: %v", err)
	}
	if !strings.HasPrefix(task.ID, "T-") || task.Parent != "F-0001" || task.State != domain.StateApproved {
		t.Fatalf("task = %+v, want an approved T- under F-0001", task)
	}
	if stored, _ := store.LoadPending(repoDir, p.ID); stored.State != domain.PendingPromoted || stored.PromotedTo != task.ID || got.PromotedTo != task.ID {
		t.Fatalf("pending = %+v, want promoted_to %s", stored, task.ID)
	}
	if !strings.Contains(promotedSpec(t, repoDir, task.ID), "[["+p.ID+"]]") {
		t.Fatal("the task's spec must link back to the pending")
	}
	findings, err := lint.Run(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fnd := range findings {
		if fnd.Rule == lint.RuleBrokenLink && strings.Contains(fnd.Detail, p.ID) {
			t.Fatalf("the back-link is a broken wiki-link: %+v", fnd)
		}
	}
}

// A closed pending cannot be promoted, an unknown kind refuses, and neither
// creates an entity.
func TestPromotePendingRefusesClosed(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	p, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", f.ID, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := PromotePending(repoDir, projDir, p.ID, "epic", false); !errors.Is(err, ErrPendingPromoteKind) {
		t.Fatalf("--as epic err = %v, want ErrPendingPromoteKind", err)
	}
	if _, err := ResolvePending(repoDir, projDir, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PromotePending(repoDir, projDir, p.ID, "feature", false); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("promote of a resolved pending err = %v, want ErrInvalidTransition", err)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 {
		t.Fatalf("refused promotions created entities: %d features, want only the origin", len(fs))
	}
}

// A debt often reads like its origin, so the F-0147 guard of the created entity
// can refuse the promotion; --force is the same conscious override there, and
// the refused attempt leaves the pending open.
func TestPromotePendingGuardAndForce(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Lint de envelhecimento", nil, nil)
	p, err := CreatePending(repoDir, projDir, "Lint de envelhecimento com limiar configurável", "", f.ID, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := PromotePending(repoDir, projDir, p.ID, "feature", false); !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("err = %v, want ErrSimilarExists", err)
	}
	if stored, _ := store.LoadPending(repoDir, p.ID); stored.State != domain.PendingOpen {
		t.Fatalf("a refused promotion changed the pending to %s", stored.State)
	}
	got, created, err := PromotePending(repoDir, projDir, p.ID, "feature", true)
	if err != nil || got.PromotedTo != created.ID {
		t.Fatalf("forced promote = %+v, %v", got, err)
	}
}

// A pending is usually registered when its feature closes, so the origin is
// often implemented/done: --as task there would file an approved task under a
// closed parent (epic-open-children). It refuses, pointing at --as feature,
// with nothing written.
func TestPromotePendingAsTaskRefusesClosedOrigin(t *testing.T) {
	for _, state := range []domain.FeatureState{domain.StateImplemented, domain.StateDone} {
		t.Run(string(state), func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", state, "spec.md", "plan.md", "result.md")
			p, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", "F-0001", nil, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			before := backendHead(t, repoDir)
			_, _, err = PromotePending(repoDir, projDir, p.ID, "task", false)
			if !errors.Is(err, ErrPendingOriginClosed) || !strings.Contains(err.Error(), "--as feature") {
				t.Fatalf("err = %v, want ErrPendingOriginClosed pointing at --as feature", err)
			}
			if stored, _ := store.LoadPending(repoDir, p.ID); stored.State != domain.PendingOpen {
				t.Fatalf("pending state = %s, want open", stored.State)
			}
			if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 {
				t.Fatalf("%d features, want only the origin", len(fs))
			}
			if after := backendHead(t, repoDir); after != before {
				t.Fatalf("backend HEAD moved %s -> %s", before, after)
			}
		})
	}
}

// Crit. 11/14: the origin is stored canonical — a lower-case --from is the same
// origin for the guard (and for every --from/--feature filter downstream), and
// anything that is not an F-/T-/E-NNNN id is refused before the store is read.
func TestCreatePendingCanonicalOrigin(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	p, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", " f-0001 ", nil, nil, false)
	if err != nil {
		t.Fatalf("lower-case --from: %v", err)
	}
	if p.From != f.ID {
		t.Fatalf("from = %q, want the canonical %q", p.From, f.ID)
	}
	if _, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", f.ID, nil, nil, false); !errors.Is(err, ErrSimilarExists) || !strings.Contains(err.Error(), p.ID) {
		t.Fatalf("retry with the canonical id: err = %v, want ErrSimilarExists naming %s", err, p.ID)
	}
	before := backendHead(t, repoDir)
	for _, bad := range []string{"../decisions/../features/F-0001", "F-1", "D-0001", "F-0001.json"} {
		if _, err := CreatePending(repoDir, projDir, "Outro débito "+bad, "", bad, nil, nil, false); !errors.Is(err, ErrPendingFromInvalid) {
			t.Fatalf("--from %q: err = %v, want ErrPendingFromInvalid", bad, err)
		}
	}
	if ps, _ := store.LoadAllPendings(repoDir); len(ps) != 1 {
		t.Fatalf("%d pendings, want 1", len(ps))
	}
	if after := backendHead(t, repoDir); after != before {
		t.Fatalf("backend HEAD moved on refused --from values")
	}
}

package app

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
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
	got, created, err := PromotePending(repoDir, projDir, p.ID, "feature", "", false)
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
	got, task, err := PromotePending(repoDir, projDir, p.ID, "task", "", false)
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
	if _, _, err := PromotePending(repoDir, projDir, p.ID, "epic", "", false); !errors.Is(err, ErrPendingPromoteKind) {
		t.Fatalf("--as epic err = %v, want ErrPendingPromoteKind", err)
	}
	if _, err := ResolvePending(repoDir, projDir, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PromotePending(repoDir, projDir, p.ID, "feature", "", false); !errors.Is(err, domain.ErrInvalidTransition) {
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
	if _, _, err := PromotePending(repoDir, projDir, p.ID, "feature", "", false); !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("err = %v, want ErrSimilarExists", err)
	}
	if stored, _ := store.LoadPending(repoDir, p.ID); stored.State != domain.PendingOpen {
		t.Fatalf("a refused promotion changed the pending to %s", stored.State)
	}
	got, created, err := PromotePending(repoDir, projDir, p.ID, "feature", "", true)
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
			_, _, err = PromotePending(repoDir, projDir, p.ID, "task", "", false)
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

// The second step of a promotion re-checks the pending inside the lock: closed
// meanwhile, the error names the created entity and says the pending was closed
// concurrently (no "resolve it" advice); a backend failure says the pending may
// still be open and how to record the link once the backend is back.
func TestMarkPromotedReportsHowToRecover(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	f := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	closed, err := CreatePending(repoDir, projDir, "Débito fechado no meio", "", f.ID, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePending(repoDir, projDir, closed.ID); err != nil {
		t.Fatal(err)
	}
	_, err = markPromoted(repoDir, projDir, []string{closed.ID}, "F-0009")
	if !errors.Is(err, ErrPendingClosedConcurrently) {
		t.Fatalf("err = %v, want ErrPendingClosedConcurrently", err)
	}
	for _, want := range []string{"F-0009", closed.ID, "resolved"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "pending resolve") {
		t.Errorf("a concurrently closed pending must not be told to resolve: %q", err.Error())
	}

	open, err := CreatePending(repoDir, projDir, "Débito com backend fora", "", f.ID, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	_, err = markPromoted(repoDir, projDir, []string{open.ID}, "F-0009")
	if err == nil || errors.Is(err, ErrPendingClosedConcurrently) {
		t.Fatalf("backend failure err = %v, want a plain failure", err)
	}
	for _, want := range []string{"F-0009", "may still be open", "cerne pending show " + open.ID, "cerne pending link " + open.ID + " F-0009"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
}

// In a project with 2+ modules the created entity needs one: inherited from
// the origin by default, and module overrides it — the way out when the origin
// has none.
func TestPromotePendingModule(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t, "core", "web")
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	p, err := CreatePending(repoDir, projDir, "Tratar o caso Y", "", "F-0001", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := PromotePending(repoDir, projDir, p.ID, "feature", "", false); err == nil || !strings.Contains(err.Error(), "--module is required") {
		t.Fatalf("origin without module: err = %v, want the module requirement", err)
	}
	_, created, err := PromotePending(repoDir, projDir, p.ID, "feature", "web", false)
	if err != nil || created.Module != "web" {
		t.Fatalf("promote --module web = %+v, %v; want module web", created, err)
	}
}

// F-0334 crit. 7: one selection for the CLI and the ui — open by default, all
// skips only the state, the origin is canonized, the component slugged, the
// module read from the origin; ascending id order and never a nil slice.
func TestListPendings(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t, "core", "web")
	f1, err := CreateFeature(repoDir, projDir, "Origem core", "core", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := CreateFeature(repoDir, projDir, "Origem web", "web", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		title, from string
		components  []string
	}{
		{"Cobrir links quebrados", f1.ID, []string{"Lint"}},
		{"Documentar rota nova", f2.ID, nil},
		{"Ajustar cache do browser", f2.ID, nil},
		{"Remover flag antiga", f1.ID, nil},
	} {
		if _, err := CreatePending(repoDir, projDir, c.title, "", c.from, nil, c.components, false); err != nil {
			t.Fatalf("CreatePending %q: %v", c.title, err)
		}
	}
	if _, err := ResolvePending(repoDir, projDir, "P-0003"); err != nil {
		t.Fatal(err)
	}
	if _, err := DropPending(repoDir, projDir, "P-0004", "fora de escopo"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		f    PendingFilter
		want []string
	}{
		{PendingFilter{}, []string{"P-0001", "P-0002"}},
		{PendingFilter{State: "open"}, []string{"P-0001", "P-0002"}},
		{PendingFilter{State: "resolved"}, []string{"P-0003"}},
		{PendingFilter{State: "dropped"}, []string{"P-0004"}},
		{PendingFilter{State: "all"}, []string{"P-0001", "P-0002", "P-0003", "P-0004"}},
		{PendingFilter{From: " " + strings.ToLower(f2.ID) + " "}, []string{"P-0002"}},
		{PendingFilter{Component: "Lint"}, []string{"P-0001"}},
		{PendingFilter{Module: "web"}, []string{"P-0002"}},
		{PendingFilter{State: "all", From: f1.ID}, []string{"P-0001", "P-0004"}},
		{PendingFilter{From: "F-0099"}, []string{}},
	} {
		got, err := ListPendings(repoDir, tc.f)
		if err != nil {
			t.Fatalf("ListPendings(%+v): %v", tc.f, err)
		}
		if got == nil {
			t.Fatalf("ListPendings(%+v) = nil, want a non-nil slice", tc.f)
		}
		ids := []string{}
		for _, p := range got {
			ids = append(ids, p.ID)
		}
		if !slices.Equal(ids, tc.want) {
			t.Fatalf("ListPendings(%+v) = %v, want %v", tc.f, ids, tc.want)
		}
	}
	_, err = ListPendings(repoDir, PendingFilter{State: "bogus"})
	if !errors.Is(err, ErrPendingStateInvalid) || !strings.Contains(err.Error(), `"bogus"`) || !strings.Contains(err.Error(), "all") {
		t.Fatalf("bogus state err = %v, want ErrPendingStateInvalid naming \"bogus\" and all", err)
	}
}

// mustPending registers an open pending or fails the test.
func mustPending(t *testing.T, repoDir, projDir, title, body, from string) store.Pending {
	t.Helper()
	p, err := CreatePending(repoDir, projDir, title, body, from, nil, nil, false)
	if err != nil {
		t.Fatalf("CreatePending(%q): %v", title, err)
	}
	return p
}

func featureCount(t *testing.T, repoDir string) int {
	t.Helper()
	fs, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	return len(fs)
}

// F-0885 crit. 1: open pendings of two origins become ONE candidate whose spec
// names each pending with its origin and carries every body; each pending is
// promoted to it and the tags are the union of theirs. Two or more ids without
// a title refuse with nothing created; a single id inherits its title, or takes
// the one given.
func TestPromotePendingsGroupsIntoOneFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a := pendingOrigin(t, repoDir, projDir, "Origem A", []string{"code"}, []string{"lint"})
	b := pendingOrigin(t, repoDir, projDir, "Origem B", []string{"data"}, []string{"store"})
	p1 := mustPending(t, repoDir, projDir, "Teto do glab sem WaitDelay", "corpo um", a.ID)
	p2 := mustPending(t, repoDir, projDir, "Manual sem passo em todo passo", "corpo dois", a.ID)
	p3 := mustPending(t, repoDir, projDir, "Cerca til na fila de accepts", "corpo três", b.ID)

	before := backendHead(t, repoDir)
	if _, _, err := PromotePendings(repoDir, projDir, []string{p1.ID, p2.ID}, "feature", "  ", "", false); !errors.Is(err, ErrPendingGroupTitle) {
		t.Fatalf("2 ids without --title: err = %v, want ErrPendingGroupTitle", err)
	}
	if after := backendHead(t, repoDir); after != before || featureCount(t, repoDir) != 2 {
		t.Fatalf("refused group wrote: HEAD %s -> %s, %d features", before, after, featureCount(t, repoDir))
	}

	got, created, err := PromotePendings(repoDir, projDir, []string{p1.ID, p2.ID, p3.ID}, "feature", "Pagar os débitos do grupo", "", false)
	if err != nil {
		t.Fatalf("PromotePendings: %v", err)
	}
	if !strings.HasPrefix(created.ID, "F-") || created.State != domain.StateCandidate || created.Title != "Pagar os débitos do grupo" ||
		!slices.Equal(created.Facets, []string{"code", "data"}) || !slices.Equal(created.Components, []string{"lint", "store"}) {
		t.Fatalf("created = %+v, want one titled candidate with the union of tags", created)
	}
	if featureCount(t, repoDir) != 3 {
		t.Fatalf("features = %d, want the two origins plus ONE created", featureCount(t, repoDir))
	}
	spec := promotedSpec(t, repoDir, created.ID)
	origem := "**Origem:** pendências [[" + p1.ID + "]] (de " + a.ID + "), [[" + p2.ID + "]] (de " + a.ID + "), [[" + p3.ID + "]] (de " + b.ID + ")"
	for _, want := range []string{origem, "### " + p1.ID + " — " + p1.Title, "corpo um", "### " + p2.ID + " — " + p2.Title, "corpo dois", "### " + p3.ID + " — " + p3.Title, "corpo três"} {
		if !strings.Contains(spec, want) {
			t.Fatalf("spec lacks %q:\n%s", want, spec)
		}
	}
	if len(got) != 3 {
		t.Fatalf("returned %d pendings, want 3", len(got))
	}
	for _, id := range []string{p1.ID, p2.ID, p3.ID} {
		if p, _ := store.LoadPending(repoDir, id); p.State != domain.PendingPromoted || p.PromotedTo != created.ID {
			t.Fatalf("%s = %+v, want promoted to %s", id, p, created.ID)
		}
	}

	single := mustPending(t, repoDir, projDir, "Débito solto de uma origem", "", a.ID)
	_, titled, err := PromotePendings(repoDir, projDir, []string{single.ID}, "feature", "Título dado à mão", "", false)
	if err != nil || titled.Title != "Título dado à mão" {
		t.Fatalf("one id with --title = %+v, %v; want the given title", titled, err)
	}
}

// F-0885 crit. 2: a closed, repeated or unknown id refuses the whole group,
// naming it, before anything is created; so do a task across origins and
// origins of different modules without --module.
func TestPromotePendingsRefusesClosedOrRepeated(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a := pendingOrigin(t, repoDir, projDir, "Origem A", nil, nil)
	b := pendingOrigin(t, repoDir, projDir, "Origem B", nil, nil)
	p1 := mustPending(t, repoDir, projDir, "Primeiro débito aberto", "", a.ID)
	p2 := mustPending(t, repoDir, projDir, "Segundo assunto pendente", "", b.ID)
	p3 := mustPending(t, repoDir, projDir, "Terceiro já pago", "", a.ID)
	if _, err := ResolvePending(repoDir, projDir, p3.ID); err != nil {
		t.Fatal(err)
	}
	before := backendHead(t, repoDir)
	cases := []struct {
		name, as string
		ids      []string
		wantErr  error
		wantMsg  string
	}{
		{"closed", "feature", []string{p1.ID, p3.ID}, domain.ErrInvalidTransition, p3.ID},
		{"repeated", "feature", []string{p1.ID, p2.ID, " p-0001 "}, ErrPendingRepeated, p1.ID},
		{"unknown", "feature", []string{p1.ID, "P-0099"}, store.ErrNotFound, "P-0099"},
		{"not a pending id", "feature", []string{p1.ID, "../x"}, ErrPendingIDInvalid, "../x"},
		{"task across origins", "task", []string{p1.ID, p2.ID}, ErrPendingTaskOrigins, b.ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := PromotePendings(repoDir, projDir, tc.ids, tc.as, "Grupo recusado", "", false)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("message %q does not name %q", err.Error(), tc.wantMsg)
			}
		})
	}
	if after := backendHead(t, repoDir); after != before || featureCount(t, repoDir) != 2 {
		t.Fatalf("refused groups wrote: HEAD %s -> %s, %d features", before, after, featureCount(t, repoDir))
	}
	for _, id := range []string{p1.ID, p2.ID} {
		if p, _ := store.LoadPending(repoDir, id); p.State != domain.PendingOpen {
			t.Fatalf("%s = %s after refusals, want open", id, p.State)
		}
	}

	modRepo, modProj, _ := newFeatureBackend(t, "core", "web")
	core, err := CreateFeature(modRepo, modProj, "Origem do core", "core", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	web, err := CreateFeature(modRepo, modProj, "Origem da web", "web", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	q1 := mustPending(t, modRepo, modProj, "Débito do core", "", core.ID)
	q2 := mustPending(t, modRepo, modProj, "Débito da web", "", web.ID)
	if _, _, err := PromotePendings(modRepo, modProj, []string{q1.ID, q2.ID}, "feature", "Grupo de dois módulos", "", false); !errors.Is(err, ErrPendingModuleDiverges) ||
		!strings.Contains(err.Error(), "core") || !strings.Contains(err.Error(), "web") {
		t.Fatalf("divergent modules: err = %v, want ErrPendingModuleDiverges naming both", err)
	}
	if _, created, err := PromotePendings(modRepo, modProj, []string{q1.ID, q2.ID}, "feature", "Grupo de dois módulos", "web", false); err != nil || created.Module != "web" {
		t.Fatalf("--module web = %+v, %v; want module web", created, err)
	}
}

// lastPayload is the payload of the latest typ event of id.
func lastPayload(t *testing.T, repoDir, id, typ string) map[string]string {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EntityID == id && evs[i].Type == typ {
			return evs[i].Payload
		}
	}
	t.Fatalf("no %s event for %s", typ, id)
	return nil
}

// F-0885 crit. 3: link marks an open pending promoted to a live feature
// (candidate, approved, in_development); an implemented, done, rejected or
// superseded one refuses, naming it and its state, with nothing written.
func TestLinkPendingOnlyToLiveFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	origin := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	closedStates := []domain.FeatureState{domain.StateImplemented, domain.StateDone, domain.StateRejected, domain.StateSuperseded}
	for i, st := range closedStates {
		seedFeatureInState(t, repoDir, fmt.Sprintf("F-%04d", 10+i), st)
	}
	p := mustPending(t, repoDir, projDir, "Débito a ligar", "", origin.ID)
	before := backendHead(t, repoDir)
	for i, st := range closedStates {
		id := fmt.Sprintf("F-%04d", 10+i)
		_, err := LinkPending(repoDir, projDir, p.ID, id)
		if !errors.Is(err, ErrPendingTargetClosed) || !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), string(st)) {
			t.Fatalf("link to %s (%s): err = %v, want ErrPendingTargetClosed naming both", id, st, err)
		}
	}
	if _, err := LinkPending(repoDir, projDir, p.ID, "F-0999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("link to an unknown feature: err = %v, want ErrNotFound", err)
	}
	if after := backendHead(t, repoDir); after != before {
		t.Fatalf("refused links moved HEAD %s -> %s", before, after)
	}
	if got, _ := store.LoadPending(repoDir, p.ID); got.State != domain.PendingOpen || got.PromotedTo != "" {
		t.Fatalf("after refusals %+v, want open and unlinked", got)
	}

	for i, st := range []domain.FeatureState{domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment} {
		id := fmt.Sprintf("F-%04d", 20+i)
		seedFeatureInState(t, repoDir, id, st)
		q := mustPending(t, repoDir, projDir, fmt.Sprintf("Débito número %d ligado", i), "", origin.ID)
		got, err := LinkPending(repoDir, projDir, strings.ToLower(q.ID), strings.ToLower(id))
		if err != nil {
			t.Fatalf("link to %s (%s): %v", id, st, err)
		}
		if got.State != domain.PendingPromoted || got.PromotedTo != id || got.ClosedAt == nil {
			t.Fatalf("linked = %+v, want promoted to %s", got, id)
		}
		if pl := lastPayload(t, repoDir, q.ID, "pending.promoted"); pl["promoted_to"] != id || pl["via"] != "link" {
			t.Fatalf("pending.promoted payload = %v, want promoted_to %s via link", pl, id)
		}
		paid, err := PendingsPaidBy(repoDir, id)
		if err != nil || len(paid) != 1 || paid[0].ID != q.ID {
			t.Fatalf("PendingsPaidBy(%s) = %+v, %v; want [%s]", id, paid, err, q.ID)
		}
	}
	if _, err := LinkPending(repoDir, projDir, "P-0002", "F-0020"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("link of an already promoted pending: err = %v, want ErrInvalidTransition", err)
	}
}

// F-0885 crit. 4: unlink hands a pending promoted to a live feature back to
// open, clears promoted_to and records pending.reopened with the feature; a
// pending not promoted, or promoted to a closed feature, refuses. A promoted
// pending is not resolved by hand: it resolves on the accept.
func TestUnlinkPendingReopens(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	origin := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateApproved)
	p := mustPending(t, repoDir, projDir, "Débito ligado e desligado", "", origin.ID)
	if _, err := LinkPending(repoDir, projDir, p.ID, "F-0010"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePending(repoDir, projDir, p.ID); !errors.Is(err, ErrPendingLinked) || !strings.Contains(err.Error(), "F-0010") {
		t.Fatalf("resolve of a linked pending: err = %v, want ErrPendingLinked naming F-0010", err)
	}
	got, err := UnlinkPending(repoDir, projDir, p.ID)
	if err != nil {
		t.Fatalf("UnlinkPending: %v", err)
	}
	if got.State != domain.PendingOpen || got.PromotedTo != "" || got.ClosedAt != nil {
		t.Fatalf("unlinked = %+v, want open, no promoted_to, no closed_at", got)
	}
	if stored, _ := store.LoadPending(repoDir, p.ID); stored.State != domain.PendingOpen || stored.PromotedTo != "" {
		t.Fatalf("persisted = %+v, want open and unlinked", stored)
	}
	if pl := lastPayload(t, repoDir, p.ID, "pending.reopened"); pl["feature"] != "F-0010" {
		t.Fatalf("pending.reopened payload = %v, want feature F-0010", pl)
	}
	if _, err := UnlinkPending(repoDir, projDir, p.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("unlink of an open pending: err = %v, want ErrInvalidTransition", err)
	}

	seedFeatureInState(t, repoDir, "F-0011", domain.StateDone)
	old := mustPending(t, repoDir, projDir, "Promovida antes da F-0885", "", origin.ID)
	old.State, old.PromotedTo = domain.PendingPromoted, "F-0011"
	if err := store.SavePending(repoDir, old); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed promoted "+old.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := UnlinkPending(repoDir, projDir, old.ID); err != nil || got.State != domain.PendingOpen || got.PromotedTo != "" {
		t.Fatalf("unlink from a done feature (promoted before F-0885): %+v, %v, want open", got, err)
	}

	seedFeatureInState(t, repoDir, "F-0012", domain.StateImplemented)
	ready := mustPending(t, repoDir, projDir, "Paga por uma feature pronta", "", origin.ID)
	if _, err := LinkPending(repoDir, projDir, ready.ID, "F-0012"); !errors.Is(err, ErrPendingTargetClosed) {
		t.Fatalf("link to an implemented feature: err = %v, want ErrPendingTargetClosed", err)
	}
	ready.State, ready.PromotedTo = domain.PendingPromoted, "F-0012"
	if err := store.SavePending(repoDir, ready); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed promoted "+ready.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := UnlinkPending(repoDir, projDir, ready.ID); !errors.Is(err, ErrPendingLinked) || !strings.Contains(err.Error(), "F-0012") {
		t.Fatalf("unlink from an implemented feature: err = %v, want ErrPendingLinked naming F-0012", err)
	}
}

// F-0885 r1 (critic): a pending promoted before F-0885 to a feature already
// terminal is moved by nothing any more, so it resolves by hand; drop asks for
// the unlink first. While the feature is implemented, its accept decides.
func TestManualCloseOfPendingPromotedToTerminalFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	origin := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	seed := func(title, featureID string, state domain.FeatureState) store.Pending {
		seedFeatureInState(t, repoDir, featureID, state)
		p := mustPending(t, repoDir, projDir, title, "", origin.ID)
		p.State, p.PromotedTo = domain.PendingPromoted, featureID
		if err := store.SavePending(repoDir, p); err != nil {
			t.Fatal(err)
		}
		if _, err := gitback.CommitAndPush(repoDir, "seed promoted "+p.ID); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rejected := seed("Promovida a uma feature rejeitada", "F-0010", domain.StateRejected)
	if got, err := ResolvePending(repoDir, projDir, rejected.ID); err != nil || got.State != domain.PendingResolved {
		t.Fatalf("resolve of a pending promoted to a rejected feature: %+v, %v", got, err)
	}
	done := seed("Promovida a uma feature aceita", "F-0011", domain.StateDone)
	if _, err := DropPending(repoDir, projDir, done.ID, "não vale mais"); err == nil || !strings.Contains(err.Error(), "cerne pending unlink "+done.ID) {
		t.Fatalf("drop of a pending promoted to a done feature: err = %v, want the unlink hint", err)
	}
	ready := seed("Promovida a uma feature pronta", "F-0012", domain.StateImplemented)
	if _, err := ResolvePending(repoDir, projDir, ready.ID); !errors.Is(err, ErrPendingLinked) || strings.Contains(err.Error(), "unlink") {
		t.Fatalf("resolve while the feature is implemented: err = %v, want ErrPendingLinked without an unlink hint", err)
	}
}

// F-0885 r1 (critic): --as task groups pendings of the same origin into one
// task under it.
func TestPromotePendingsTaskFromOneOrigin(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	p1 := mustPending(t, repoDir, projDir, "Tratar o caso Y do parser", "", "F-0001")
	p2 := mustPending(t, repoDir, projDir, "Medir a latência do export", "", "F-0001")
	got, task, err := PromotePendings(repoDir, projDir, []string{p1.ID, p2.ID}, "task", "Pagar Y e a latência", "", false)
	if err != nil {
		t.Fatalf("PromotePendings --as task, one origin: %v", err)
	}
	if !strings.HasPrefix(task.ID, "T-") || task.Parent != "F-0001" || len(got) != 2 {
		t.Fatalf("task = %+v, pendings = %d, want one T- under F-0001 paying both", task, len(got))
	}
	for _, p := range got {
		if p.State != domain.PendingPromoted || p.PromotedTo != task.ID {
			t.Fatalf("%s = %+v, want promoted to %s", p.ID, p, task.ID)
		}
	}
}

package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"gitlab.com/rockt/cerne/internal/domain"
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

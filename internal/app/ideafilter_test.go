package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0419 crit. 2: ListIdeas filters by state (active by default, all, each
// state), component (slugged) and search; an invalid state is
// ErrIdeaStateInvalid and the result is never nil.
func TestListIdeasStateComponentSearch(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateApproved)
	mk := func(title string, comps []string) store.Idea {
		i, err := CreateIdea(repoDir, projDir, title, "", nil, comps, "")
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	a := mk("ativa do kanban", []string{"kanban"})
	b := mk("resolvida do lint", []string{"lint"})
	c := mk("descartada do kanban", []string{"kanban"})
	d := mk("promovida", nil)
	if _, err := ResolveIdea(repoDir, projDir, b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := DropIdea(repoDir, projDir, c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkIdea(repoDir, projDir, d.ID, "F-0010"); err != nil {
		t.Fatal(err)
	}
	ids := func(f IdeaFilter) []string {
		t.Helper()
		got, err := ListIdeas(repoDir, f)
		if err != nil {
			t.Fatalf("ListIdeas(%+v): %v", f, err)
		}
		var out []string
		for _, i := range got {
			out = append(out, i.ID)
		}
		return out
	}
	for _, tc := range []struct {
		name string
		f    IdeaFilter
		want []string
	}{
		{"default is active", IdeaFilter{}, []string{a.ID}},
		{"explicit active", IdeaFilter{State: "active"}, []string{a.ID}},
		{"resolved", IdeaFilter{State: "resolved"}, []string{b.ID}},
		{"dropped", IdeaFilter{State: "dropped"}, []string{c.ID}},
		{"promoted", IdeaFilter{State: "promoted"}, []string{d.ID}},
		{"all, newest first", IdeaFilter{State: "all"}, []string{d.ID, c.ID, b.ID, a.ID}},
		{"component", IdeaFilter{State: "all", Component: "Kanban"}, []string{c.ID, a.ID}},
		{"component and state", IdeaFilter{State: "dropped", Component: "kanban"}, []string{c.ID}},
		{"component without hit", IdeaFilter{State: "all", Component: "nope"}, nil},
		{"search within state", IdeaFilter{State: "all", Search: "KANBAN descartada"}, []string{c.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(tc.f); !slices.Equal(got, tc.want) {
				t.Fatalf("ids = %v, want %v", got, tc.want)
			}
		})
	}
	if got, err := ListIdeas(repoDir, IdeaFilter{State: "all", Component: "nope"}); err != nil || got == nil {
		t.Fatalf("empty selection = %#v, %v; want a non-nil empty slice", got, err)
	}
	if _, err := ListIdeas(repoDir, IdeaFilter{State: "open"}); !errors.Is(err, ErrIdeaStateInvalid) {
		t.Fatalf("invalid state err = %v, want ErrIdeaStateInvalid", err)
	}
	if !ValidIdeaState("resolved") || ValidIdeaState("") || ValidIdeaState("open") {
		t.Fatal("ValidIdeaState: want resolved valid, empty and open not")
	}
}

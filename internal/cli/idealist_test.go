package cli

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0419 crit. 2: idea list takes --state (active by default, all, each state)
// and --component next to --search; --all is --state all; the JSON is exactly
// app.ListIdeas for the same filter; an invalid state is refused.
func TestIdeaListStateComponentParity(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "ativa do kanban", "--component", "kanban")
	ideaCmd(t, projDir, "new", "descartada do lint", "--component", "lint")
	ideaCmd(t, projDir, "new", "descartada do kanban", "--component", "kanban")
	ideaCmd(t, projDir, "drop", "I-0002")
	ideaCmd(t, projDir, "drop", "I-0003", "--reason", "duplicada")

	listed := func(args ...string) []string {
		t.Helper()
		var doc struct {
			Ideas []store.Idea `json:"ideas"`
		}
		out := ideaCmd(t, projDir, append([]string{"list", "--json"}, args...)...)
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("list --json %v: %v\n%s", args, err, out)
		}
		var ids []string
		for _, i := range doc.Ideas {
			ids = append(ids, i.ID)
		}
		return ids
	}
	for _, tc := range []struct {
		args []string
		f    app.IdeaFilter
	}{
		{nil, app.IdeaFilter{}},
		{[]string{"--state", "dropped"}, app.IdeaFilter{State: "dropped"}},
		{[]string{"--state", "all"}, app.IdeaFilter{State: "all"}},
		{[]string{"--all"}, app.IdeaFilter{State: "all"}},
		{[]string{"--state", "all", "--component", "kanban"}, app.IdeaFilter{State: "all", Component: "kanban"}},
		{[]string{"--state", "dropped", "--component", "kanban", "--search", "DESCARTADA"}, app.IdeaFilter{State: "dropped", Component: "kanban", Search: "DESCARTADA"}},
	} {
		want, err := app.ListIdeas(repoDir, tc.f)
		if err != nil {
			t.Fatal(err)
		}
		var wantIDs []string
		for _, i := range want {
			wantIDs = append(wantIDs, i.ID)
		}
		if got := listed(tc.args...); !slices.Equal(got, wantIDs) {
			t.Fatalf("list %v = %v, want %v (app.ListIdeas %+v)", tc.args, got, wantIDs, tc.f)
		}
	}
	if got := listed("--state", "dropped", "--component", "kanban"); !slices.Equal(got, []string{"I-0003"}) {
		t.Fatalf("dropped kanban = %v, want [I-0003]", got)
	}
	if out := ideaCmd(t, projDir, "list", "--state", "all", "--component", "kanban"); !strings.Contains(out, "dropped  descartada do kanban  — duplicada") || !strings.Contains(out, "active  ativa do kanban") {
		t.Fatalf("--state all lines lack each line's own state:\n%s", out)
	}
	if out, err := execCerne(t, "idea", "list", "--state", "open", "--dir", projDir); !errors.Is(err, app.ErrIdeaStateInvalid) {
		t.Fatalf("--state open err = %v, want ErrIdeaStateInvalid\n%s", err, out)
	}
}

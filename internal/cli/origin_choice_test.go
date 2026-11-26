package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/store"
)

// B-0086: the Backlog chat spawns `cerne feature new` with CERNE_ACTOR=agent and
// CERNE_ORIGIN=chat and no flag; its creation must keep working.
func TestCreateFromChatKeepsWorking(t *testing.T) {
	for _, env := range []string{"CERNE_ORIGIN", llm.DenyLifecycleEnv} {
		t.Run(env, func(t *testing.T) { createFromChat(t, env) })
	}
}

// createFromChat is one chat channel: the verb executor stamps CERNE_ORIGIN=chat,
// the claude-cli provider stamps CERNE_DENY_LIFECYCLE=1.
func createFromChat(t *testing.T, env string) {
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv(env, map[bool]string{true: "chat", false: "1"}[env == "CERNE_ORIGIN"])
	if out, err := execCerne(t, "feature", "new", "From chat", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("chat creation refused: %v\n%s", err, out)
	}
	if f, err := store.LoadFeature(repoDir, "F-0001"); err != nil || f.Origin != "agent" {
		t.Fatalf("card = %+v, %v; want origin agent as before", f, err)
	}
}

// B-0086: in an agent shell, feature/bug/epic/task new must say who asked:
// --on-behalf "<quote>" (a human's request) or --origin agent (the agent's own
// idea). Neither, or both, is refused before anything is written; a human in a
// terminal needs neither.
func TestCreateRequiresOriginChoice(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"feature", []string{"feature", "new", "Titulo F", "--context", "ctx"}},
		{"bug", []string{"bug", "new", "Titulo B", "--context", "ctx"}},
		{"epic", []string{"epic", "new", "Titulo E", "--context", "ctx"}},
		{"task", []string{"task", "new", "Titulo T", "--parent", "F-0001", "--context", "ctx"}},
	}
	scenarios := []struct {
		name       string
		actor      string
		extra      []string
		wantErr    error
		wantOrigin string
	}{
		{"agent, no flag", "agent", nil, app.ErrOriginChoiceRequired, ""},
		{"agent, on-behalf", "agent", []string{"--on-behalf", "o dono pediu"}, nil, "human"},
		{"agent, origin agent", "agent", []string{"--origin", "agent"}, nil, "agent"},
		{"agent, both", "agent", []string{"--on-behalf", "o dono pediu", "--origin", "agent"}, app.ErrOriginChoiceBoth, ""},
		{"agent, origin human", "agent", []string{"--origin", "human"}, app.ErrOriginChoiceRequired, ""},
		{"human, no flag", "", nil, nil, "human"},
	}
	for _, c := range commands {
		for _, s := range scenarios {
			t.Run(c.name+"/"+s.name, func(t *testing.T) {
				projDir, _, repoDir := setupProject(t)
				if out, err := execCerne(t, "feature", "new", "PRD", "--context", "ctx", "--dir", projDir); err != nil {
					t.Fatalf("seed feature: %v\n%s", err, out)
				}
				f, _ := store.LoadFeature(repoDir, "F-0001")
				seedApprovedState(t, repoDir, f)
				t.Setenv("CERNE_ACTOR", s.actor)
				args := append(append([]string{}, c.args...), s.extra...)
				args = append(args, "--dir", projDir)
				out, err := execCerne(t, args...)
				cards, _ := store.LoadAllFeatures(repoDir)
				if s.wantErr != nil {
					if !errors.Is(err, s.wantErr) {
						t.Fatalf("err = %v, want %v\n%s", err, s.wantErr, out)
					}
					if len(cards) != 1 {
						t.Fatalf("refused creation wrote a card: %d cards", len(cards))
					}
					if s.name == "agent, no flag" && (!strings.Contains(err.Error(), "--on-behalf") || !strings.Contains(err.Error(), "--origin agent")) {
						t.Fatalf("message must show both forms: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("create: %v\n%s", err, out)
				}
				if len(cards) != 2 {
					t.Fatalf("cards = %d, want 2", len(cards))
				}
				for _, f := range cards {
					if f.ID != "F-0001" && f.Origin != s.wantOrigin {
						t.Fatalf("%s origin = %q, want %q", f.ID, f.Origin, s.wantOrigin)
					}
				}
			})
		}
	}
}

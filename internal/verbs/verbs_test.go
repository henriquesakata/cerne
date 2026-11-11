package verbs

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func run(*cobra.Command, []string) error { return nil }

func TestFromSkipsGroupsAndHidden(t *testing.T) {
	root := &cobra.Command{Use: "cerne"}
	var dir string
	group := &cobra.Command{Use: "reference", Short: "group"}
	group.PersistentFlags().StringVar(&dir, "dir", "", "project dir")
	_ = group.PersistentFlags().MarkHidden("dir")
	leaf := &cobra.Command{Use: "new <title>", Short: "Register", RunE: run}
	leaf.Flags().Bool("force", false, "force")
	leaf.Flags().StringArray("informs", nil, "ids")
	hidden := &cobra.Command{Use: "plumbing", Hidden: true, RunE: run}
	group.AddCommand(leaf)
	root.AddCommand(group, hidden)

	got := From(root)
	if len(got) != 1 {
		t.Fatalf("From = %+v, want only reference new", got)
	}
	v := got[0]
	if v.Path != "reference new" || v.Use != "new <title>" || v.Short != "Register" || v.Lifecycle {
		t.Fatalf("verb = %+v", v)
	}
	want := map[string]string{"force": "bool", "informs": "stringArray"}
	if len(v.Flags) != len(want) {
		t.Fatalf("flags = %v, want %v (no hidden dir, no help)", v.Flags, want)
	}
	for k, typ := range want {
		if v.Flags[k] != typ {
			t.Fatalf("flags[%s] = %q, want %q", k, v.Flags[k], typ)
		}
	}
}

func TestPositionals(t *testing.T) {
	v := Verb{Use: "new <title> [kind]"}
	if got, err := v.Positionals(); err != nil || len(got) != 2 || got[0] != "title" || got[1] != "kind" {
		t.Fatalf("Positionals = %v, %v", got, err)
	}
	for _, use := range []string{"add <ids>...", "say <room> <message...>", "new title"} {
		if _, err := (Verb{Use: use}).Positionals(); err == nil {
			t.Errorf("Positionals(%q) parsed, want an error", use)
		}
	}
}

// TestClassIsExplicitAndFailsClosed (F-0323 C4): every verb carries its chat
// class on its own definition; only "local" flows down from a group, so a new
// leaf under a chat-bearing group is unclassified (and denied), never chat.
func TestClassIsExplicitAndFailsClosed(t *testing.T) {
	root := &cobra.Command{Use: "cerne"}
	feature := MarkChat(&cobra.Command{Use: "feature", RunE: run})
	feature.AddCommand(
		MarkChat(&cobra.Command{Use: "new <title>", RunE: run}),
		MarkLifecycle(&cobra.Command{Use: "approve <id>", RunE: run}),
		&cobra.Command{Use: "reopen <id>", RunE: run},
	)
	ops := MarkLocal(&cobra.Command{Use: "hook"})
	ops.AddCommand(&cobra.Command{Use: "install", RunE: run})
	root.AddCommand(feature, ops, MarkChatTool(&cobra.Command{Use: "brief", RunE: run}))

	want := map[string]string{
		"feature": ClassChat, "feature new": ClassChat, "feature approve": ClassLifecycle,
		"feature reopen": "", "hook install": ClassLocal, "brief": ClassChat,
	}
	got := map[string]string{}
	for _, v := range From(root) {
		got[v.Path] = v.Class
		if v.Lifecycle != (v.Class == ClassLifecycle) {
			t.Errorf("%s: Lifecycle %v disagrees with class %q", v.Path, v.Lifecycle, v.Class)
		}
	}
	for path, class := range want {
		if got[path] != class {
			t.Errorf("%s: class %q, want %q", path, got[path], class)
		}
	}
}

// TestAffectedIDsListsEveryEntity (F-0323 C10): a call that writes several
// entities reports all of them, in event order, first one primary.
func TestAffectedIDsListsEveryEntity(t *testing.T) {
	repo := t.TempDir()
	for _, ev := range []struct{ id, call string }{{"F-0004", "n1"}, {"X-0001", "other"}, {"P-0001", "n1"}, {"F-0004", "n1"}} {
		e := domain.NewEvent(ev.id, "x.changed", map[string]string{"tool_call": ev.call}, "agent")
		if err := store.AppendEvent(repo, e); err != nil {
			t.Fatal(err)
		}
	}
	if got := affectedIDs(repo, "n1"); !slices.Equal(got, []string{"F-0004", "P-0001"}) {
		t.Fatalf("affectedIDs = %v, want [F-0004 P-0001]", got)
	}
}

// TestAffectedIDsSeesIdeaEvents (F-0312 crit. 9): idea.* stays out of the
// funnel log, but the chat channel still names the idea a call captured.
func TestAffectedIDsSeesIdeaEvents(t *testing.T) {
	repo := t.TempDir()
	e := domain.NewEvent("I-0001", "idea.created", map[string]string{"tool_call": "n1"}, "agent")
	if err := store.AppendEvent(repo, e); err != nil {
		t.Fatal(err)
	}
	if got := affectedIDs(repo, "n1"); !slices.Equal(got, []string{"I-0001"}) {
		t.Fatalf("affectedIDs = %v, want [I-0001]", got)
	}
}

package verbs

import (
	"testing"

	"github.com/spf13/cobra"
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

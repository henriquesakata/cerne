package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/verbs"
)

// verbPaths walks the tree independently of verbs.From: every runnable,
// non-hidden command under a non-hidden parent, by its path without "cerne ".
func verbPaths(c *cobra.Command) []string {
	var out []string
	for _, sub := range c.Commands() {
		if sub.Hidden {
			continue
		}
		if sub.Runnable() {
			out = append(out, strings.TrimPrefix(sub.CommandPath(), "cerne "))
		}
		out = append(out, verbPaths(sub)...)
	}
	return out
}

func catalogByPath(root *cobra.Command) map[string]verbs.Verb {
	out := map[string]verbs.Verb{}
	for _, v := range verbs.From(root) {
		out[v.Path] = v
	}
	return out
}

// TestCatalogFollowsCommandTree is criterion 6 of F-0323: the verb catalog the
// chat exposes is the cobra tree itself, so a verb added to the tree shows up
// with no list edited anywhere — the two probes below are added here and
// nowhere else.
func TestCatalogFollowsCommandTree(t *testing.T) {
	root := Root()
	probe := &cobra.Command{Use: "zz-probe <x>", Short: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
	probe.Flags().String("y", "", "y flag")
	gate := &cobra.Command{Use: "zz-gate", Short: "gate", RunE: func(*cobra.Command, []string) error { return nil }}
	verbs.MarkLifecycle(gate)
	root.AddCommand(probe, gate)

	cat := catalogByPath(root)
	p, ok := cat["zz-probe"]
	if !ok {
		t.Fatalf("zz-probe missing from catalog")
	}
	if p.Lifecycle || p.Use != "zz-probe <x>" || p.Flags["y"] != "string" {
		t.Fatalf("zz-probe = %+v, want non-lifecycle, Use %q, flag y string", p, "zz-probe <x>")
	}
	if g, ok := cat["zz-gate"]; !ok || !g.Lifecycle {
		t.Fatalf("zz-gate = %+v (present %v), want Lifecycle", g, ok)
	}

	real := Root()
	got := slices.Sorted(func(yield func(string) bool) {
		for _, v := range verbs.From(real) {
			if !yield(v.Path) {
				return
			}
		}
	})
	want := verbPaths(real)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("catalog paths diverge from the tree:\n got %v\nwant %v", got, want)
	}
	for _, must := range []string{"reference new", "note new", "feature spec"} {
		if !slices.Contains(got, must) {
			t.Errorf("catalog lacks %q", must)
		}
	}
}

// TestLifecycleMarksMatchDomainActions pins the lifecycle marks to the FSM in
// both directions (F-0323 criterion 4): every feature transition (plus accept
// and deliver, which succeed an approval) and every retire/substituir at any
// depth is marked, and nothing else is.
func TestLifecycleMarksMatchDomainActions(t *testing.T) {
	cat := catalogByPath(Root())
	verbOf := map[domain.Action]string{domain.ActionSupersede: "substituir"}
	featureVerbs := map[string]bool{"feature accept": true, "feature deliver": true}
	for _, a := range domain.FeatureActions() {
		v, ok := verbOf[a]
		if !ok {
			v = string(a)
		}
		featureVerbs["feature "+v] = true
	}
	for path := range featureVerbs {
		if v, ok := cat[path]; !ok || !v.Lifecycle {
			t.Errorf("(a) %q: present %v, marked %v — want a marked feature transition", path, ok, v.Lifecycle)
		}
	}
	for path, v := range cat {
		name := path[strings.LastIndex(path, " ")+1:]
		byName := name == "retire" || name == "substituir"
		if byName && !v.Lifecycle {
			t.Errorf("(b) %q is a %s and is not marked lifecycle", path, name)
		}
		if v.Lifecycle && !byName && !featureVerbs[path] {
			t.Errorf("(c) %q is marked lifecycle but is neither a feature transition nor a retire/substituir", path)
		}
	}
}

// TestAgentDeniedCommandsCoverLifecycleMarks keeps the claude-cli harness
// refusals (the one hand-kept list) an exact projection of the lifecycle marks
// (F-0323 criterion 4, "in any mode"): every marked verb has its rule, and no
// rule outlives its verb.
func TestAgentDeniedCommandsCoverLifecycleMarks(t *testing.T) {
	rules := map[string]bool{}
	for _, r := range llm.AgentDeniedCommands {
		rules[r] = true
	}
	marked := map[string]bool{}
	for _, v := range verbs.From(Root()) {
		if !v.Lifecycle {
			continue
		}
		rule := "Bash(cerne " + v.Path + ":*)"
		marked[rule] = true
		if !rules[rule] {
			t.Errorf("llm.AgentDeniedCommands lacks %q for lifecycle verb %q", rule, v.Path)
		}
	}
	for r := range rules {
		if !marked[r] {
			t.Errorf("llm.AgentDeniedCommands has orphan rule %q (no marked verb)", r)
		}
	}
}

// TestChatToolMarksAreSafe (F-0323): the high-traffic verbs that get a typed
// chat tool are marked on their definitions — no list here. A marked verb is
// never lifecycle, its Use parses into positionals, and no positional collides
// with a flag or with the stdin field of the typed schema.
func TestChatToolMarksAreSafe(t *testing.T) {
	var marked []string
	for _, v := range verbs.From(Root()) {
		if !v.ChatTool {
			continue
		}
		marked = append(marked, v.Path)
		if v.Lifecycle {
			t.Errorf("%q is a typed chat tool and a lifecycle verb", v.Path)
		}
		pos, err := v.Positionals()
		if err != nil {
			t.Errorf("%q: Use %q does not parse: %v", v.Path, v.Use, err)
		}
		for _, p := range pos {
			if _, clash := v.Flags[p]; clash || p == "stdin" {
				t.Errorf("%q: positional %q collides with a flag or stdin", v.Path, p)
			}
		}
	}
	if len(marked) == 0 {
		t.Fatal("no verb is marked as a typed chat tool")
	}
	if t.Failed() {
		t.Logf("typed chat tools: %v", marked)
	}
}

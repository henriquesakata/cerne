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
// chat offers is the cobra tree itself, so a verb added to the tree shows up
// with no list edited anywhere — the probes below are added here and nowhere
// else. Only a verb classified chat on its own definition is offered; one
// left unclassified is reported and never offered (C4, fail closed).
func TestCatalogFollowsCommandTree(t *testing.T) {
	root := Root()
	noop := func(*cobra.Command, []string) error { return nil }
	probe := verbs.MarkChat(&cobra.Command{Use: "zz-probe <x>", Short: "probe", RunE: noop})
	probe.Flags().String("y", "", "y flag")
	gate := verbs.MarkLifecycle(&cobra.Command{Use: "zz-gate", Short: "gate", RunE: noop})
	raw := &cobra.Command{Use: "zz-raw", Short: "unclassified", RunE: noop}
	root.AddCommand(probe, gate, raw)

	cat := catalogByPath(root)
	p, ok := cat["zz-probe"]
	if !ok || p.Class != verbs.ClassChat || p.Use != "zz-probe <x>" || p.Flags["y"] != "string" {
		t.Fatalf("zz-probe = %+v (present %v), want class chat, Use %q, flag y string", p, ok, "zz-probe <x>")
	}
	if g := cat["zz-gate"]; !g.Lifecycle || g.Class != verbs.ClassLifecycle {
		t.Fatalf("zz-gate = %+v, want lifecycle", g)
	}
	if got := unclassified(root); !slices.Equal(got, []string{"zz-raw"}) {
		t.Fatalf("unclassified = %v, want exactly the injected zz-raw", got)
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
		if cat[must].Class != verbs.ClassChat {
			t.Errorf("%q is not offered (class %q)", must, cat[must].Class)
		}
	}
}

func unclassified(root *cobra.Command) []string {
	var out []string
	for _, v := range verbs.From(root) {
		if v.Class == "" {
			out = append(out, v.Path)
		}
	}
	return out
}

// TestEveryVerbIsClassified (F-0323 C4): the chat class is mandatory on every
// user-facing verb — a verb added without one fails here, and at runtime it is
// denied rather than offered.
func TestEveryVerbIsClassified(t *testing.T) {
	if got := unclassified(Root()); len(got) > 0 {
		t.Fatalf("verbs without a chat class (mark them verbs.MarkChat/MarkLocal/MarkLifecycle where they are defined): %v", got)
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

// TestAgentDeniedCommandsCoverLifecycleMarks: the claude-cli harness refusals
// are derived from the command classes (F-0323 C19, criterion 4 "in any
// mode"): every verb the chat channel denies — lifecycle, local, unclassified —
// has its rule, and no chat verb is refused.
func TestAgentDeniedCommandsCoverLifecycleMarks(t *testing.T) {
	rules := map[string]bool{}
	for _, r := range llm.AgentDeniedCommands {
		rules[r] = true
	}
	for _, v := range verbs.From(Root()) {
		rule := "Bash(cerne " + v.Path + ":*)"
		if denied := v.Class != verbs.ClassChat; denied != rules[rule] {
			t.Errorf("%q (class %q): refused by the harness = %v, want %v", v.Path, v.Class, rules[rule], denied)
		}
	}
	if !rules["Bash(cerne feature approve:*)"] || !rules["Bash(cerne import:*)"] {
		t.Fatalf("derived rules miss the lifecycle or local verbs: %v", llm.AgentDeniedCommands)
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

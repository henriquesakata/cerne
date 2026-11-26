package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// verbPaths walks the tree independently of verbs.From: every runnable,
// non-hidden command under a non-hidden parent, by its path without "cerne ".
func verbPaths(c *cobra.Command) []string {
	var out []string
	for _, sub := range c.Commands() {
		if sub.Hidden {
			continue
		}
		if verbs.IsVerb(sub) {
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
// and deliver, which succeed an approval) and every retire/supersede at any
// depth is marked, and nothing else is.
func TestLifecycleMarksMatchDomainActions(t *testing.T) {
	cat := catalogByPath(Root())
	verbOf := map[domain.Action]string{domain.ActionSupersede: "supersede"}
	featureVerbs := map[string]bool{"feature accept": true, "feature deliver": true}
	// import mints entities straight into any state (implemented, superseded),
	// past every human gate — lifecycle in every mode (F-0323 crit. 4).
	gateBypass := map[string]bool{"import": true}
	// feature feedback is the owner's verdict on a risk accept (F-0518): a
	// human act an agent must not reach through chat, so it is lifecycle.
	ownerAct := map[string]bool{"feature feedback": true}
	for _, a := range domain.FeatureActions() {
		v, ok := verbOf[a]
		if !ok {
			v = string(a)
		}
		featureVerbs["feature "+v] = true
	}
	for path := range ownerAct {
		if v, ok := cat[path]; !ok || !v.Lifecycle {
			t.Errorf("(e) %q: present %v, marked %v — want a marked owner act", path, ok, v.Lifecycle)
		}
	}
	for path := range featureVerbs {
		if v, ok := cat[path]; !ok || !v.Lifecycle {
			t.Errorf("(a) %q: present %v, marked %v — want a marked feature transition", path, ok, v.Lifecycle)
		}
	}
	for path, v := range cat {
		name := path[strings.LastIndex(path, " ")+1:]
		byName := name == "retire" || name == "supersede"
		if byName && !v.Lifecycle {
			t.Errorf("(b) %q is a %s and is not marked lifecycle", path, name)
		}
		if gateBypass[path] && !v.Lifecycle {
			t.Errorf("(d) %q bypasses the gates and is not marked lifecycle", path)
		}
		if v.Lifecycle && !byName && !featureVerbs[path] && !gateBypass[path] && !ownerAct[path] {
			t.Errorf("(c) %q is marked lifecycle but is neither a feature transition, a retire/supersede nor a gate bypass", path)
		}
	}
}

// TestAgentDeniedCommandsCoverLifecycleMarks: the claude-cli harness refusals
// are exactly the lifecycle-marked verbs (F-0323 crit. 4, "in any mode"),
// derived, never hand-kept — and nothing more: local verbs stay allowed in the
// subscription mode, whose write policy (F-0260/F-0267/F-0269) is unchanged.
// Hidden verbs count (B-0039): the pt-BR twin `substituir` runs like
// `supersede`, so a list built from the visible catalog let it through. The
// rules match by name, so a cobra alias on a lifecycle path, or cobra's prefix
// or case-insensitive matching, would escape them the same way; none may exist.
// A hidden twin is tied to its visible sibling by their shared Short, so a twin
// built without the lifecycle mark fails here instead of leaving both lists.
func TestAgentDeniedCommandsCoverLifecycleMarks(t *testing.T) {
	if cobra.EnablePrefixMatching || cobra.EnableCaseInsensitive {
		t.Error("cobra prefix/case-insensitive matching runs names the refusals never list")
	}
	var want []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden && verbs.IsVerb(sub) {
				for _, twin := range c.Commands() {
					if !twin.Hidden && twin.Short == sub.Short && verbs.IsLifecycle(twin) != verbs.IsLifecycle(sub) {
						t.Errorf("hidden %q and its visible twin %q differ in the lifecycle mark", sub.CommandPath(), twin.CommandPath())
					}
				}
			}
			if verbs.IsLifecycle(sub) {
				want = append(want, "Bash("+sub.CommandPath()+":*)")
				for p := sub; p != nil; p = p.Parent() {
					if len(p.Aliases) > 0 {
						t.Errorf("%q has cobra aliases %v on a lifecycle path: the name-based refusals miss them", p.CommandPath(), p.Aliases)
					}
				}
			}
			walk(sub)
		}
	}
	walk(Root())
	for _, g := range []string{"feature", "decision", "note", "runbook"} {
		if rule := "Bash(cerne " + g + " substituir:*)"; !slices.Contains(want, rule) {
			t.Errorf("walk lacks the hidden twin %s", rule)
		}
	}
	got := slices.Clone(llm.AgentDeniedCommands)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("claude-cli refusals = %v\nwant the lifecycle-marked set %v", got, want)
	}
}

// TestImportIsRefusedInBothModes (F-0323 crit. 4): import --yes mints
// implemented features past every gate, so it is lifecycle — refused by the
// tool channel and by the claude-cli harness alike.
func TestImportIsRefusedInBothModes(t *testing.T) {
	res := verbs.Run(context.Background(), Root(), verbs.Call{Verb: "import", Args: []string{"--yes"}},
		verbs.Env{Bin: "/nonexistent", Confirmed: true})
	if !errors.Is(res.Err, verbs.ErrLifecycleDenied) {
		t.Fatalf("tool channel: import --yes = %+v, want lifecycle_denied", res)
	}
	if !slices.Contains(llm.AgentDeniedCommands, "Bash(cerne import:*)") {
		t.Fatalf("claude-cli harness does not refuse import: %v", llm.AgentDeniedCommands)
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

// TestLifecycleRefusedUnderAgentHarnessStamp (B-0040): the harness rules match
// by prefix, so `cerne --verbose feature supersede` or `cerne feature --dir .
// supersede` reach the verb without matching any of them. The binary itself
// refuses a lifecycle verb when the agentic runner's stamp is in the
// environment, whatever the spelling: the check sits on the resolved command.
//
// Safety: the commands below really run when the check regresses. They start
// in a temp dir with no .cernerc above it and CERNE_ROOT unset, so project
// discovery finds nothing and a regression ends at "no cerne project", never
// at a write.
func TestLifecycleRefusedUnderAgentHarnessStamp(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_ROOT", "")
	execute := func(args ...string) error {
		root := Root()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		return root.Execute()
	}
	tail := []string{"F-9998", "--by", "F-9999", "--reason", "x"}
	for _, stamp := range []string{"1", ""} { // "": an empty assignment still refuses
		t.Setenv(llm.DenyLifecycleEnv, stamp)
		for _, spelling := range [][]string{
			{"feature", "supersede"},
			{"--verbose", "feature", "supersede"},
			{"feature", "--dir", ".", "supersede"},
			{"feature", "substituir"},
			{"decision", "--dir", ".", "supersede"},
		} {
			if err := execute(append(slices.Clone(spelling), tail...)...); !errors.Is(err, verbs.ErrLifecycleDenied) {
				t.Errorf("stamp %q: cerne %s … = %v, want ErrLifecycleDenied", stamp, strings.Join(spelling, " "), err)
			}
		}
		// The agent's own writes stay open: a candidate is revisable (F-0269).
		if err := execute("--verbose", "feature", "new", "x", "--context", "y"); errors.Is(err, verbs.ErrLifecycleDenied) {
			t.Errorf("stamp %q: feature new refused: %v", stamp, err)
		}
	}
	root := Root()
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.PersistentPreRun != nil || sub.PersistentPreRunE != nil {
				t.Errorf("%s defines its own persistent pre-run: cobra then skips the root's, and with it the lifecycle refusal", sub.CommandPath())
			}
			if err := refuseAgentLifecycle(sub); verbs.IsLifecycle(sub) != errors.Is(err, verbs.ErrLifecycleDenied) {
				t.Errorf("%s: refusal = %v, lifecycle = %v", sub.CommandPath(), err, verbs.IsLifecycle(sub))
			}
			walk(sub)
		}
	}
	walk(root)
	// Without the stamp nothing changes: a coding agent in a terminal is
	// CERNE_ACTOR=agent too, and it runs start/complete/supersede.
	os.Unsetenv(llm.DenyLifecycleEnv)
	t.Setenv("CERNE_ACTOR", "agent")
	sup, _, err := root.Find([]string{"feature", "supersede"})
	if err != nil || !verbs.IsLifecycle(sup) {
		t.Fatalf("feature supersede = %v, %v", sup, err)
	}
	if err := refuseAgentLifecycle(sup); err != nil {
		t.Errorf("unstamped agent refused: %v", err)
	}
}

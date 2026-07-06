package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestTransitionHelpShowsGatePrecondition pins crit. 4: each lifecycle
// transition's --help names its gate precondition and the full sequence.
func TestTransitionHelpShowsGatePrecondition(t *testing.T) {
	tests := []struct{ cmd, gate string }{
		{"approve", "spec"},
		{"start", "plan"},
		{"complete", "result"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			out, err := execCerne(t, "feature", tt.cmd, "--help")
			if err != nil {
				t.Fatalf("feature %s --help: %v\n%s", tt.cmd, err, out)
			}
			if !strings.Contains(out, tt.gate) {
				t.Errorf("feature %s --help missing gate precondition %q:\n%s", tt.cmd, tt.gate, out)
			}
			for _, step := range []string{"new", "approve", "start", "complete"} {
				if !strings.Contains(out, step) {
					t.Errorf("feature %s --help missing lifecycle step %q:\n%s", tt.cmd, step, out)
				}
			}
		})
	}
}

var exampleFlagRx = regexp.MustCompile(`--[a-zA-Z][\w-]*`)

// TestExamplesReferenceOnlyRealFlags pins crit. 5: no Example may cite a flag the
// command does not have, and any pending-dir path it cites must equal the shared
// const (so the help can't drift from the code path).
func TestExamplesReferenceOnlyRealFlags(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Example != "" {
			for _, m := range exampleFlagRx.FindAllString(c.Example, -1) {
				name := strings.TrimPrefix(m, "--")
				if c.Flags().Lookup(name) == nil && c.InheritedFlags().Lookup(name) == nil && c.PersistentFlags().Lookup(name) == nil {
					t.Errorf("%s Example cites unknown flag %s", c.CommandPath(), m)
				}
			}
			if strings.Contains(c.Example, ".docs/wiki/.pending") && !strings.Contains(c.Example, pendingRelDir) {
				t.Errorf("%s Example cites a pending path that is not pendingRelDir (%q)", c.CommandPath(), pendingRelDir)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(Root())
}

// TestShortHelpStaysShort pins crit. 5: the one-line Short never grows the
// example body — examples live only in Example/--help.
func TestShortHelpStaysShort(t *testing.T) {
	root := Root()
	for _, path := range [][]string{{"compile"}, {"feature", "approve"}, {"feature", "start"}} {
		cmd := findCmd(t, root, path)
		if strings.Contains(cmd.Short, "\n") {
			t.Errorf("%s Short is not single-line: %q", strings.Join(path, " "), cmd.Short)
		}
		for _, marker := range []string{"--ingest", "response_path", "ciclo de vida"} {
			if strings.Contains(cmd.Short, marker) {
				t.Errorf("%s Short leaked example content %q: %q", strings.Join(path, " "), marker, cmd.Short)
			}
		}
	}
}

func findCmd(t *testing.T, root *cobra.Command, path []string) *cobra.Command {
	t.Helper()
	cur := root
	for _, name := range path {
		found := false
		for _, sub := range cur.Commands() {
			if sub.Name() == name {
				cur, found = sub, true
				break
			}
		}
		if !found {
			t.Fatalf("command %v not found (stuck at %q)", path, name)
		}
	}
	return cur
}

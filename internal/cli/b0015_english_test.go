package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// B-0015: the verbs and flags a user reads are English; the pt-BR names stay
// accepted as hidden aliases for hooks and scripts already installed.

// legacyPtNames are the pt-BR command and flag names that were public.
var legacyPtNames = []string{"substituir", "altera", "sem-passo", "consolidado"}

// TestPublicSurfaceHasNoPortugueseNames walks the whole command tree: no
// visible command or flag may carry a legacy pt-BR name or a diacritic.
func TestPublicSurfaceHasNoPortugueseNames(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		check := func(kind, name string) {
			if strings.ContainsAny(name, "áàâãéêíóôõúç") {
				t.Errorf("%s %q of %q has a diacritic", kind, name, c.CommandPath())
			}
			for _, pt := range legacyPtNames {
				if name == pt {
					t.Errorf("%s %q of %q is a pt-BR name", kind, name, c.CommandPath())
				}
			}
		}
		check("command", c.Name())
		for _, a := range c.Aliases {
			check("alias", a)
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden {
				check("flag", f.Name)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(Root())
}

func TestSupersedeVerbIsEnglishAndLegacyStillRuns(t *testing.T) {
	for _, group := range []string{"decision", "note", "runbook", "feature"} {
		help, err := execCerne(t, group, "--help")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(help, "supersede") || strings.Contains(help, "substituir") {
			t.Errorf("%s --help must list supersede and not substituir:\n%s", group, help)
		}
		// Both spellings reach the same command: same refusal for a missing --by/--reason.
		newOut, newErr := execCerne(t, group, "supersede", "X-0001")
		oldOut, oldErr := execCerne(t, group, "substituir", "X-0001")
		if newErr == nil || oldErr == nil || newErr.Error() != oldErr.Error() || newOut != oldOut {
			t.Errorf("%s: supersede (%v) and substituir (%v) must behave the same", group, newErr, oldErr)
		}
		if newErr != nil && strings.Contains(newErr.Error(), "unknown command") {
			t.Errorf("%s supersede is not a command: %v", group, newErr)
		}
	}
	// a real transition with the English name.
	projDir, _, _ := setupProject(t)
	id := newDecision(t, projDir, "ADR A", 1)
	id2 := newDecision(t, projDir, "ADR B", 2)
	out, err := execCerne(t, "decision", "supersede", id, "--by", id2, "--reason", "new course", "--dir", projDir)
	if err != nil || !strings.Contains(out, "superseded") {
		t.Fatalf("decision supersede: %v\n%s", err, out)
	}
}

func TestEnglishFlagsAndLegacyAliases(t *testing.T) {
	help, _ := execCerne(t, "runbook", "errata", "--help")
	for _, want := range []string{"--step", "--no-step"} {
		if !strings.Contains(help, want) {
			t.Errorf("errata --help must list %s:\n%s", want, help)
		}
	}
	for _, bad := range []string{"--altera", "--sem-passo", "--consolidado"} {
		if strings.Contains(help, bad) {
			t.Errorf("errata --help lists %s:\n%s", bad, help)
		}
	}
	showHelp, _ := execCerne(t, "show", "--help")
	if !strings.Contains(showHelp, "--consolidated") || strings.Contains(showHelp, "--consolidado") {
		t.Errorf("show --help must list --consolidated only:\n%s", showHelp)
	}

	projDir, _ := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Step one.", "--step", "S6.6")
	rb(t, projDir, "runbook", "errata", "RB-0001", "General.", "--no-step")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Legacy step.", "--altera", "S6.7")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Legacy general.", "--sem-passo")
	newView := rb(t, projDir, "show", "RB-0001", "--consolidated")
	oldView := rb(t, projDir, "show", "RB-0001", "--consolidado")
	if newView != oldView || newView == "" {
		t.Fatalf("--consolidated and --consolidado must print the same view:\n%s\n---\n%s", newView, oldView)
	}
}

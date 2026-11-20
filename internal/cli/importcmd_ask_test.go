package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// execCerneIn runs the CLI with stdin set to in and the interactive check
// forced to interactive.
func execCerneIn(t *testing.T, interactive bool, in string, args ...string) string {
	t.Helper()
	prev := importInteractive
	importInteractive = func(io.Reader) bool { return interactive }
	t.Cleanup(func() { importInteractive = prev })
	root := Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetIn(strings.NewReader(in))
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return out.String()
}

const deliveredQuestion = "Features imported as delivered enter as implemented"

func TestImportAsksDeliveredStateOnlyOnATerminal(t *testing.T) {
	treeDir := t.TempDir()
	writeTree(t, treeDir, importFixtureFiles())

	t.Run("terminal answers done", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		out := execCerneIn(t, true, "done\n", "import", "--root", treeDir, "--dir", projDir)
		for _, want := range []string{deliveredQuestion, "done 1", "--delivered-state done"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("terminal empty answer keeps implemented", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		out := execCerneIn(t, true, "\n", "import", "--root", treeDir, "--dir", projDir)
		if !strings.Contains(out, deliveredQuestion) || !strings.Contains(out, "implemented 1") || strings.Contains(out, "--delivered-state") {
			t.Errorf("empty answer should keep implemented:\n%s", out)
		}
	})
	t.Run("not a terminal never asks", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		out := execCerneIn(t, false, "done\n", "import", "--root", treeDir, "--dir", projDir)
		if strings.Contains(out, deliveredQuestion) {
			t.Errorf("asked without a terminal:\n%s", out)
		}
		if !strings.Contains(out, noTerminalLine) {
			t.Errorf("dry-run without a terminal must state the default:\n%s", out)
		}
	})
	t.Run("choice given never asks", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		out := execCerneIn(t, true, "done\n", "import", "--root", treeDir, "--delivered-state", "implemented", "--dir", projDir)
		if strings.Contains(out, deliveredQuestion) {
			t.Errorf("asked with --delivered-state:\n%s", out)
		}
	})
	t.Run("yes never asks", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		out := execCerneIn(t, true, "done\n", "import", "--root", treeDir, "--yes", "--dir", projDir)
		if strings.Contains(out, deliveredQuestion) {
			t.Errorf("asked with --yes:\n%s", out)
		}
	})
	t.Run("agent ingest dry-run asks", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		writeAgentPlan(t, projDir, gabaritoPlan)
		out := execCerneIn(t, true, "done\n", "import", "--agent", "--ingest", "--dir", projDir)
		if !strings.Contains(out, deliveredQuestion) || !strings.Contains(out, "--delivered-state done") {
			t.Errorf("agent dry-run did not ask:\n%s", out)
		}
	})
	t.Run("agent ingest not a terminal never asks", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		writeAgentPlan(t, projDir, gabaritoPlan)
		out := execCerneIn(t, false, "done\n", "import", "--agent", "--ingest", "--dir", projDir)
		if strings.Contains(out, deliveredQuestion) {
			t.Errorf("asked without a terminal:\n%s", out)
		}
		if !strings.Contains(out, "delivered feature(s) will enter as implemented; add --delivered-state done") {
			t.Errorf("agent dry-run without a terminal must state the default:\n%s", out)
		}
	})
}

// T-0013: with no terminal (an agent or a script) the dry-run states the default
// and the flag in one line; with a terminal, a chosen state or --yes it stays quiet.
const noTerminalLine = "1 delivered feature(s) will enter as implemented; add --delivered-state done to enter them as done."

func TestImportDryRunWithoutTerminalIsSilentWhenTheLineDoesNotApply(t *testing.T) {
	treeDir := t.TempDir()
	writeTree(t, treeDir, importFixtureFiles())
	for name, args := range map[string][]string{
		"choice given": {"--delivered-state", "done"},
		"yes":          {"--yes"},
	} {
		t.Run(name, func(t *testing.T) {
			projDir, _, _ := setupProject(t)
			out := execCerneIn(t, false, "", append([]string{"import", "--root", treeDir, "--dir", projDir}, args...)...)
			if strings.Contains(out, "will enter as implemented; add") {
				t.Errorf("line printed:\n%s", out)
			}
		})
	}
}

func TestImportTerminalAnswerDoneIsAnInstruction(t *testing.T) {
	treeDir := t.TempDir()
	writeTree(t, treeDir, importFixtureFiles())
	projDir, _, _ := setupProject(t)
	out := execCerneIn(t, true, "done\n", "import", "--root", treeDir, "--dir", projDir)
	if !strings.Contains(out, "To enter them as done, add --delivered-state done when you apply.") || strings.Contains(out, "will enter as implemented; add") {
		t.Errorf("wrong terminal answer wording:\n%s", out)
	}
}

func TestStdinIsInteractiveRejectsPlainReaders(t *testing.T) {
	if stdinIsInteractive(strings.NewReader("")) {
		t.Fatal("a plain reader is not a terminal")
	}
}

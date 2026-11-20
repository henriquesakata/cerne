package cli

import (
	"strings"
	"testing"
)

// E-0005 criterion 10 (T-0017): a subagent of a session bound to a task —
// here through the work whose ref it is — gets the task, the index of the
// notes that inform it and the package command; an unbound session gets
// nothing new.
func TestHookChannelSubagentGetsTheBoundTask(t *testing.T) {
	boardOff(t)
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	newFeature(t, projDir, "Feature A", 1)
	seedInformingNotes(t, repoDir, "F-0001", 2)
	w := startBoundWork(t, projDir, repoDir, "sess-task", "Implementar a feature A", "--ref", "F-0001")

	out, err := execCerneStdin(t, withAgent(sessionStartJSON(t, "sess-task", projDir), "a0sub"), "hook", "channel", "--point", "subagent")
	if err != nil {
		t.Fatalf("subagent: %v\n%s", err, out)
	}
	for _, want := range []string{
		"THIS SESSION'S TASK: F-0001 — Feature A [candidate] (bound through " + w + ")\n",
		"  [a] N-0001  Rule 1 — gist 1\n", "  [a] N-0002  Rule 2 — gist 2\n",
		"Task package: cerne context F-0001\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("subagent context missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "BODY-MARKER") {
		t.Fatalf("a body reached the subagent:\n%s", out)
	}

	if out, err := execCerneStdin(t, sessionStartJSON(t, "sess-free", projDir), "hook", "channel", "--point", "subagent"); err != nil || out != "" {
		t.Fatalf("unbound session: %v %q, want silence", err, out)
	}
}

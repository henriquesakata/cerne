package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// Under the merge gate the pre-tool shell hook denies an agent's PR merge,
// in the envelope of each format that runs it; a look-alike passes, and with
// the gate off nothing changes.
// F-0488 crit. 10
func TestToolNotesDeniesMergeUnderGate(t *testing.T) {
	projDir, _, _ := setupProject(t)
	setGate := func(accept string) {
		t.Helper()
		cfg, _, err := project.LoadConfig(projDir)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Gates.Accept = accept
		if err := project.SaveConfig(projDir, cfg); err != nil {
			t.Fatal(err)
		}
	}
	setGate(project.GateMerge)

	denied := []string{
		"gh pr merge 12 --squash",
		"cd sub && gh pr merge 12",
		"GH_TOKEN=x gh pr merge 12",
		"gh -R o/r pr merge 1",
		"glab mr merge 7",
		"tea pulls merge 3",
		"gh api -X PUT repos/o/r/pulls/12/merge",
		"gh api repos/o/r/pulls/12/merge --method PUT",
		"glab api --method PUT projects/1/merge_requests/7/merge",
		`bash -c "gh pr merge 1"`,
		"sudo gh pr merge 1",
		"env GH_TOKEN=x gh pr merge 1",
		"command gh pr merge 1",
		"if gh pr merge 1; then echo ok; fi",
		"{ gh pr merge 1; }",
		`gh api graphql -f query='mutation { mergePullRequest(input:{pullRequestId:"x"}) { clientMutationId } }'`,
		`gh api graphql -f query='mutation { enablePullRequestAutoMerge(input:{pullRequestId:"x"}) { clientMutationId } }'`,
	}
	allowed := []string{
		"gh pr view 12",
		"gh pr list --state merged",
		`git commit -m "docs: explain gh pr merge"`,
		"gh api repos/o/r/pulls/12",
		"gh api repos/o/r/pulls/12/merge", // GET: GitHub's read-only "is it merged?" check
		`gh api graphql -f query='{ viewer { login } }'`,
		"echo merge",
	}
	for _, format := range []string{"claude", "codex"} {
		for _, c := range denied {
			out, err := execCerneStdin(t, bashJSON(t, "s-"+format, projDir, c), "hook", "tool-notes", "--format", format)
			var d gateDeny
			if err != nil || json.Unmarshal([]byte(out), &d) != nil || d.HookSpecificOutput.PermissionDecision != "deny" || !strings.Contains(d.HookSpecificOutput.PermissionDecisionReason, "cerne feature accept --merged") {
				t.Errorf("%s %q: err %v out %q, want a deny that names accept --merged", format, c, err, out)
			}
		}
		for _, c := range allowed {
			out, _ := execCerneStdin(t, bashJSON(t, "s-"+format, projDir, c), "hook", "tool-notes", "--format", format)
			if strings.Contains(out, `"deny"`) {
				t.Errorf("%s %q: denied, want it to pass: %s", format, c, out)
			}
		}
	}
	cur := cursorFixture(t, "post-tool-use-terminals-cwd.json", projDir)
	cur["hook_event_name"] = "preToolUse"
	cur["tool_input"] = map[string]any{"command": "gh pr merge 12"}
	out, _ := execCerneStdin(t, payloadString(t, cur), "hook", "tool-notes")
	var cd cursorGateDeny
	if json.Unmarshal([]byte(out), &cd) != nil || cd.Permission != "deny" {
		t.Errorf("cursor: out %q, want Cursor's deny envelope", out)
	}
	// After the tool there is nothing to deny.
	if out, _ := execCerneStdin(t, bashJSON(t, "s-after", projDir, "gh pr merge 12"), "hook", "tool-notes", "--after", "--format", "copilot"); strings.Contains(out, `"deny"`) {
		t.Errorf("after the tool: %s", out)
	}

	setGate("")
	if out, _ := execCerneStdin(t, bashJSON(t, "s-off", projDir, "gh pr merge 12"), "hook", "tool-notes"); strings.Contains(out, `"deny"`) {
		t.Errorf("gate off: denied: %s", out)
	}
}

// TestToolNotesMergeDenyOncePerCursorCall (F-0527 crit. 2): Cursor runs the
// pre-tool shell hook from .cursor/hooks.json and again from the imported
// .claude hooks for the same call; the merge deny comes out of the first only.
func TestToolNotesMergeDenyOncePerCursorCall(t *testing.T) { // F-0527 crit. 2
	projDir, _, _ := setupProject(t)
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Gates.Accept = project.GateMerge
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
	cur := cursorFixture(t, "pre-tool-use-shell.json", projDir)
	cur["tool_input"] = map[string]any{"command": "gh pr merge 12"}
	var denies int
	for range 2 {
		out, _ := execCerneStdin(t, payloadString(t, cur), "hook", "tool-notes")
		if strings.Contains(out, `"deny"`) {
			denies++
		}
	}
	if denies != 1 {
		t.Fatalf("denies across both layers = %d, want 1", denies)
	}
}

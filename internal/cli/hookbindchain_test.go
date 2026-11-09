package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
)

// bindFromBash feeds the bind-work hook one Bash call and returns the W- the
// session ends up bound to ("" when unbound) plus the hook's stdout. The
// response is built from the REAL output of the real `cerne work start` and
// `work list` run in the project (F-0876 round 1), not from a literal: if the
// command's wording changes, these tests follow it. stdout receives the real
// list output and the real "created" output of the start under test.
func bindFromBash(t *testing.T, command string, stdout func(listed, created string) string) (bound, createdID, hookOut string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Antigo trabalho um", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	created, err := execCerne(t, "work", "start", "Trabalho recem criado", "--dir", projDir)
	if err != nil {
		t.Fatalf("work start: %v\n%s", err, created)
	}
	listed, err := execCerne(t, "work", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("work list: %v\n%s", err, listed)
	}
	createdID = regexp.MustCompile(`W-\d+`).FindString(created)
	stdin := hookJSON(t, "sess-chain", projDir, "Bash",
		map[string]any{"command": command}, map[string]any{"stdout": stdout(listed, created)})
	hookOut, err = execCerneStdin(t, stdin, "hook", "bind-work")
	if err != nil {
		t.Fatalf("hook bind-work: %v", err)
	}
	id, ok, err := app.CurrentBinding(filepath.Dir(repoDir), repoDir, "sess-chain")
	if err != nil {
		t.Fatalf("CurrentBinding: %v", err)
	}
	if !ok {
		id = ""
	}
	return id, createdID, hookOut
}

// F-0876 crit. 3, case that reproduces: a chain whose output lists other works
// before the minted one bound the session to the first W- of the response (an
// old one), not to the work just created.
func TestHookBindWorkChainBindsTheCreatedWork(t *testing.T) {
	for name, tc := range map[string]struct {
		command string
		stdout  func(listed, created string) string
	}{
		"list then start": {`cerne work list; cerne work start "Trabalho recem criado"`, func(l, c string) string { return l + c }},
		"pipe to tail":    {`cerne work start "Trabalho recem criado" | tail -1`, func(_, c string) string { return c }},
		"and-chain":       {`cd /x && cerne work start "Trabalho recem criado" --ref F-0001`, func(_, c string) string { return c }},
	} {
		t.Run(name, func(t *testing.T) {
			got, want, _ := bindFromBash(t, tc.command, tc.stdout)
			if want == "" || got != want {
				t.Fatalf("bound to %q, want %q (the created work)", got, want)
			}
		})
	}
}

// F-0876 crit. 3: a real `cerne work start` whose output never reached the hook
// (redirected or filtered away) cannot be bound; the hook says so explicitly
// instead of leaving the Edit gate to deny with no explanation.
func TestHookBindWorkWarnsWhenItCannotBind(t *testing.T) {
	for name, stdout := range map[string]string{
		"output redirected": "",
		"output filtered":   "something unrelated",
	} {
		t.Run(name, func(t *testing.T) {
			got, _, hookOut := bindFromBash(t, `cerne work start "Trabalho recem criado" > /dev/null`,
				func(_, _ string) string { return stdout })
			if got != "" {
				t.Fatalf("nothing to bind from, got %q", got)
			}
			for _, want := range []string{`"hookEventName":"PostToolUse"`, "NOT bound", "Edit gate", "without a pipe"} {
				if !strings.Contains(hookOut, want) {
					t.Fatalf("hook output missing %q:\n%s", want, hookOut)
				}
			}
		})
	}
}

// F-0876 round 1: a Bash call that only MENTIONS "work start" is not a work
// start. The false "NOT bound" alarm trains the agent to ignore the real one.
// Commands are real shell lines (grep, echo, commit message, help, a refused
// start), each carrying the word "cerne" so the hook does not bail out early.
func TestHookBindWorkDoesNotWarnOnMentions(t *testing.T) {
	for name, tc := range map[string]struct{ command, stdout string }{
		"grep":            {`grep "cerne work start" notes.md`, "notes.md: cerne work start deve ser sozinho"},
		"echo":            {`echo "rode cerne work start sozinho"`, "rode cerne work start sozinho"},
		"commit message":  {`git commit -m "docs: cerne work start sem pipe"`, "[main abc1234] docs: cerne work start sem pipe"},
		"heredoc-ish cat": {`cat <<'EOF'\ncerne work start x\nEOF`, "cerne work start x"},
		"help":            {`cerne work start --help`, "Usage: cerne work start <title>"},
		"refused start":   {`cerne work start "Trabalho recem criado"`, "Error: similar active work W-0001 (use --force)"},
	} {
		t.Run(name, func(t *testing.T) {
			got, _, hookOut := bindFromBash(t, tc.command, func(_, _ string) string { return tc.stdout })
			if got != "" || strings.Contains(hookOut, "NOT bound") {
				t.Fatalf("false alarm: bound=%q hook output:\n%s", got, hookOut)
			}
		})
	}
}

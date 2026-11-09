package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
)

// bindFromBash feeds the bind-work hook one Bash call and returns the W- the
// session ends up bound to ("" when unbound) plus the hook's stdout.
func bindFromBash(t *testing.T, command, stdout string) (bound, hookOut string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	for _, title := range []string{"Antigo trabalho um", "Trabalho recem criado"} {
		if out, err := execCerne(t, "work", "start", title, "--dir", projDir); err != nil {
			t.Fatalf("work start: %v\n%s", err, out)
		}
	}
	stdin := hookJSON(t, "sess-chain", projDir, "Bash",
		map[string]any{"command": command}, map[string]any{"stdout": stdout})
	hookOut, err := execCerneStdin(t, stdin, "hook", "bind-work")
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
	return id, hookOut
}

// F-0876 crit. 3, case that reproduces: a chain whose output lists other works
// before the minted one bound the session to the first W- of the response (an
// old one), not to the work just created.
func TestHookBindWorkChainBindsTheCreatedWork(t *testing.T) {
	listed := "W-0001  [active]  Antigo trabalho um  actor: agent  age: 3d\n"
	created := "W-0002 created (active) — Trabalho recem criado (actor: agent)"
	for name, tc := range map[string]struct{ command, stdout string }{
		"list then start": {`cerne work list; cerne work start "Trabalho recem criado"`, listed + created},
		"pipe to tail":    {`cerne work start "Trabalho recem criado" | tail -1`, created},
		"and-chain":       {`cd /x && cerne work start "Trabalho recem criado" --ref F-0001`, created},
	} {
		t.Run(name, func(t *testing.T) {
			if got, _ := bindFromBash(t, tc.command, tc.stdout); got != "W-0002" {
				t.Fatalf("bound to %q, want W-0002 (the created work)", got)
			}
		})
	}
}

// F-0876 crit. 3: a work start whose output never reached the hook (redirected
// or filtered away) cannot be bound; the hook says so explicitly instead of
// leaving the Edit gate to deny with no explanation.
func TestHookBindWorkWarnsWhenItCannotBind(t *testing.T) {
	for name, stdout := range map[string]string{
		"output redirected": "",
		"output filtered":   "something unrelated",
	} {
		t.Run(name, func(t *testing.T) {
			got, hookOut := bindFromBash(t, `cerne work start "Trabalho recem criado" > /dev/null`, stdout)
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

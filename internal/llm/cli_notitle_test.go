package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// The session-title request (F-0505): every `claude -p` fires a second API
// request that resends the whole prompt, uncached, to name the session, and
// modelUsage counts both. CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1 turns it off.

const noTitleVar = "CLAUDE_CODE_DISABLE_TERMINAL_TITLE"

// envCall is what one runner call saw: its argv and the child environment the
// context carries to execRun/agentRun.
type envCall struct {
	args       []string
	extra, all []string
}

func recordEnv(calls *[]envCall, out string, errs ...error) runner {
	return func(ctx context.Context, _, _ string, args []string, _ string) (string, string, error) {
		extra, _ := ctx.Value(envKey{}).([]string)
		all, _ := ctx.Value(fullEnvKey{}).([]string)
		*calls = append(*calls, envCall{slices.Clone(args), slices.Clone(extra), slices.Clone(all)})
		if n := len(*calls) - 1; n < len(errs) && errs[n] != nil {
			return "", "", errs[n]
		}
		return out, "", nil
	}
}

func hasNoTitle(env []string) bool { return slices.Contains(env, noTitleVar+"=1") }

// F-0505 crit. 1
func TestClaudeOneShotTurnsOffTitleRequest(t *testing.T) {
	var calls []envCall
	p := &cliProvider{bin: "claude", run: recordEnv(&calls, "ok")}
	if _, err := p.Complete(context.Background(), "a prompt long enough to be titled"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(calls) != 1 || !hasNoTitle(calls[0].extra) {
		t.Fatalf("calls = %+v, want one call whose child env has %s=1", calls, noTitleVar)
	}
}

// F-0505 crit. 2
func TestClaudeOldCLIRetryKeepsNoTitle(t *testing.T) {
	var calls []envCall
	old := errors.New(`"claude" exited with exit status 1: error: unknown option '--safe-mode'`)
	p := &cliProvider{bin: "claude", run: recordEnv(&calls, "ok", old)}
	if _, err := p.Complete(context.Background(), "a prompt long enough to be titled"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want the first try and the plain retry", len(calls))
	}
	for i, c := range calls {
		if !hasNoTitle(c.extra) {
			t.Errorf("call %d env = %v, want %s=1", i, c.extra, noTitleVar)
		}
	}
}

// F-0505 crit. 3
func TestClaudeAgenticTurnTurnsOffTitleRequest(t *testing.T) {
	var calls []envCall
	p := &cliProvider{bin: "claude", dir: "/proj", agentTools: AgentReadTools, run: recordEnv(&calls, "ok")}
	if _, err := p.Complete(context.Background(), "a chat turn long enough to be titled"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(calls) != 1 || !hasNoTitle(calls[0].extra) {
		t.Fatalf("calls = %+v, want one agentic call whose child env has %s=1", calls, noTitleVar)
	}
}

// F-0505 crit. 4: the variable travels in the environment, never on argv.
func TestClaudeNoTitleLeavesArgvAlone(t *testing.T) {
	var one, agent []envCall
	(&cliProvider{bin: "claude", run: recordEnv(&one, "ok")}).Complete(context.Background(), "q")
	(&cliProvider{bin: "claude", dir: "/proj", agentTools: AgentReadTools, run: recordEnv(&agent, "ok")}).Complete(context.Background(), "q")
	wantOne := []string{"-p", "--output-format", "text", "--safe-mode", "--tools", ""}
	wantAgent := []string{"-p", "--output-format", "json", "--setting-sources", "project,local", "--strict-mcp-config",
		"--allowedTools", strings.Join(AgentReadTools, ",")}
	if len(one) != 1 || !slices.Equal(one[0].args, wantOne) {
		t.Errorf("one-shot argv = %q, want %q", one, wantOne)
	}
	if len(agent) != 1 || !slices.Equal(agent[0].args, wantAgent) {
		t.Errorf("agentic argv = %q, want %q", agent, wantAgent)
	}
}

// printChildEnv is the re-executed test binary's side of the real-child tests.
func printChildEnv() {
	fmt.Printf("TITLE:%s INHERITED:%s ACTOR:%s DENY:%s\n",
		os.Getenv(noTitleVar), os.Getenv("F0505_INHERITED"), os.Getenv("CERNE_ACTOR"), os.Getenv(DenyLifecycleEnv))
	os.Exit(0)
}

// childRunner runs the test binary itself in place of claude, through the real
// runner, so the environment the child sees is the one execRun/agentRun build.
func childRunner(real runner, test string) runner {
	return func(ctx context.Context, dir, _ string, _ []string, stdin string) (string, string, error) {
		return real(ctx, dir, os.Args[0], []string{"-test.run=" + test + "$"}, stdin)
	}
}

// F-0505 crit. 1, 4: through a real child, the variable wins over an inherited
// value and the rest of the inherited environment arrives unchanged (N-0011).
func TestNoTitleReachesRealOneShotChild(t *testing.T) {
	if os.Getenv("GO_WANT_NOTITLE_CHILD") == "1" {
		printChildEnv()
	}
	t.Setenv("GO_WANT_NOTITLE_CHILD", "1")
	t.Setenv(noTitleVar, "0")
	t.Setenv("F0505_INHERITED", "kept")
	p := &cliProvider{bin: "claude", run: childRunner(execRun, "TestNoTitleReachesRealOneShotChild")}
	out, err := p.Complete(context.Background(), "q")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for _, want := range []string{"TITLE:1", "INHERITED:kept"} {
		if !strings.Contains(out, want) {
			t.Errorf("child env %q, want %s", out, want)
		}
	}
}

// F-0505 crit. 3: the agentic child gets the variable and keeps both stamps,
// which stay last so no extra can override them.
func TestNoTitleReachesRealAgenticChild(t *testing.T) {
	if os.Getenv("GO_WANT_NOTITLE_CHILD") == "1" {
		printChildEnv()
	}
	t.Setenv("GO_WANT_NOTITLE_CHILD", "1")
	t.Setenv(noTitleVar, "0")
	t.Setenv("CERNE_ACTOR", "")
	p := &cliProvider{bin: "claude", agentTools: AgentReadTools, run: childRunner(agentRun, "TestNoTitleReachesRealAgenticChild")}
	out, err := p.Complete(context.Background(), "q")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for _, want := range []string{"TITLE:1", "ACTOR:agent", "DENY:1"} {
		if !strings.Contains(out, want) {
			t.Errorf("agentic child env %q, want %s", out, want)
		}
	}
	ctx := context.WithValue(context.Background(), envKey{}, []string{"CERNE_ACTOR=human", DenyLifecycleEnv + "=0"})
	out, _, err = agentRun(ctx, "", os.Args[0], []string{"-test.run=TestNoTitleReachesRealAgenticChild$"}, "")
	if err != nil {
		t.Fatalf("agentRun: %v", err)
	}
	if !strings.Contains(out, "ACTOR:agent") || !strings.Contains(out, "DENY:1") {
		t.Errorf("agentRun child env %q, want the stamps to win over the extras", out)
	}
}

// F-0505 crit. 5
func TestOtherCLIsDoNotGetNoTitle(t *testing.T) {
	t.Setenv(noTitleVar, "") // an agent session sets it; the copilot env copies os.Environ
	for _, kind := range []string{ModeCodexCLI, ModeGeminiCLI, ModeCursorCLI, ModeCopilotCLI} {
		var calls []envCall
		p := &cliProvider{kind: kind, bin: "x", run: recordEnv(&calls, "", errors.New("boom"))}
		p.Complete(context.Background(), "a prompt long enough to be titled")
		if len(calls) == 0 {
			t.Fatalf("%s: the runner was never called", kind)
		}
		for _, c := range calls {
			if hasNoTitle(c.extra) || hasNoTitle(c.all) {
				t.Errorf("%s: child env carries %s=1", kind, noTitleVar)
			}
		}
	}
}

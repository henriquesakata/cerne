package llm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// copilotStream is what `copilot --output-format json` printed in the probe
// (F-0445, copilot 1.0.112), trimmed to the events cerne reads.
func copilotStream(answer, model string) string {
	return `{"type":"session.start","data":{"copilotVersion":"1.0.112"}}` + "\n" +
		`{"type":"assistant.message","data":{"model":"` + model + `","phase":"commentary","content":"","toolRequests":[]}}` + "\n" +
		`{"type":"assistant.message","data":{"model":"` + model + `","phase":"final_answer","content":"` + answer + `","toolRequests":[]}}` + "\n" +
		`{"type":"result","exitCode":0}` + "\n"
}

// copilotCall is what the provider handed the runner, read while the call runs
// (the scratch directory is gone afterwards).
type copilotCall struct {
	args       []string
	stdin, dir string
	home       string
	dirEntries int
	homeIsDir  bool
}

func copilotFake(reply string, calls *int, got *copilotCall) runner {
	return func(ctx context.Context, dir, _ string, args []string, stdin string) (string, string, error) {
		*calls++
		env, _ := ctx.Value(envKey{}).([]string)
		got.args, got.stdin, got.dir, got.home = args, stdin, dir, ""
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "COPILOT_HOME="); ok {
				got.home = v
			}
		}
		es, _ := os.ReadDir(dir)
		got.dirEntries = len(es)
		st, err := os.Stat(got.home)
		got.homeIsDir = err == nil && st.IsDir()
		return reply, "", nil
	}
}

// F-0445 criteria 1 and 2: one-shot through copilot, prompt on stdin, JSON
// events (the only output that names the model), no question to the user, no
// auto-update, no instruction, no built-in MCP, every tool denied and hidden,
// in an empty scratch directory with a throwaway COPILOT_HOME, never trusted.
func TestCopilotOneShotArgv(t *testing.T) {
	var calls int
	var got copilotCall
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", model: "claude-sonnet-5.5", effort: "high", run: copilotFake(copilotStream("the answer", "claude-sonnet-5.5"), &calls, &got)}
	out, err := p.Complete(context.Background(), "question?")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "the answer" || got.stdin != "question?" || p.AnsweredModel() != "claude-sonnet-5.5" {
		t.Fatalf("out = %q, stdin = %q, answered = %q", out, got.stdin, p.AnsweredModel())
	}
	want := []string{"-s", "--output-format", "json", "--no-ask-user", "--no-auto-update", "--no-custom-instructions", "--disable-builtin-mcps",
		"--deny-tool", "shell", "--deny-tool", "write", "--deny-tool", "url", "--deny-tool", "read", "--deny-tool", "memory",
		"--excluded-tools", "web_fetch", "--available-tools", "cerne-no-tools",
		"--model", "claude-sonnet-5.5", "--effort", "high"}
	if !reflect.DeepEqual(got.args, want) {
		t.Fatalf("args = %v\nwant   %v", got.args, want)
	}
	for _, a := range got.args {
		if a == "-p" || strings.HasPrefix(a, "--trust") || strings.HasPrefix(a, "--allow") || a == "--yolo" || a == "--add-dir" {
			t.Fatalf("%q must never reach the Copilot CLI: %v", a, got.args)
		}
	}
	if got.dirEntries != 0 || !got.homeIsDir || got.home == "" || strings.HasPrefix(got.home, got.dir+string(filepath.Separator)) {
		t.Fatalf("dir %q (%d entries) home %q (dir %v): want an empty working dir and a separate COPILOT_HOME", got.dir, got.dirEntries, got.home, got.homeIsDir)
	}
	for _, d := range []string{got.dir, got.home} {
		if _, err := os.Stat(d); err == nil {
			t.Fatalf("%s must be removed after the call", d)
		}
	}
	p.model, p.effort = "", ""
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	for _, a := range got.args {
		if a == "--model" || a == "--effort" {
			t.Fatalf("%s passed without a value: %v", a, got.args)
		}
	}
}

// A bad effort word never reaches the argv.
func TestCopilotEffortIsABareWord(t *testing.T) {
	var calls int
	var got copilotCall
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", effort: "high --allow-all", run: copilotFake(copilotStream("x", "m"), &calls, &got)}
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	for _, a := range got.args {
		if strings.Contains(a, "allow") || a == "--effort" {
			t.Fatalf("effort leaked into args: %v", got.args)
		}
	}
}

// F-0445 criterion 2: an isolation flag the CLI does not know is never dropped.
// The text is the probe's, from copilot 1.0.112.
func TestCopilotRejectedFlag(t *testing.T) {
	calls := 0
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: func(context.Context, string, string, []string, string) (string, string, error) {
		calls++
		return "", "", errors.New(`"copilot" exited with exit status 1: error: unexpected argument '--available-tools' found`)
	}}
	_, err := p.Complete(context.Background(), "q")
	if !errors.Is(err, ErrCopilotCLI) || errors.Is(err, ErrClaudeCLI) || !strings.Contains(err.Error(), "update") || !strings.Contains(err.Error(), "@github/copilot") || calls != 1 {
		t.Fatalf("err = %v after %d calls, want the update instruction and no retry", err, calls)
	}
}

// F-0445 criterion 4: one message per cause, naming the fix. The texts come
// from the probe, except the organization policy, which no probe could cause.
func TestCopilotFailureHints(t *testing.T) {
	if got := cliProduct("copilot"); got != "GitHub Copilot CLI" {
		t.Fatalf("cliProduct = %q", got)
	}
	for _, c := range []struct{ name, err, want string }{
		{"missing binary", `could not run "copilot" (install GitHub Copilot CLI and ensure it is on PATH): exec: not found`, "npm install -g @github/copilot"},
		{"no login", `"copilot" exited with exit status 1: Error: No authentication information found.`, "copilot login"},
		{"no login token", `"copilot" exited with exit status 1: Error: No authentication information found.`, "COPILOT_GITHUB_TOKEN"},
		{"model not allowed", `"copilot" exited with exit status 1: Execution failed: Model "gpt-x" is not available.`, "another model"},
		{"organization policy", `"copilot" exited with exit status 1: Copilot CLI is disabled by your organization's policy`, "administrator"},
	} {
		p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: func(context.Context, string, string, []string, string) (string, string, error) {
			return "", "", errors.New(c.err)
		}}
		_, err := p.Complete(context.Background(), "q")
		if !errors.Is(err, ErrCopilotCLI) || errors.Is(err, ErrClaudeCLI) {
			t.Fatalf("%s: err = %v, want ErrCopilotCLI only", c.name, err)
		}
		if !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "Claude") {
			t.Fatalf("%s: err = %v, want %q and no Claude wording", c.name, err, c.want)
		}
	}
}

// In JSON mode the CLI reports a failure as a session.error event on stdout,
// ahead of events that push it out of describeExit's tail: it must still name
// the cause.
func TestCopilotSessionErrorSurvivesTheTail(t *testing.T) {
	stdout := `{"type":"session.error","data":{"errorType":"query","message":"Execution failed: Model \"zzz\" is not available."}}` + "\n" +
		strings.Repeat(`{"type":"assistant.idle","data":{}}`+"\n", 30) + `{"type":"result","exitCode":1}` + "\n"
	got := describeExit("copilot", "exit status 1", []byte(stdout), nil)
	if !strings.Contains(got, `Model "zzz" is not available`) {
		t.Fatalf("describeExit = %q", got)
	}
}

func TestCopilotEmptyAnswerNamesLoginModelAndVersion(t *testing.T) {
	var calls int
	var got copilotCall
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: copilotFake(copilotStream("  ", "m"), &calls, &got)}
	_, err := p.Complete(context.Background(), "q")
	if !errors.Is(err, ErrCopilotCLI) {
		t.Fatalf("err = %v, want ErrCopilotCLI", err)
	}
	for _, w := range []string{"login", "--model", "version"} {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("err = %v, want %q", err, w)
		}
	}
}

func TestCopilotProviderWiring(t *testing.T) {
	cfg := project.LLMConfig{Mode: ModeCopilotCLI}
	if got := ConfiguredProviders(cfg); !reflect.DeepEqual(got, []string{"copilot"}) {
		t.Fatalf("ConfiguredProviders = %v", got)
	}
	if got := ModeForProvider("copilot"); got != ModeCopilotCLI {
		t.Fatalf("ModeForProvider = %q", got)
	}
	pr, err := NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cp, ok := pr.(*cliProvider); !ok || cp.bin != "copilot" || cp.kind != ModeCopilotCLI {
		t.Fatalf("NewProvider = %#v", pr)
	}
	if _, err := ListModels(context.Background(), cfg); !errors.Is(err, ErrNoListing) {
		t.Fatalf("ListModels err = %v, want ErrNoListing", err)
	}
}

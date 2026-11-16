package llm

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
	env        []string
}

func copilotFake(reply string, calls *int, got *copilotCall) runner {
	return func(ctx context.Context, dir, _ string, args []string, stdin string) (string, string, error) {
		*calls++
		env, _ := ctx.Value(fullEnvKey{}).([]string)
		got.env = env
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
	// The login hint depends on the platform and on a token in the environment
	// (GITHUB_TOKEN is set on CI runners): pin both, the per-platform cases are
	// in TestCopilotLoginHintPerPlatform.
	origOS, origEnv := copilotGOOS, copilotLookEnv
	t.Cleanup(func() { copilotGOOS, copilotLookEnv = origOS, origEnv })
	copilotGOOS = "darwin"
	copilotLookEnv = func(string) (string, bool) { return "", false }
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

func envHas(env []string, entry string) bool {
	for _, e := range env {
		if e == entry {
			return true
		}
	}
	return false
}

func envKeyed(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

// A COPILOT_* variable of the user (allow-all, extra skill or agent dirs…)
// never reaches the child: only the auth tokens survive, and COPILOT_HOME is
// ours.
func TestCopilotEnvDropsInheritedConfig(t *testing.T) {
	parent := []string{"PATH=/bin", "COPILOT_ALLOW_ALL=true", "COPILOT_SKILLS_DIRS=/x", "COPILOT_GITHUB_TOKEN=t", "GH_TOKEN=g", "COPILOT_HOME=/user"}
	got := copilotEnv(parent, "/h")
	for _, want := range []string{"PATH=/bin", "COPILOT_GITHUB_TOKEN=t", "GH_TOKEN=g", "COPILOT_HOME=/h"} {
		if !envHas(got, want) {
			t.Fatalf("env %v lacks %q", got, want)
		}
	}
	for _, bad := range []string{"COPILOT_ALLOW_ALL", "COPILOT_SKILLS_DIRS"} {
		if envKeyed(got, bad) {
			t.Fatalf("env %v still has %s", got, bad)
		}
	}
	if envHas(got, "COPILOT_HOME=/user") {
		t.Fatalf("the user's COPILOT_HOME survived: %v", got)
	}
}

// The provider hands the filtered environment to the runner, and execRun uses
// it as is.
func TestCopilotChildEnvironmentIsFiltered(t *testing.T) {
	t.Setenv("COPILOT_ALLOW_ALL", "true")
	t.Setenv("COPILOT_EXTRA_DIRS", "/x")
	var calls int
	var got copilotCall
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: copilotFake(copilotStream("x", "m"), &calls, &got)}
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if envKeyed(got.env, "COPILOT_ALLOW_ALL") || envKeyed(got.env, "COPILOT_EXTRA_DIRS") || !envKeyed(got.env, "COPILOT_HOME") {
		t.Fatalf("child env = %v", got.env)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	ctx := context.WithValue(context.Background(), fullEnvKey{}, []string{"ONLY=1"})
	out, _, err := execRun(ctx, "", "sh", []string{"-c", "echo ${ONLY}-${HOME:-nohome}"}, "")
	if err != nil || strings.TrimSpace(out) != "1-nohome" {
		t.Fatalf("execRun with a full env = %q, %v", out, err)
	}
}

// A --model that looks like a flag never reaches the argv.
func TestCopilotModelIsABareWord(t *testing.T) {
	var calls int
	var got copilotCall
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", model: "--allow-all-tools", run: copilotFake(copilotStream("x", "m"), &calls, &got)}
	_, err := p.Complete(context.Background(), "q")
	if !errors.Is(err, ErrCopilotCLI) || calls != 0 || strings.Contains(err.Error(), "update") {
		t.Fatalf("err = %v after %d calls, want an invalid-model error and no run", err, calls)
	}
	p.model = "claude-sonnet-5.5"
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatalf("a real model id was refused: %v", err)
	}
}

func failing(msg string) runner {
	return func(context.Context, string, string, []string, string) (string, string, error) {
		return "", "", errors.New(msg)
	}
}

// Classification order: the model message is anchored, the policy is checked
// before the login and before the generic cases. The policy texts are
// provisional: no probe could cause one.
func TestCopilotClassificationOrder(t *testing.T) {
	for _, c := range []struct{ name, err, want, not string }{
		{"policy wins over not available", `"copilot" exited with exit status 1: Copilot CLI is not available for your organization`, "administrator", "another model"},
		{"policy wins over unauthorized", `"copilot" exited with exit status 1: unauthorized: blocked by your organization's policy`, "administrator", "copilot login"},
		{"model anchored", `"copilot" exited with exit status 1: Execution failed: Model "gpt-x" is not available.`, "another model", "administrator"},
		{"unrelated not available is generic", `"copilot" exited with exit status 1: the network is not available`, "use another mode", "another model"},
	} {
		p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: failing(c.err)}
		_, err := p.Complete(context.Background(), "q")
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), c.not) {
			t.Fatalf("%s: err = %v, want %q and not %q", c.name, err, c.want, c.not)
		}
	}
}

// F-0445 critic: on a platform where the throwaway COPILOT_HOME may hide the
// stored login, a missing login names the token route.
func TestCopilotLoginHintPerPlatform(t *testing.T) {
	origOS, origEnv := copilotGOOS, copilotLookEnv
	t.Cleanup(func() { copilotGOOS, copilotLookEnv = origOS, origEnv })
	const noAuth = `"copilot" exited with exit status 1: Error: No authentication information found.`
	for _, c := range []struct {
		goos   string
		tokens map[string]string
		want   string
		not    string
	}{
		{"linux", nil, "throwaway COPILOT_HOME", ""},
		{"windows", nil, "hides the stored login", ""},
		{"linux", map[string]string{"GH_TOKEN": "x"}, "copilot login", "throwaway"},
		{"darwin", nil, "copilot login", "throwaway"},
	} {
		copilotGOOS = c.goos
		copilotLookEnv = func(k string) (string, bool) { v, ok := c.tokens[k]; return v, ok }
		p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: failing(noAuth)}
		_, err := p.Complete(context.Background(), "q")
		if err == nil || !strings.Contains(err.Error(), c.want) || (c.not != "" && strings.Contains(err.Error(), c.not)) || !strings.Contains(err.Error(), "COPILOT_GITHUB_TOKEN") {
			t.Fatalf("%s %v: err = %v, want %q, not %q", c.goos, c.tokens, err, c.want, c.not)
		}
	}
}

// describeExit finds the session.error on stdout even when stderr has noise.
func TestCopilotSessionErrorBeatsStderrNoise(t *testing.T) {
	stdout := `{"type":"session.error","data":{"message":"Execution failed: Model \"zzz\" is not available."}}` + "\n"
	got := describeExit("copilot", "exit status 1", []byte(stdout), []byte("A new version is available: 1.0.113\n"))
	if !strings.Contains(got, `Model "zzz" is not available`) {
		t.Fatalf("describeExit = %q", got)
	}
}

// Exit 0, a session.error and no answer: the error carries the message, not
// the generic login hint.
func TestCopilotSessionErrorWithoutAnswer(t *testing.T) {
	out := `{"type":"session.error","data":{"message":"Execution failed: Model \"zzz\" is not available."}}` + "\n" + `{"type":"result","exitCode":0}` + "\n"
	var calls int
	var got copilotCall
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: copilotFake(out, &calls, &got)}
	_, err := p.Complete(context.Background(), "q")
	if !errors.Is(err, ErrCopilotCLI) || !strings.Contains(err.Error(), `Model "zzz" is not available`) || !strings.Contains(err.Error(), "another model") || strings.Contains(err.Error(), "no answer") {
		t.Fatalf("err = %v", err)
	}
}

// Reply selection: the final answer beats later commentary; without one the
// last non-empty message counts; noise and a cut last line are skipped.
func TestCopilotReplySelection(t *testing.T) {
	msg := func(phase, model, content string) string {
		return `{"type":"assistant.message","data":{"model":"` + model + `","phase":"` + phase + `","content":"` + content + `"}}` + "\n"
	}
	for _, c := range []struct{ name, out, answer, model string }{
		{"final beats later commentary", msg("final_answer", "m1", "the answer") + msg("commentary", "m2", "chatter"), "the answer", "m1"},
		{"no final: last non-empty", msg("commentary", "m1", "first") + msg("commentary", "m2", "second") + msg("commentary", "m3", ""), "second", "m2"},
		{"noise and a cut line", "A new version is available\n" + msg("final_answer", "m1", "ok") + `{"type":"assistant.message","data":{"model":"m9","pha`, "ok", "m1"},
	} {
		var calls int
		var got copilotCall
		p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: copilotFake(c.out, &calls, &got)}
		out, err := p.Complete(context.Background(), "q")
		if err != nil || out != c.answer || p.AnsweredModel() != c.model {
			t.Fatalf("%s: %q / %q, %v; want %q / %q", c.name, out, p.AnsweredModel(), err, c.answer, c.model)
		}
	}
}

// Another clap wording of an unknown flag is still "update the CLI"; a value
// the CLI refuses is not.
func TestCopilotFlagWordings(t *testing.T) {
	for _, c := range []struct{ err, want, not string }{
		{`"copilot" exited with exit status 1: error: Found argument '--available-tools' which wasn't expected, or isn't valid in this context`, "update", ""},
		{`"copilot" exited with exit status 1: error: invalid value 'turbo' for '--effort <EFFORT>'`, "effort", "update it"},
	} {
		p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", effort: "turbo", run: failing(c.err)}
		_, err := p.Complete(context.Background(), "q")
		if !errors.Is(err, ErrCopilotCLI) || !strings.Contains(err.Error(), c.want) || (c.not != "" && strings.Contains(err.Error(), c.not)) {
			t.Fatalf("err = %v, want %q and not %q", err, c.want, c.not)
		}
	}
}

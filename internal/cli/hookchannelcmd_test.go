package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/room"
)

func sessionStartJSON(t *testing.T, sess, cwd string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"session_id": sess, "cwd": cwd, "hook_event_name": "SessionStart", "source": "startup"})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestHookChannelExportsSession (F-0325): session start exports CERNE_SESSION
// through CLAUDE_ENV_FILE so the agent's own commands (msg, next, status) know
// their session. Only an id that is inert when the file is sourced by bash is
// written, appended after whatever other hooks exported.
func TestHookChannelExportsSession(t *testing.T) {
	projDir, _, _ := setupProject(t)
	env := filepath.Join(t.TempDir(), "env.sh")
	if err := os.WriteFile(env, []byte("export OTHER=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_ENV_FILE", env)

	if out, err := execCerneStdin(t, sessionStartJSON(t, "4f1c-9a_b.2", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("hook channel: %v\n%s", err, out)
	}
	for _, bad := range []string{"a;rm -rf x", "a b", "$(id)", "", "x\nexport Y=1"} {
		if out, err := execCerneStdin(t, sessionStartJSON(t, bad, projDir), "hook", "channel", "--point", "session-start"); err != nil {
			t.Fatalf("hook channel %q: %v\n%s", bad, err, out)
		}
	}
	got, err := os.ReadFile(env)
	if err != nil {
		t.Fatal(err)
	}
	if want := "export OTHER=1\nexport CERNE_SESSION=4f1c-9a_b.2\n"; string(got) != want {
		t.Fatalf("env file = %q, want %q", got, want)
	}

	t.Setenv("CLAUDE_ENV_FILE", "")
	if out, err := execCerneStdin(t, sessionStartJSON(t, "s2", projDir), "hook", "channel", "--point", "session-start"); err != nil {
		t.Fatalf("without CLAUDE_ENV_FILE: %v\n%s", err, out)
	}
}

// TestHookChannelSessionStart (F-0325 crit. 15/19): session start prints the
// session's pending channel block as plain text (the script's stdout is plain
// text — JSON would break it), then nothing; a corrupt channel is silence.
func TestHookChannelSessionStart(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "terminei o contrato, pode consumir")
	payload := sessionStartJSON(t, "sess-a", projDir)

	out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start")
	if err != nil || out != "cerne channel:\n#1 "+b+": terminei o contrato, pode consumir\n" {
		t.Fatalf("first session start: %v\n%q", err, out)
	}
	if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start"); err != nil || out != "" {
		t.Fatalf("nothing new: %v %q, want zero bytes", err, out)
	}
	seedChannel(t, repoDir, b, "mais uma")
	if err := os.WriteFile(filepath.Join(filepath.Dir(repoDir), "rooms", "channel", "lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start"); err != nil || out != "" {
		t.Fatalf("locked channel: %v %q, want silent exit 0", err, out)
	}
	if out, err := execCerneStdin(t, "garbage{", "hook", "channel", "--point", "session-start"); err != nil || out != "" {
		t.Fatalf("garbage payload: %v %q", err, out)
	}
}

// TestHookSessionContextIncludesChannel (crit. 15): the provider-envelope
// session start (F-0174 parity: Codex, Cursor, Gemini) carries the same block
// at the end of its context.
func TestHookSessionContextIncludesChannel(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde")

	out, err := execCerneStdin(t, hookJSON(t, "sess-a", projDir, "", nil, nil), "hook", "session-context", "--format", "codex")
	if err != nil {
		t.Fatalf("session-context: %v\n%s", err, out)
	}
	c := channelEnvelope(t, out)
	if want := "\n\ncerne channel:\n#1 " + b + ": build verde\n"; !strings.HasSuffix(c.HookSpecificOutput.AdditionalContext, want) {
		t.Fatalf("context does not end with the channel block:\n%s", c.HookSpecificOutput.AdditionalContext)
	}
	out, err = execCerneStdin(t, hookJSON(t, "sess-a", projDir, "", nil, nil), "hook", "session-context", "--format", "codex")
	if err != nil || strings.Contains(out, "cerne channel:") {
		t.Fatalf("delivered twice: %v\n%s", err, out)
	}
}

// TestHookChannelSubagentPeek (crit. 15): a subagent shares its parent's
// session id and a context that will be thrown away, so it gets a peek — it
// records what it was shown but never moves the parent's watermark.
func TestHookChannelSubagentPeek(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	t.Setenv("CLAUDE_ENV_FILE", "")
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde")
	payload := sessionStartJSON(t, "sess-a", projDir)

	want := "cerne channel:\n#1 " + b + ": build verde\n"
	for i := range 2 {
		if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "subagent"); err != nil || out != want {
			t.Fatalf("subagent peek %d: %v %q", i+1, err, out)
		}
	}
	dels, err := room.OpenChannel(filepath.Join(filepath.Dir(repoDir), "rooms")).Deliveries()
	if err != nil || len(dels) != 2 || dels[0].Through != 0 || dels[0].Seqs[0] != 1 {
		t.Fatalf("peek deliveries = %+v, %v", dels, err)
	}
	if out, err := execCerneStdin(t, payload, "hook", "channel", "--point", "session-start"); err != nil || out != want {
		t.Fatalf("parent lost the background to a peek: %v %q", err, out)
	}
}

// channelContactFixture: sess-a and sess-b on live works, one background
// message from b pending for sess-a.
func channelContactFixture(t *testing.T) (projDir, repoDir, block string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde")
	return projDir, repoDir, "cerne channel:\n#1 " + b + ": build verde\n"
}

// assertLeadsWithChannel runs args with and without CERNE_SESSION: the text
// output gains the block plus a blank line on top and is otherwise unchanged;
// --json never changes; no session never consumes; a locked channel changes
// nothing (crit. 15/19).
func assertLeadsWithChannel(t *testing.T, repoDir, block string, args ...string) {
	t.Helper()
	run := func(sess string, extra ...string) string {
		t.Helper()
		t.Setenv("CERNE_SESSION", sess)
		out, err := execCerne(t, append(slices.Clone(args), extra...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return out
	}
	baseline := run("")
	if strings.Contains(baseline, "cerne channel:") {
		t.Fatalf("no session consumed the channel:\n%s", baseline)
	}
	if jsonOut := run("sess-a", "--json"); jsonOut != run("", "--json") {
		t.Fatalf("--json changed with a session:\n%s", jsonOut)
	}
	lockPath := filepath.Join(filepath.Dir(repoDir), "rooms", "channel", "lock")
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run("sess-a"); out != baseline {
		t.Fatalf("locked channel changed the output:\n%s", out)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if out := run("sess-a"); out != block+"\n"+baseline {
		t.Fatalf("output does not lead with the channel block:\n%s", out)
	}
	if out := run("sess-a"); out != baseline {
		t.Fatalf("block delivered twice:\n%s", out)
	}
}

// TestNextLeadsWithChannel (F-0325 crit. 15): an agent's `cerne next` opens
// with its session's pending channel messages.
func TestNextLeadsWithChannel(t *testing.T) {
	projDir, repoDir, block := channelContactFixture(t)
	assertLeadsWithChannel(t, repoDir, block, "next", "--dir", projDir)
}

// TestStatusActiveLeadsWithChannel (crit. 15): `status --active` — the agent's
// lean view — opens with the block, even on the "No features yet" path; plain
// `status` never carries it.
func TestStatusActiveLeadsWithChannel(t *testing.T) {
	projDir, repoDir, block := channelContactFixture(t)
	t.Setenv("CERNE_SESSION", "sess-a")
	if out, err := execCerne(t, "status", "--dir", projDir); err != nil || strings.Contains(out, "cerne channel:") {
		t.Fatalf("plain status: %v\n%s", err, out)
	}
	assertLeadsWithChannel(t, repoDir, block, "status", "--active", "--dir", projDir)
}

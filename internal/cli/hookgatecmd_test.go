package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/cost"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/room"
)

// execCerneStdin runs the CLI with args feeding stdin — the hook plumbing
// subcommands read their payload there.
func execCerneStdin(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := Root()
	out := &bytes.Buffer{}
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func hookJSON(t *testing.T, sessionID, cwd, toolName string, toolInput, toolResponse any) string {
	t.Helper()
	payload := map[string]any{
		"session_id": sessionID,
		"cwd":        cwd,
		"tool_name":  toolName,
		"tool_input": toolInput,
	}
	if toolResponse != nil {
		payload["tool_response"] = toolResponse
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal hook payload: %v", err)
	}
	return string(raw)
}

// enableWorkGate rewrites the project's .cernerc with [gates] work_gate on.
func enableWorkGate(t *testing.T, projDir string, extraAllow ...string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.WorkGate = "on"
	cfg.Gates.WorkGateAllow = extraAllow
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestHookBindWorkBindsFromWorkStart (F-0151 crit. 1): a successful
// `cerne work start` seen by the PostToolUse hook binds the session to the
// minted W- id.
func TestHookBindWorkBindsFromWorkStart(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}

	stdin := hookJSON(t, "sess-hook", projDir, "Bash",
		map[string]any{"command": `cerne work start "Investigar erro X"`},
		map[string]any{"stdout": "W-0001 criada (active) — Investigar erro X (ator: agent)"})
	if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}

	cacheDir := filepath.Dir(repoDir)
	id, ok, err := app.CurrentBinding(cacheDir, repoDir, "sess-hook")
	if err != nil {
		t.Fatalf("CurrentBinding: %v", err)
	}
	if !ok || id != "W-0001" {
		t.Fatalf("binding = (%q, %v), want (W-0001, true)", id, ok)
	}
}

// TestHookBindWorkRecordsAgentInLedger (F-0309): a bind fired from inside a
// subagent records the payload's agent_id (and the claude-code harness) on the
// cost ledger line; the sessions/ binding is written as before.
func TestHookBindWorkRecordsAgentInLedger(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	stdin := strings.Replace(hookJSON(t, "sess-hook", projDir, "Bash",
		map[string]any{"command": `cerne work start "Investigar erro X"`},
		map[string]any{"stdout": "W-0001 criada (active) — Investigar erro X (ator: agent)"}),
		"{", `{"agent_id":"a1b2",`, 1)
	if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}

	cacheDir := filepath.Dir(repoDir)
	if id, ok, err := app.CurrentBinding(cacheDir, repoDir, "sess-hook"); err != nil || !ok || id != "W-0001" {
		t.Fatalf("binding = (%q, %v, %v), want (W-0001, true)", id, ok, err)
	}
	raw, err := os.ReadFile(filepath.Join(cacheDir, "costbind.ndjson"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	var b cost.Bind
	if err := json.Unmarshal(bytes.TrimSpace(raw), &b); err != nil {
		t.Fatalf("ledger %q: %v", raw, err)
	}
	if b.Session != "sess-hook" || b.Agent != "a1b2" || b.Entity != "W-0001" || b.Harness != cost.HarnessClaudeCode {
		t.Fatalf("ledger line = %+v, want sess-hook/a1b2/W-0001/claude-code", b)
	}
}

// TestHookBindWorkIgnoresOtherCommands: unrelated Bash calls, and hook input
// that is not even JSON, bind nothing and never error (fail-open plumbing).
func TestHookBindWorkIgnoresOtherCommands(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	for name, stdin := range map[string]string{
		"other command": hookJSON(t, "sess-a", projDir, "Bash",
			map[string]any{"command": "go test ./..."}, map[string]any{"stdout": "ok W-0001"}),
		"failed work start (no id in response)": hookJSON(t, "sess-a", projDir, "Bash",
			map[string]any{"command": "cerne work start x"}, map[string]any{"stdout": "Error: similar exists"}),
		"not json": "garbage{",
	} {
		if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
			t.Fatalf("%s: hook bind-work errored: %v\noutput: %s", name, err, out)
		}
	}
	if _, ok, _ := app.CurrentBinding(filepath.Dir(repoDir), repoDir, "sess-a"); ok {
		t.Fatal("nothing should have been bound")
	}
}

// TestHookWorkGateDeniesUnbound (F-0151 crit. 2): gate on + project file +
// unbound session -> deny JSON with an actionable reason.
func TestHookWorkGateDeniesUnbound(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)

	stdin := hookJSON(t, "sess-unbound", projDir, "Edit",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("hook work-gate: %v\noutput: %s", err, out)
	}
	var d gateDeny
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("deny output is not JSON: %v\noutput: %s", err, out)
	}
	if d.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("decision = %q, want deny:\n%s", d.HookSpecificOutput.PermissionDecision, out)
	}
	if !strings.Contains(d.HookSpecificOutput.PermissionDecisionReason, "cerne work start") {
		t.Fatalf("reason not actionable:\n%s", out)
	}
}

// TestHookWorkGateAllows (crit. 3/4): bound session, allowlisted paths, and a
// disabled gate all pass silently (no output at all).
func TestHookWorkGateAllows(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir, "docs/**")
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	cacheDir := filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, "sess-bound", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}

	codePath := filepath.Join(projDir, "internal", "a.go")
	cases := map[string]string{
		"bound session":   hookJSON(t, "sess-bound", projDir, "Edit", map[string]any{"file_path": codePath}, nil),
		".docs path":      hookJSON(t, "sess-x", projDir, "Write", map[string]any{"file_path": filepath.Join(projDir, ".docs", "n.md")}, nil),
		"extra allowlist": hookJSON(t, "sess-x", projDir, "Write", map[string]any{"file_path": filepath.Join(projDir, "docs", "n.md")}, nil),
		"outside project": hookJSON(t, "sess-x", projDir, "Edit", map[string]any{"file_path": filepath.Join(t.TempDir(), "s.md")}, nil),
	}
	for name, stdin := range cases {
		out, err := execCerneStdin(t, stdin, "hook", "work-gate")
		if err != nil {
			t.Fatalf("%s: %v\noutput: %s", name, err, out)
		}
		if strings.TrimSpace(out) != "" {
			t.Fatalf("%s: allow must be silent, got:\n%s", name, out)
		}
	}

	// Gate off (default config): even the unbound project file passes.
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.WorkGate = ""
	cfg.Gates.WorkGateAllow = nil
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stdin := hookJSON(t, "sess-x", projDir, "Edit", map[string]any{"file_path": codePath}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("gate off: %v\noutput: %s", err, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("gate off must be silent, got:\n%s", out)
	}
}

// TestHookBindWorkBindsFromFeatureStart (crit. 1): a confirmed
// `cerne feature start F-…` binds the F- id taken from the command.
func TestHookBindWorkBindsFromFeatureStart(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	stdin := hookJSON(t, "sess-f", projDir, "Bash",
		map[string]any{"command": "cerne feature start F-0002"},
		map[string]any{"stdout": "F-0002: approved → in_development"})
	if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}
	// The binding file exists even though F-0002 does not (CurrentBinding
	// would then read it as stale) — assert the raw bind happened.
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(repoDir), "sessions", "sess-f"))
	if err != nil {
		t.Fatalf("binding file: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "F-0002" {
		t.Fatalf("bound id = %q, want F-0002", got)
	}
}

// TestHookBindWorkBindsEpicAndTaskStart (F-0309 C5): `feature start` of an
// E- or T- binds it like an F- — each start writes its ledger line.
func TestHookBindWorkBindsEpicAndTaskStart(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for i, id := range []string{"T-0007", "E-0002", "F-0005"} {
		stdin := hookJSON(t, "sess-"+id, projDir, "Bash",
			map[string]any{"command": "cerne feature start " + id},
			map[string]any{"stdout": id + ": approved → in_development"})
		if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
			t.Fatalf("%s: hook bind-work: %v\noutput: %s", id, err, out)
		}
		raw, _ := os.ReadFile(filepath.Join(filepath.Dir(repoDir), "costbind.ndjson"))
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) != i+1 || !strings.Contains(lines[i], `"entity_id":"`+id+`"`) {
			t.Fatalf("%s: ledger =\n%s", id, raw)
		}
	}
}

// TestHookWorkGateDenyHint (F-0170 crit. 4): with [gates] work_gate_hint set,
// the deny reason carries the project's suffix; without it, the reason stays
// free of any hint separator.
func TestHookWorkGateDenyHint(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.WorkGateHint = "see the project convention catalog"
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	stdin := hookJSON(t, "sess-hint", projDir, "Edit",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("hook work-gate: %v\noutput: %s", err, out)
	}
	var d gateDeny
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("deny output is not JSON: %v\noutput: %s", err, out)
	}
	if !strings.Contains(d.HookSpecificOutput.PermissionDecisionReason, "see the project convention catalog") {
		t.Fatalf("reason missing hint:\n%s", out)
	}

	// Without the hint the reason must not carry the separator suffix.
	cfg.Gates.WorkGateHint = ""
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	out, err = execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("hook work-gate (no hint): %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "convention catalog") {
		t.Fatalf("reason should have no hint:\n%s", out)
	}
}

// seedChannel posts body to the project's permanent channel as nick, now.
func seedChannel(t *testing.T, repoDir, nick, body string) int {
	t.Helper()
	m, err := room.OpenChannel(filepath.Join(filepath.Dir(repoDir), "rooms")).
		Append(room.Message{Type: "message", Nick: nick, Body: body, Time: time.Now().UTC()})
	if err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	return m.Seq
}

// channelEnvelope decodes a PreToolUse additionalContext envelope.
func channelEnvelope(t *testing.T, out string) sessionContext {
	t.Helper()
	var c sessionContext
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("not a context envelope: %v\n%s", err, out)
	}
	return c
}

// TestHookWorkGateInjectsChannel (F-0325 crit. 15): an allowed Edit carries the
// session's pending channel messages as PreToolUse additionalContext — the
// exact moment a collision would happen.
func TestHookWorkGateInjectsChannel(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor", "--paths", "internal/server/**")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "vou mexer em internal/server/rooms.go, segura")

	stdin := hookJSON(t, "sess-a", projDir, "Edit", map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("hook work-gate: %v\n%s", err, out)
	}
	c := channelEnvelope(t, out)
	if c.HookSpecificOutput.HookEventName != "PreToolUse" ||
		!strings.HasPrefix(c.HookSpecificOutput.AdditionalContext, "cerne channel:\n#1 "+b+" → you: vou mexer") {
		t.Fatalf("envelope = %+v", c)
	}
	if strings.Contains(out, "permissionDecision") {
		t.Fatalf("an allow must not carry a decision:\n%s", out)
	}
}

// TestHookWorkGateInjectsWithGateOff: the channel rides the gate hook even when
// the gate itself is off, and for codex too (same envelope, R-0035).
func TestHookWorkGateInjectsWithGateOff(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde no main")

	for _, args := range [][]string{{"hook", "work-gate"}, {"hook", "work-gate", "--format", "codex"}} {
		sess := "sess-a"
		if len(args) > 2 {
			sess = "sess-c" // an unregistered session still hears the channel
		}
		stdin := hookJSON(t, sess, projDir, "Write", map[string]any{"file_path": filepath.Join(projDir, "x.go")}, nil)
		out, err := execCerneStdin(t, stdin, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if c := channelEnvelope(t, out); !strings.Contains(c.HookSpecificOutput.AdditionalContext, "#1 "+b+": build verde no main") {
			t.Fatalf("%v: envelope = %+v", args, c)
		}
	}
	for _, format := range []string{"cursor", "gemini"} {
		stdin := hookJSON(t, "sess-d", projDir, "Write", map[string]any{"file_path": filepath.Join(projDir, "x.go")}, nil)
		if out, err := execCerneStdin(t, stdin, "hook", "work-gate", "--format", format); err != nil || out != "" {
			t.Fatalf("%s: no documented allow context — want silence, got %v\n%s", format, err, out)
		}
	}
}

// TestHookWorkGateDenyUnchanged: a deny is byte-for-byte what it was, and
// consumes nothing from the channel.
func TestHookWorkGateDenyUnchanged(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir)
	stdin := hookJSON(t, "sess-unbound", projDir, "Edit", map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	before, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatal(err)
	}
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde no main")
	after, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil || after != before || !strings.Contains(after, `"permissionDecision":"deny"`) {
		t.Fatalf("deny changed:\nbefore %s\nafter  %s", before, after)
	}
	if dels, _ := room.OpenChannel(filepath.Join(filepath.Dir(repoDir), "rooms")).Deliveries(); len(dels) != 0 {
		t.Fatalf("a deny consumed the channel: %+v", dels)
	}
}

// TestHookWorkGateChannelSilent (crit. 5): nothing pending is zero bytes.
func TestHookWorkGateChannelSilent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	stdin := hookJSON(t, "sess-a", projDir, "Edit", map[string]any{"file_path": filepath.Join(projDir, "a.go")}, nil)
	if out, err := execCerneStdin(t, stdin, "hook", "work-gate"); err != nil || out != "" {
		t.Fatalf("no channel: %v %q", err, out)
	}
	seedChannel(t, repoDir, b, "build verde")
	if out, _ := execCerneStdin(t, stdin, "hook", "work-gate"); out == "" {
		t.Fatal("first edit got nothing")
	}
	if out, err := execCerneStdin(t, stdin, "hook", "work-gate"); err != nil || out != "" {
		t.Fatalf("second edit: %v %q, want zero bytes", err, out)
	}
}

// TestHookWorkGateChannelFailOpen (crit. 19, N-0021): a corrupt, unreadable or
// locked channel never breaks the Edit — exit 0, nothing printed.
func TestHookWorkGateChannelFailOpen(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, "build verde")
	chDir := filepath.Join(filepath.Dir(repoDir), "rooms", "channel")
	stdin := hookJSON(t, "sess-a", projDir, "Edit", map[string]any{"file_path": filepath.Join(projDir, "a.go")}, nil)
	run := func(name string) {
		t.Helper()
		if out, err := execCerneStdin(t, stdin, "hook", "work-gate"); err != nil || out != "" {
			t.Fatalf("%s: %v %q, want silent exit 0", name, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(chDir, "lock"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("locked")
	if err := os.Remove(filepath.Join(chDir, "lock")); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(chDir, 0o000); err != nil {
		t.Fatal(err)
	}
	run("unreadable")
	if err := os.Chmod(chDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(chDir, "messages.jsonl"), []byte("garbage{\n\x00\x01"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("corrupt")
}

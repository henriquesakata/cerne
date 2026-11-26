package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

// Cursor runs the project's .claude hooks through its third-party import
// (F-0428). The fixtures in testdata/cursor are payloads from a real Cursor
// 3.25.6 session's hook log (user_email dropped, paths replaced by
// placeholders the tests fill in).

const cursorSess = "2cbcdf25-fae0-4ddd-abb5-8ce420078093"

// cursorFixture loads testdata/cursor/<name>, with __PROJ__ pointing at projDir
// and __CURSOR_HOME__ at a dir outside any project, as a mutable map.
func cursorFixture(t *testing.T, name, projDir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "cursor", name))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(raw), "__PROJ__", projDir)
	s = strings.ReplaceAll(s, "__CURSOR_HOME__", t.TempDir())
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func payloadString(t *testing.T, m map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// cursorShellOutput is Cursor's tool_output: the shell result as a JSON string.
func cursorShellOutput(t *testing.T, output string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"output": output, "exitCode": 0})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func boundTo(t *testing.T, repoDir, sess string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(repoDir), "sessions", sess))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// TestHookCursorBindsFeatureStartFromToolOutput (F-0428 crit. 1, 10): the
// logged `cerne push && cerne feature start F-0425` call — Shell tool, empty
// cwd, the result in tool_output — binds the session, through the plain
// `cerne hook bind-work` the .claude wrapper runs.
func TestHookCursorBindsFeatureStartFromToolOutput(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	p := cursorFixture(t, "post-tool-use-feature-start.json", projDir)
	if out, err := execCerneStdin(t, payloadString(t, p), "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}
	if got := boundTo(t, repoDir, cursorSess); got != "F-0425" {
		t.Fatalf("bound id = %q, want F-0425", got)
	}
}

// TestHookCursorWorkStartResolvesWorkspaceRoot (crit. 1b): a cwd outside the
// project (Cursor's own terminals dir) and an empty one both resolve to the
// workspace root, and the minted W- binds.
func TestHookCursorWorkStartResolvesWorkspaceRoot(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	for _, cwd := range []string{"outside", ""} {
		p := cursorFixture(t, "post-tool-use-terminals-cwd.json", projDir)
		sess := "sess-cwd-" + cwd
		p["session_id"], p["conversation_id"] = sess, sess
		if cwd == "" {
			p["cwd"] = ""
		}
		p["tool_input"] = map[string]any{"command": `cerne work start "Investigar erro X"`, "cwd": p["cwd"]}
		p["tool_output"] = cursorShellOutput(t, "W-0001 created (active) — Investigar erro X (actor: agent)\n")
		if out, err := execCerneStdin(t, payloadString(t, p), "hook", "bind-work"); err != nil {
			t.Fatalf("cwd %q: %v\noutput: %s", cwd, err, out)
		}
		if got := boundTo(t, repoDir, sess); got != "W-0001" {
			t.Fatalf("cwd %q: bound id = %q, want W-0001", cwd, got)
		}
	}
}

// TestHookCursorConversationIDAsSession (crit. 1c): no session_id → the
// conversation_id keys the binding.
func TestHookCursorConversationIDAsSession(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	p := cursorFixture(t, "post-tool-use-feature-start.json", projDir)
	delete(p, "session_id")
	p["conversation_id"] = "conv-only"
	if out, err := execCerneStdin(t, payloadString(t, p), "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}
	if got := boundTo(t, repoDir, "conv-only"); got != "F-0425" {
		t.Fatalf("bound id = %q, want F-0425", got)
	}
}

// TestHookCursorUnboundWarningEnvelope (crit. 2): the unbound-start warning
// reaches Cursor as additional_context, not Claude Code's hookSpecificOutput.
func TestHookCursorUnboundWarningEnvelope(t *testing.T) {
	projDir, _, _ := setupProject(t)
	p := cursorFixture(t, "post-tool-use-feature-start.json", projDir)
	p["tool_input"] = map[string]any{"command": `cerne work start "X" > /dev/null`, "cwd": ""}
	p["tool_output"] = cursorShellOutput(t, "")
	out, err := execCerneStdin(t, payloadString(t, p), "hook", "bind-work")
	if err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}
	assertCursorContext(t, out, "additional_context", "NOT bound")
}

// assertCursorContext: out is exactly one JSON object whose only key is key,
// carrying want, and nothing of Claude Code's envelope.
func assertCursorContext(t *testing.T, out, key, want string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out)
	}
	v, ok := m[key].(string)
	if len(m) != 1 || !ok || !strings.Contains(v, want) {
		t.Fatalf("want {%q: …%q…}, got:\n%s", key, want, out)
	}
}

// TestHookCursorProvenanceEnvelope (crit. 2): a push citing an orphan URL
// reminds Cursor through additional_context.
func TestHookCursorProvenanceEnvelope(t *testing.T) {
	projDir, _, _ := setupProject(t)
	writeAuthoredDoc(t, projDir, ".docs/current/features/F-0001-coisa/spec.md", "F-0001",
		"Baseado em https://exemplo.org/artigo-que-moldou.")
	p := cursorFixture(t, "post-tool-use-feature-start.json", projDir)
	p["tool_input"] = map[string]any{"command": "cerne push", "cwd": ""}
	p["tool_output"] = cursorShellOutput(t, "Sent: 1 document(s) (snapshot abc).\n")
	out, err := execCerneStdin(t, payloadString(t, p), "hook", "provenance")
	if err != nil {
		t.Fatalf("hook provenance: %v\noutput: %s", err, out)
	}
	assertCursorContext(t, out, "additional_context", "https://exemplo.org/artigo-que-moldou")
}

// TestHookCursorWorkGate (crit. 2): the logged Write payload (no cwd) is
// denied in Cursor's flat envelope while unbound, and passes silently once
// the session is bound.
func TestHookCursorWorkGate(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir)
	p := cursorFixture(t, "pre-tool-use-write.json", projDir)
	out, err := execCerneStdin(t, payloadString(t, p), "hook", "work-gate")
	if err != nil {
		t.Fatalf("hook work-gate: %v\noutput: %s", err, out)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(out), &d); err != nil || d["permission"] != "deny" || !strings.Contains(d["agent_message"].(string), "work start") || d["hookSpecificOutput"] != nil {
		t.Fatalf("want Cursor deny envelope, got:\n%s", out)
	}

	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), cursorSess, "W-0001"); err != nil {
		t.Fatal(err)
	}
	out, err = execCerneStdin(t, payloadString(t, p), "hook", "work-gate")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("bound session: want silence, got %v:\n%s", err, out)
	}
}

// TestHookCursorSessionContext (crit. 2): the sessionStart payload (no cwd)
// gets the rules and the live board as additional_context.
func TestHookCursorSessionContext(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	p := cursorFixture(t, "session-start.json", projDir)
	out, err := execCerneStdin(t, payloadString(t, p), "hook", "session-context")
	if err != nil {
		t.Fatalf("hook session-context: %v\noutput: %s", err, out)
	}
	assertCursorContext(t, out, "additional_context", "W-0001")
	assertCursorContext(t, out, "additional_context", "cerne feature new")
}

// TestHookCursorStopFollowup (crit. 3): a completed turn of a bound session
// gets the closeout as a followup_message; an aborted one gets nothing and
// does not spend the reminder; a follow-up's own stop (loop_count > 0) is
// silent.
func TestHookCursorStopFollowup(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), cursorSess, "W-0001"); err != nil {
		t.Fatal(err)
	}
	stop := func(status string, loop int) string {
		t.Helper()
		p := cursorFixture(t, "stop.json", projDir)
		p["status"], p["loop_count"] = status, loop
		out, err := execCerneStdin(t, payloadString(t, p), "hook", "stop-check")
		if err != nil {
			t.Fatalf("stop-check: %v\noutput: %s", err, out)
		}
		return out
	}
	if out := stop("completed", 1); strings.TrimSpace(out) != "" {
		t.Fatalf("loop_count 1: want silence, got:\n%s", out)
	}
	if out := stop("aborted", 0); strings.TrimSpace(out) != "" {
		t.Fatalf("aborted: want silence, got:\n%s", out)
	}
	assertCursorContext(t, stop("completed", 0), "followup_message", "W-0001")
	if out := stop("completed", 0); strings.TrimSpace(out) != "" {
		t.Fatalf("second completed stop: want the rationed silence, got:\n%s", out)
	}
}

// TestHookNonCursorPayloadWithCursorNamedKeys (crit. 4, critic finding 1): a
// payload without cursor_version that carries keys Cursor also uses, with
// other types, still binds — the Cursor fields never fail its decode.
func TestHookNonCursorPayloadWithCursorNamedKeys(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	stdin := strings.Replace(hookJSON(t, "sess-other", projDir, "Bash",
		map[string]any{"command": `cerne work start "Investigar erro X"`},
		map[string]any{"stdout": "W-0001 criada (active) — Investigar erro X (ator: agent)"}),
		"{", `{"status":{"code":0},"loop_count":"2","workspace_roots":{"a":1},"conversation_id":7,`, 1)
	if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}
	if got := boundTo(t, repoDir, "sess-other"); got != "W-0001" {
		t.Fatalf("bound id = %q, want W-0001", got)
	}
}

// TestCursorToolOutputStaysJSON (critic finding 2): a plain-text tool_output
// keeps ToolResponse valid JSON (the quoted string), a JSON one is unwrapped.
func TestCursorToolOutputStaysJSON(t *testing.T) {
	for _, tc := range []struct{ toolOutput, want string }{
		{`"{\"output\":\"W-0001 created (active)\"}"`, `{"output":"W-0001 created (active)"}`},
		{`"plain text, not JSON"`, `"plain text, not JSON"`},
		{`null`, `null`},
	} {
		in, err := decodeHookInput(strings.NewReader(`{"cursor_version":"3.25.6","tool_output":` + tc.toolOutput + `}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.toolOutput, err)
		}
		if got := string(in.ToolResponse); got != tc.want || !json.Valid(in.ToolResponse) {
			t.Errorf("tool_output %s: ToolResponse = %s, want %s", tc.toolOutput, got, tc.want)
		}
	}
}

// TestWorkspaceDir (crit. 1b, critic findings 3 and 4): a cwd inside a root —
// at its top, in a subdir, under the second root or through a symlink — is
// kept; an empty or outside cwd falls back to the first root.
func TestWorkspaceDir(t *testing.T) {
	a, b, out := t.TempDir(), t.TempDir(), t.TempDir()
	sub := filepath.Join(b, "ui", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(out, "link-to-b")
	if err := os.Symlink(b, link); err != nil {
		t.Fatal(err)
	}
	roots := []string{a, b + string(filepath.Separator)}
	for _, tc := range []struct{ cwd, want string }{
		{a, a},
		{sub, sub},
		{b, b},
		{filepath.Join(link, "ui"), filepath.Join(link, "ui")},
		{"", a},
		{out, a},
		{filepath.Join(a, "..", "elsewhere"), a},
	} {
		if got := workspaceDir(tc.cwd, roots); got != tc.want {
			t.Errorf("workspaceDir(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
	if got := workspaceDir("/x", nil); got != "/x" {
		t.Errorf("no roots: %q, want /x", got)
	}
}

// cursorRecord is the cursor-sessions record of sess in the project whose
// clone is repoDir; nil when there is none.
func cursorRecord(t *testing.T, repoDir, sess string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(repoDir), "cursor-sessions", sess+".json"))
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("record %s: %v\n%s", sess, err, raw)
	}
	return m
}

// TestHookCursorRecordsSessionModel (F-0513 crit. 1): every hook a Cursor
// payload reaches records the session's model — model_id (grok-4.7) over the
// mode variant in model (grok-4.7-high-fast) — with the time of the
// observation and, when the payload has one, the transcript path.
func TestHookCursorRecordsSessionModel(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for _, tc := range []struct{ fixture, hook, flag string }{
		{"session-start.json", "session-context", ""},
		{"post-tool-use-feature-start.json", "bind-work", ""},
		{"post-tool-use-feature-start.json", "tool-notes", "--after"},
		{"pre-tool-use-write.json", "work-gate", ""},
		{"stop.json", "stop-check", ""},
	} {
		p := cursorFixture(t, tc.fixture, projDir)
		sess := "c-" + tc.hook
		p["session_id"], p["conversation_id"] = sess, sess
		args := []string{"hook", tc.hook}
		if tc.flag != "" {
			args = append(args, tc.flag)
		}
		if out, err := execCerneStdin(t, payloadString(t, p), args...); err != nil {
			t.Fatalf("hook %s: %v\noutput: %s", tc.hook, err, out)
		}
		rec := cursorRecord(t, repoDir, sess)
		if rec["model"] != "grok-4.7" || rec["updated_at"] == nil {
			t.Errorf("%s: record = %v, want model grok-4.7 and updated_at", tc.hook, rec)
		}
		if want, _ := p["transcript_path"].(string); want != "" && rec["transcript_path"] != want {
			t.Errorf("%s: transcript_path = %v, want %s", tc.hook, rec["transcript_path"], want)
		}
	}
}

// TestHookCursorSessionRecordFailOpen (F-0513 crit. 1): a session id that
// could leave the directory records nothing, a record that cannot be written
// never breaks the hook (the session still binds), and a payload that is not
// Cursor's records nothing.
func TestHookCursorSessionRecordFailOpen(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)

	p := cursorFixture(t, "post-tool-use-feature-start.json", projDir)
	p["session_id"], p["conversation_id"] = "../evil", "../evil"
	if out, err := execCerneStdin(t, payloadString(t, p), "hook", "bind-work"); err != nil {
		t.Fatalf("unsafe id: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "cursor-sessions")); !os.IsNotExist(err) {
		t.Fatalf("unsafe id: cursor-sessions exists (%v), want nothing written", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "evil.json")); !os.IsNotExist(err) {
		t.Fatalf("unsafe id: evil.json written outside cursor-sessions")
	}

	p = cursorFixture(t, "post-tool-use-feature-start.json", projDir)
	delete(p, "cursor_version")
	p["session_id"], p["cwd"] = "claude-like", projDir // a Claude Code payload names its cwd
	if out, err := execCerneStdin(t, payloadString(t, p), "hook", "bind-work"); err != nil {
		t.Fatalf("no cursor_version: %v\noutput: %s", err, out)
	}
	if rec := cursorRecord(t, repoDir, "claude-like"); rec != nil {
		t.Fatalf("no cursor_version: record = %v, want none", rec)
	}

	if err := os.WriteFile(filepath.Join(cacheDir, "cursor-sessions"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	p = cursorFixture(t, "post-tool-use-feature-start.json", projDir)
	if out, err := execCerneStdin(t, payloadString(t, p), "hook", "bind-work"); err != nil {
		t.Fatalf("unwritable record: %v\noutput: %s", err, out)
	}
	if got := boundTo(t, repoDir, cursorSess); got != "F-0425" {
		t.Fatalf("unwritable record: bound id = %q, want F-0425 (the hook must go on)", got)
	}
}

// TestPushRecordsCursorAuthor (F-0513 crit. 2): the whole path — Cursor's
// sessionStart reaches the hooks, then its agent shell (CURSOR_AGENT=1, no
// CERNE_SESSION, nothing declared) pushes the spec: the write names cursor
// and grok-4.7. Once a second session on another model starts, the next push
// names no model.
func TestPushRecordsCursorAuthor(t *testing.T) {
	projDir, _, repoDir, specPath := pulledProject(t)
	for _, k := range []string{"CERNE_ACTOR", "CERNE_SESSION", "CERNE_PROVIDER", "CERNE_MODEL"} {
		t.Setenv(k, "")
	}
	t.Setenv("CURSOR_AGENT", "1")
	start := func(p map[string]any) {
		t.Helper()
		if out, err := execCerneStdin(t, payloadString(t, p), "hook", "session-context"); err != nil {
			t.Fatalf("hook session-context: %v\noutput: %s", err, out)
		}
	}
	push := func(body string) map[string]string {
		t.Helper()
		mustWrite(t, specPath, "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\n"+body+"\n")
		if out, err := execCerne(t, "push", "--dir", projDir); err != nil {
			t.Fatalf("push: %v\n%s", err, out)
		}
		evs, err := store.ReadEvents(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		var last map[string]string
		for _, ev := range evs {
			if ev.EntityID == "F-0001" && ev.Type == "feature.doc_edited" {
				last = ev.Payload
			}
		}
		return last
	}

	start(cursorFixture(t, "session-start.json", projDir))
	if p := push("Dividir compras em parcelas, no Cursor."); p["provider"] != "cursor" || p["model"] != "grok-4.7" || p["source"] != "hook" {
		t.Fatalf("one session: doc_edited = %v, want cursor/grok-4.7 from the hook", p)
	}
	other := cursorFixture(t, "session-start.json", projDir)
	other["session_id"], other["conversation_id"], other["model_id"] = "other-sess", "other-sess", "gpt-6"
	start(other)
	if p := push("Dividir compras em parcelas, em duas sessões."); p["provider"] != "cursor" || p["model"] != "" || p["source"] != "hook" {
		t.Fatalf("two sessions on two models: doc_edited = %v, want cursor with no model", p)
	}
}

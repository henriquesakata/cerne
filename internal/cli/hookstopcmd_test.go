package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func stopPayload(t *testing.T, sess, cwd string) string {
	t.Helper()
	return hookJSON(t, sess, cwd, "", nil, nil)
}

// TestHookStopCheckRemindsOpenWork (F-0171 crit. 1): a session bound to a live
// W- gets an end-of-turn additionalContext naming the id and the closeout
// commands.
func TestHookStopCheckRemindsOpenWork(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	cacheDir := filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, "sess-stop", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	out, err := execCerneStdin(t, stopPayload(t, "sess-stop", projDir), "hook", "stop-check")
	if err != nil {
		t.Fatalf("hook stop-check: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"additionalContext", `"Stop"`, "W-0001", "work done", "abandon"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestHookStopCheckRemindsFeatureProgress (F-0171 crit. 2): a session bound to
// an in-development F- is told to sync plan/result progress before stopping.
func TestHookStopCheckRemindsFeatureProgress(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	if err := store.SaveFeature(repoDir, store.Feature{
		ID: "F-0042", Title: "coisa", State: domain.StateInDevelopment,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	cacheDir := filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, "sess-stop-f", "F-0042"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	out, err := execCerneStdin(t, stopPayload(t, "sess-stop-f", projDir), "hook", "stop-check")
	if err != nil {
		t.Fatalf("hook stop-check: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"F-0042", "plan", "result", "cerne sync"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestHookStopCheckSilence (F-0171 crit. 3/6): no binding, a dead binding
// (self-heal), and a malformed payload all produce no output and no error.
func TestHookStopCheckSilence(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)

	// Dead binding: work exists but is done.
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	if err := store.SaveWork(repoDir, store.Work{
		ID: "W-0009", Title: "morta", State: domain.WorkDone, Actor: "a",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if err := app.BindSession(cacheDir, "sess-dead", "W-0009"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}

	for name, stdin := range map[string]string{
		"no binding":   stopPayload(t, "sess-none", projDir),
		"dead binding": stopPayload(t, "sess-dead", projDir),
		"not json":     "garbage{",
	} {
		out, err := execCerneStdin(t, stdin, "hook", "stop-check")
		if err != nil {
			t.Fatalf("%s: errored: %v\noutput: %s", name, err, out)
		}
		if strings.TrimSpace(out) != "" {
			t.Fatalf("%s: want silence, got:\n%s", name, out)
		}
	}
}

// TestHookStopCheckSilentWhenStopHookActive (F-0294 crit. 1): a Stop that is
// itself the continuation of a Stop hook carries stop_hook_active=true — the
// check must stay silent even with a live binding, or the reminder loops.
func TestHookStopCheckSilentWhenStopHookActive(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), "sess-loop", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	stdin := `{"session_id":"sess-loop","cwd":` + strconv.Quote(projDir) + `,"stop_hook_active":true}`
	out, err := execCerneStdin(t, stdin, "hook", "stop-check")
	if err != nil {
		t.Fatalf("hook stop-check: %v\noutput: %s", err, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stop_hook_active: want silence, got:\n%s", out)
	}
	// The silenced continuation must not consume the session's one reminder.
	out, err = execCerneStdin(t, stopPayload(t, "sess-loop", projDir), "hook", "stop-check")
	if err != nil {
		t.Fatalf("hook stop-check: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("first real stop after stop_hook_active: want W-0001 reminder, got:\n%s", out)
	}
}

// TestHookStopCheckRemindsOncePerBinding (F-0294 crit. 2-5): the reminder fires
// once per (session, binding) — repeated Stops stay silent, a new binding or a
// different session is reminded again.
func TestHookStopCheckRemindsOncePerBinding(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for _, title := range []string{"Investigar erro X", "Revisar deploy de sexta"} {
		if out, err := execCerne(t, "work", "start", title, "--dir", projDir); err != nil {
			t.Fatalf("work start %q: %v\noutput: %s", title, err, out)
		}
	}
	cacheDir := filepath.Dir(repoDir)
	bind := func(sess, id string) {
		t.Helper()
		if err := app.BindSession(cacheDir, sess, id); err != nil {
			t.Fatalf("BindSession: %v", err)
		}
	}
	stop := func(sess string) string {
		t.Helper()
		out, err := execCerneStdin(t, stopPayload(t, sess, projDir), "hook", "stop-check")
		if err != nil {
			t.Fatalf("hook stop-check: %v\noutput: %s", err, out)
		}
		return strings.TrimSpace(out)
	}

	bind("sess-a", "W-0001")
	if out := stop("sess-a"); !strings.Contains(out, "W-0001") {
		t.Fatalf("first stop: want W-0001 reminder, got:\n%s", out)
	}
	if out := stop("sess-a"); out != "" {
		t.Fatalf("repeat stop: want silence, got:\n%s", out)
	}

	bind("sess-b", "W-0001")
	if out := stop("sess-b"); !strings.Contains(out, "W-0001") {
		t.Fatalf("other session: want W-0001 reminder, got:\n%s", out)
	}

	bind("sess-a", "W-0002")
	if out := stop("sess-a"); !strings.Contains(out, "W-0002") {
		t.Fatalf("rebound session: want W-0002 reminder, got:\n%s", out)
	}
	if out := stop("sess-a"); out != "" {
		t.Fatalf("rebound repeat: want silence, got:\n%s", out)
	}
}

// TestHookStopCheckChannelReminderNonReentrant (F-0325 crit. 16, F-0294): an
// addressed channel message still unanswered when the turn ends is reminded
// once — never on the Stop's own continuation, never again next turn — a new
// one is reminded once more, and with the closeout reminder due in the same
// Stop both go out in one envelope (a single continuation).
func TestHookStopCheckChannelReminderNonReentrant(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	a := startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	stop := func(sess string, active bool) string {
		t.Helper()
		stdin := `{"session_id":"` + sess + `","cwd":` + strconv.Quote(projDir) + `,"stop_hook_active":` + strconv.FormatBool(active) + `}`
		out, err := execCerneStdin(t, stdin, "hook", "stop-check")
		if err != nil {
			t.Fatalf("stop-check: %v\n%s", err, out)
		}
		return out
	}
	if out := stop("sess-a", false); !strings.Contains(out, "work done") {
		t.Fatalf("closeout reminder (F-0171) missing:\n%s", out)
	}

	seedChannel(t, repoDir, b, a+" segura o rooms.go")
	out := stop("sess-a", false)
	if strings.Count(out, "\n") != 1 || strings.Contains(out, "work done") || strings.Contains(out, `"decision"`) {
		t.Fatalf("want one non-blocking channel-only envelope:\n%s", out)
	}
	c := channelEnvelope(t, out)
	for _, want := range []string{"#1", b, "cerne msg send", "cerne msg ack 1"} {
		if c.HookSpecificOutput.HookEventName != "Stop" || !strings.Contains(c.HookSpecificOutput.AdditionalContext, want) {
			t.Fatalf("reminder missing %q:\n%s", want, out)
		}
	}
	if out := stop("sess-a", true); out != "" {
		t.Fatalf("continuation reminded again: %q", out)
	}
	if out := stop("sess-a", false); out != "" {
		t.Fatalf("next turn reminded the same message again: %q", out)
	}

	seedChannel(t, repoDir, b, a+" mais uma")
	if out := stop("sess-a", false); !strings.Contains(out, "#2") || strings.Contains(out, "#1") {
		t.Fatalf("new addressed message: %q", out)
	}

	cw := startBoundWork(t, projDir, repoDir, "sess-c", "Atualizar a documentação")
	seedChannel(t, repoDir, b, cw+" pode consumir")
	out = stop("sess-c", false)
	if strings.Count(out, "hookSpecificOutput") != 1 || !strings.Contains(out, "work done") || !strings.Contains(out, "cerne msg ack 3") {
		t.Fatalf("want one envelope with both parts:\n%s", out)
	}
}

// TestHookStopCheckChannelFailOpen (crit. 19): an unreadable or corrupt channel
// never breaks the end of the turn, nor the closeout reminder.
func TestHookStopCheckChannelFailOpen(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	a := startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	seedChannel(t, repoDir, b, a+" segura")
	chDir := filepath.Join(filepath.Dir(repoDir), "rooms", "channel")
	if err := os.Chmod(chDir, 0o000); err != nil {
		t.Fatal(err)
	}
	out, err := execCerneStdin(t, stopPayload(t, "sess-a", projDir), "hook", "stop-check")
	if err := os.Chmod(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err != nil || !strings.Contains(out, "work done") || strings.Contains(out, "cerne msg") {
		t.Fatalf("unreadable channel: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(chDir, "messages.jsonl"), []byte("{garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, stopPayload(t, "sess-a", projDir), "hook", "stop-check"); err != nil || out != "" {
		t.Fatalf("corrupt channel: %v %q", err, out)
	}
}

// TestHookStopCheckHarvestsCost (F-0309 clarification): the Stop hook harvests
// the session's cost snapshot for its live feature, from the payload's
// transcript_path, and keeps its reminder intact; a transcript it cannot parse
// or read changes neither the output nor the exit (fail-open, N-0021).
func TestHookStopCheckHarvestsCost(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0042", Title: "coisa", State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Dir(repoDir)
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	ts := now.Add(time.Minute).Format(time.RFC3339Nano)
	stop := func(sess string, lines ...string) string {
		t.Helper()
		if err := app.BindSession(cacheDir, sess, "F-0042"); err != nil {
			t.Fatal(err)
		}
		writeTranscript(t, root, sess+".jsonl", lines...)
		main := filepath.Join(root, "projects", "-tmp-p", sess+".jsonl")
		payload := strings.Replace(stopPayload(t, sess, projDir), "{", `{"transcript_path":`+strconv.Quote(main)+`,`, 1)
		out, err := execCerneStdin(t, payload, "hook", "stop-check")
		if err != nil {
			t.Fatalf("%s: stop-check: %v\n%s", sess, err, out)
		}
		for _, want := range []string{"F-0042", "plan", "result", "cerne sync"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: reminder lost %q:\n%s", sess, want, out)
			}
		}
		if strings.Contains(out, "transcript") {
			t.Fatalf("%s: harvest leaked output:\n%s", sess, out)
		}
		return main
	}

	stop("sess-cost", costLine(ts, "m1", "claude-opus-5-5", "", 1, 2, 3, 4))
	if _, err := os.Stat(filepath.Join(cacheDir, "cost", "F-0042.json")); err != nil {
		t.Fatalf("no cost snapshot: %v", err)
	}

	stop("sess-broken", `{"type":"assistant","usage" broken`)
	if os.Geteuid() != 0 {
		main := stop("sess-locked") // bound and harvested once, then changed and made unreadable
		later := time.Now().Add(time.Hour)
		if err := os.Chtimes(main, later, later); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(main, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(main, 0o644) })
		payload := strings.Replace(stopPayload(t, "sess-locked", projDir), "{", `{"transcript_path":`+strconv.Quote(main)+`,`, 1)
		if out, err := execCerneStdin(t, payload, "hook", "stop-check"); err != nil || strings.Contains(out, "transcript") {
			t.Fatalf("unreadable transcript: err=%v\n%s", err, out)
		}
	}
}

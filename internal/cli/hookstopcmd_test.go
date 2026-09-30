package cli

import (
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

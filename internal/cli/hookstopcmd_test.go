package cli

import (
	"path/filepath"
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

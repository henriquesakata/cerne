package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
)

// signalFixture binds sess to W-0001 with a 3-day-old mtime.
func signalFixture(t *testing.T, sess string) (projDir, cacheDir, path string, old time.Time) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	cacheDir = filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, sess, "W-0001"); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(cacheDir, "sessions", sess)
	old = time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return
}

// TestHookSignalRefreshesBinding (F-0361 crit. 1): each of the four runtime
// hooks leaves the session's binding with a fresh mtime and the same content.
func TestHookSignalRefreshesBinding(t *testing.T) {
	cases := []struct {
		name string
		args []string
		tool string
		in   any
	}{
		{"bind-work", []string{"hook", "bind-work"}, "Bash", map[string]any{"command": "ls"}},
		{"work-gate", []string{"hook", "work-gate"}, "Edit", map[string]any{"file_path": "/x/y.go"}},
		{"stop-check", []string{"hook", "stop-check"}, "", nil},
		{"session-context", []string{"hook", "session-context"}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projDir, _, path, _ := signalFixture(t, "sess-sig")
			start := time.Now().Add(-2 * time.Second) // fs mtime granularity slack
			if out, err := execCerneStdin(t, hookJSON(t, "sess-sig", projDir, tc.tool, tc.in, nil), tc.args...); err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if mt := fi.ModTime(); mt.Before(start) || mt.After(time.Now()) {
				t.Fatalf("mtime %v not in [%v, now]", mt, start)
			}
			if raw, _ := os.ReadFile(path); string(raw) != "W-0001\n" {
				t.Fatalf("content changed: %q", raw)
			}
		})
	}
}

// TestHookSignalNoBindingCreatesNothing (F-0361 crit. 2).
func TestHookSignalNoBindingCreatesNothing(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)
	if err := os.RemoveAll(filepath.Join(cacheDir, "sessions")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"hook", "bind-work"}, {"hook", "work-gate"}, {"hook", "stop-check"}, {"hook", "session-context"}} {
		if _, err := execCerneStdin(t, hookJSON(t, "sess-none", projDir, "Bash", map[string]any{"command": "ls"}, nil), args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("sessions dir created: %v", err)
	}
}

// TestHookStopCheckSignalStaysLocal (F-0361 crit. 7): with git and ssh on PATH
// replaced by failing fakes, stop-check still runs and refreshes the signal.
func TestHookStopCheckSignalStaysLocal(t *testing.T) {
	projDir, _, path, _ := signalFixture(t, "sess-local")
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	for _, n := range []string{"git", "ssh"} {
		script := "#!/bin/sh\necho " + n + " >> " + marker + "\nexit 1\n"
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now().Add(-2 * time.Second)
	if out, err := execCerneStdin(t, stopPayload(t, "sess-local", projDir), "hook", "stop-check"); err != nil {
		t.Fatalf("stop-check: %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("stop-check invoked git/ssh")
	}
	if fi, err := os.Stat(path); err != nil || fi.ModTime().Before(start) || fi.ModTime().After(time.Now()) {
		t.Fatalf("signal not refreshed within window: %v %v", fi, err)
	}
}

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"
)

// freePort grabs an ephemeral port for a supervise test. The tiny window
// between Close and the supervisor's bind is acceptable in tests.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// fakeResolver resolves any dir to itself, with the dir's base name as
// identity — no backend involved (same spirit as the internal/server tests).
func fakeResolver(dir string) (repoDir, projDir, projectID, name string, err error) {
	return dir, dir, filepath.Base(dir), filepath.Base(dir), nil
}

// listPIDs fetches the registered project ids from the host on base, sorted.
func listPIDs(base string) ([]string, error) {
	r, err := probeClient.Get(base + "/api/projects")
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	var body struct {
		Projects []struct {
			PID string `json:"pid"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	pids := make([]string, 0, len(body.Projects))
	for _, p := range body.Projects {
		pids = append(pids, p.PID)
	}
	sort.Strings(pids)
	return pids, nil
}

// waitPIDs polls the host until exactly want (as a set) is registered or the
// deadline passes.
func waitPIDs(t *testing.T, base string, timeout time.Duration, want ...string) {
	t.Helper()
	sort.Strings(want)
	deadline := time.Now().Add(timeout)
	var last []string
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = listPIDs(base)
		if lastErr == nil && slices.Equal(last, want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("projects = %v (err %v), want %v", last, lastErr, want)
}

// startSupervised runs superviseUI in a goroutine, returning its cancel and
// exit channel.
func startSupervised(t *testing.T, dir string, port int) (context.CancelFunc, chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- superviseUI(ctx, io.Discard, dir, port, true, 0, fakeResolver)
	}()
	return cancel, done
}

// waitExit asserts the supervised process left cleanly.
func waitExit(t *testing.T, done chan error, who string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s exited with error: %v", who, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not exit", who)
	}
}

func superviseDirs(t *testing.T, names ...string) []string {
	t.Helper()
	root := t.TempDir()
	dirs := make([]string, len(names))
	for i, n := range names {
		dirs[i] = filepath.Join(root, n)
		if err := os.MkdirAll(dirs[i], 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dirs
}

// TestSuperviseTakeover: host + 1 secondary; the host dies; the survivor must
// take the port over and serve only its own project (spec F-0119 criteria 1,
// 3, 5).
func TestSuperviseTakeover(t *testing.T) {
	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	dirs := superviseDirs(t, "proj-a", "proj-b")

	cancelA, doneA := startSupervised(t, dirs[0], port)
	defer cancelA()
	waitPIDs(t, base, 5*time.Second, "proj-a")

	cancelB, doneB := startSupervised(t, dirs[1], port)
	defer cancelB()
	waitPIDs(t, base, 5*time.Second, "proj-a", "proj-b")

	cancelA() // the host dies
	waitExit(t, doneA, "host")

	// Criteria 1+3: within 5s the survivor answers, with the dead host's
	// project gone and its own re-registered.
	waitPIDs(t, base, 5*time.Second, "proj-b")

	// Criterion 5: the takeover persisted nothing.
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("%s gained files during takeover: %v", d, entries)
		}
	}

	cancelB()
	waitExit(t, doneB, "survivor")
}

// TestSuperviseRace: two secondaries race for the port when the host dies;
// exactly one wins, the other re-registers, nobody errors (criteria 2 and 6).
func TestSuperviseRace(t *testing.T) {
	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	dirs := superviseDirs(t, "proj-a", "proj-b", "proj-c")

	cancelA, doneA := startSupervised(t, dirs[0], port)
	defer cancelA()
	waitPIDs(t, base, 5*time.Second, "proj-a")

	cancelB, doneB := startSupervised(t, dirs[1], port)
	defer cancelB()
	waitPIDs(t, base, 5*time.Second, "proj-a", "proj-b")

	cancelC, doneC := startSupervised(t, dirs[2], port)
	defer cancelC()
	waitPIDs(t, base, 5*time.Second, "proj-a", "proj-b", "proj-c")

	cancelA() // the host dies; B and C race
	waitExit(t, doneA, "host")

	// Both survivors present: one won the bind, the other re-registered with it.
	waitPIDs(t, base, 5*time.Second, "proj-b", "proj-c")

	cancelC()
	waitExit(t, doneC, "second survivor")
	cancelB()
	waitExit(t, doneB, "first survivor")
}

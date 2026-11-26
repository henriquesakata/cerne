package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/version"
)

// installFake puts a copy of this test binary at path, answering
// `ui --identity` with id (TestMain); an empty id makes a binary that does
// not run. The mtime moves forward so the file reads as another one even when
// the copy has the same size.
func installFake(t *testing.T, path, id string, mtime time.Time) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".identity", []byte(id), 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func identityJSON(v, ui string) string {
	b, _ := json.Marshal(Identity{Version: v, UI: ui})
	return string(b)
}

// TestBinaryWatch (F-0478 criteria 1 and 2): while the command's file is the
// one that started the panel, nothing is announced and installed is the
// running pair; once it is another file, one "projects" event goes out and
// installed carries the identity the new binary reports.
func TestBinaryWatch(t *testing.T) {
	t.Setenv("CERNE_TEST_FAKE_IDENTITY", "1")
	cmd := filepath.Join(t.TempDir(), "cerne")
	t0 := time.Now().Add(-time.Hour)
	installFake(t, cmd, identityJSON(version.Version, UIIdentity()), t0)

	hub := newMultiHub(nil)
	sub := hub.subscribe()
	w := newBinaryWatch(UIHost{Command: cmd}, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx, 10*time.Millisecond, func() { hub.broadcast(liveEvent{kind: "projects"}) })

	select {
	case ev := <-sub:
		t.Fatalf("event %+v with the starting binary still on disk", ev)
	case <-time.After(100 * time.Millisecond):
	}
	if got, want := w.installed(), (Identity{Version: version.Version, UI: UIIdentity()}); got != want {
		t.Fatalf("installed = %+v, want the running pair %+v", got, want)
	}

	installFake(t, cmd, identityJSON("v9.9.9", "newui"), t0.Add(time.Minute))
	select {
	case ev := <-sub:
		if ev.kind != "projects" {
			t.Fatalf("event kind %q, want projects", ev.kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event after the binary on disk changed")
	}
	if got := w.installed(); got.Version != "v9.9.9" || got.UI != "newui" {
		t.Fatalf("installed = %+v, want v9.9.9/newui", got)
	}
	select {
	case ev := <-sub:
		t.Fatalf("second event %+v for the same file", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestRestartRoute (F-0478 criteria 1, 2 and 6): GET /api/projects carries
// installed; POST /api/restart refuses while the disk holds the running
// binary (409) and when the binary on disk does not run (422), and calls the
// host's Restart once for a binary that runs.
func TestRestartRoute(t *testing.T) {
	t.Setenv("CERNE_TEST_FAKE_IDENTITY", "1")
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(string) (string, string, string, string, error) {
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	cmd := filepath.Join(t.TempDir(), "cerne")
	t0 := time.Now().Add(-time.Hour)
	installFake(t, cmd, identityJSON(version.Version, UIIdentity()), t0)
	restarts := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewUIMultiHost(ctx, resolve, 0, "A", UIHost{Command: cmd, Restart: func() { restarts <- struct{}{} }})
	if err != nil {
		t.Fatalf("NewUIMultiHost: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	var list struct {
		Version   string    `json:"version"`
		UI        string    `json:"ui"`
		Installed *Identity `json:"installed"`
	}
	getJSON(t, srv.URL+"/api/projects", &list)
	if list.Installed == nil || list.Installed.Version != list.Version || list.Installed.UI != list.UI {
		t.Fatalf("installed = %+v, want the running pair %s/%s", list.Installed, list.Version, list.UI)
	}

	restart := func() (int, string) {
		r, err := http.Post(srv.URL+"/api/restart", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var b struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		return r.StatusCode, b.Error
	}
	if code, _ := restart(); code != http.StatusConflict {
		t.Fatalf("restart with the running binary on disk: %d, want 409", code)
	}

	installFake(t, cmd, "", t0.Add(time.Minute)) // does not run
	if code, msg := restart(); code != http.StatusUnprocessableEntity || !strings.Contains(msg, "does not run") {
		t.Fatalf("restart into a binary that does not run: %d %q, want 422 naming it", code, msg)
	}
	select {
	case <-restarts:
		t.Fatal("Restart called for a binary that does not run")
	case <-time.After(50 * time.Millisecond):
	}

	installFake(t, cmd, identityJSON("v9.9.9", "newui"), t0.Add(2*time.Minute))
	if code, _ := restart(); code != http.StatusAccepted {
		t.Fatalf("restart into a binary that runs: %d, want 202", code)
	}
	select {
	case <-restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("Restart not called")
	}
	getJSON(t, srv.URL+"/api/projects", &list)
	if list.Installed == nil || list.Installed.Version != "v9.9.9" {
		t.Fatalf("installed after the pre-check = %+v, want v9.9.9", list.Installed)
	}
}

// TestPanelJoinAndRestartLandWithoutOrigin (B-0091): the two writes another
// `cerne ui` sends to a running panel with no Origin — registering its
// project (registerProject) and asking the panel to restart
// (requestPanelRestart) — still land through the real router.
func TestPanelJoinAndRestartLandWithoutOrigin(t *testing.T) {
	t.Setenv("CERNE_TEST_FAKE_IDENTITY", "1")
	repoA, projA, _ := newGitBackendNoClean(t)
	repoB, projB, _ := newGitBackendNoClean(t)
	resolve := func(dir string) (string, string, string, string, error) {
		if dir == "B" {
			return repoB, projB, "proj-b", "Projeto B", nil
		}
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	cmd := filepath.Join(t.TempDir(), "cerne")
	t0 := time.Now().Add(-time.Hour)
	installFake(t, cmd, identityJSON(version.Version, UIIdentity()), t0)
	restarts := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewUIMultiHost(ctx, resolve, 0, "A", UIHost{Command: cmd, Restart: func() { restarts <- struct{}{} }})
	if err != nil {
		t.Fatalf("NewUIMultiHost: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	post := func(path, body string) int {
		r, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := post("/api/projects", `{"dir":"B"}`); code != http.StatusOK {
		t.Fatalf("POST /api/projects with no Origin = %d, want 200", code)
	}
	installFake(t, cmd, identityJSON("v9.9.9", "newui"), t0.Add(time.Minute))
	if code := post("/api/restart", ""); code != http.StatusAccepted {
		t.Fatalf("POST /api/restart with no Origin = %d, want 202", code)
	}
	select {
	case <-restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("Restart not called")
	}
}

// TestRestartRouteWithoutHost: a panel built without a host (tests, the
// zero UIHost) has no watcher and no restart.
func TestRestartRouteWithoutHost(t *testing.T) {
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(string) (string, string, string, string, error) {
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	var list map[string]any
	getJSON(t, srv.URL+"/api/projects", &list)
	if _, ok := list["installed"]; ok {
		t.Fatalf("installed present without a host: %v", list["installed"])
	}
	r, err := http.Post(srv.URL+"/api/restart", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("restart without a host: %d, want 409", r.StatusCode)
	}
}

// Critic 1/2 (F-0478): the running binary is the file the process started
// from, stamped at process start — not the file on disk when the server is
// built. An old attached process that takes the port over after an upgrade
// already landed on disk must still see the new binary.
func TestBinaryWatchStartsFromProcessStamp(t *testing.T) {
	t.Setenv("CERNE_TEST_FAKE_IDENTITY", "1")
	cmd := filepath.Join(t.TempDir(), "cerne")
	t0 := time.Now().Add(-time.Hour)
	installFake(t, cmd, identityJSON(version.Version, UIIdentity()), t0)
	started := StampCommand(cmd) // the process started from this file
	installFake(t, cmd, identityJSON("v9.9.9", "newui"), t0.Add(time.Minute))

	hub := newMultiHub(nil)
	sub := hub.subscribe()
	w := newBinaryWatch(UIHost{Command: cmd, Start: started}, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx, 10*time.Millisecond, func() { hub.broadcast(liveEvent{kind: "projects"}) })
	select {
	case <-sub:
	case <-time.After(5 * time.Second):
		t.Fatal("the binary upgraded before the server was built went unnoticed")
	}
	if got := w.installed(); got.Version != "v9.9.9" || !got.Changed {
		t.Fatalf("installed = %+v, want v9.9.9 changed", got)
	}
}

// Critic 3 (F-0478 criterion 1): a new local build may report the same
// version and UI; the file is what changed, and that is announced too.
func TestBinaryWatchSameIdentityOtherFile(t *testing.T) {
	t.Setenv("CERNE_TEST_FAKE_IDENTITY", "1")
	cmd := filepath.Join(t.TempDir(), "cerne")
	t0 := time.Now().Add(-time.Hour)
	same := identityJSON(version.Version, UIIdentity())
	installFake(t, cmd, same, t0)
	hub := newMultiHub(nil)
	sub := hub.subscribe()
	w := newBinaryWatch(UIHost{Command: cmd}, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx, 10*time.Millisecond, func() { hub.broadcast(liveEvent{kind: "projects"}) })

	installFake(t, cmd, same, t0.Add(time.Minute))
	select {
	case <-sub:
	case <-time.After(5 * time.Second):
		t.Fatal("a rebuilt binary with the same identity went unnoticed")
	}
	if got := w.installed(); !got.Changed || got.Version != version.Version {
		t.Fatalf("installed = %+v, want the same identity marked changed", got)
	}
}

// Critic 7: a second restart request while one is under way is accepted
// again (another tab, a racing joiner) and restarts nothing more.
func TestRestartRouteIsIdempotent(t *testing.T) {
	t.Setenv("CERNE_TEST_FAKE_IDENTITY", "1")
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(string) (string, string, string, string, error) {
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	cmd := filepath.Join(t.TempDir(), "cerne")
	t0 := time.Now().Add(-time.Hour)
	installFake(t, cmd, identityJSON(version.Version, UIIdentity()), t0)
	restarts := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewUIMultiHost(ctx, resolve, 0, "A", UIHost{Command: cmd, Restart: func() { restarts <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	installFake(t, cmd, identityJSON("v9.9.9", "newui"), t0.Add(time.Minute))
	for i := 0; i < 2; i++ {
		r, err := http.Post(srv.URL+"/api/restart", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusAccepted {
			t.Fatalf("restart request %d: %d, want 202", i+1, r.StatusCode)
		}
	}
	<-restarts
	select {
	case <-restarts:
		t.Fatal("the second request restarted again")
	case <-time.After(100 * time.Millisecond):
	}
}

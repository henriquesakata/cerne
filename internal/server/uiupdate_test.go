package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/version"
)

// fakeReleases answers the /releases/latest redirect with tag (an error
// status when tag is empty) and counts the requests.
func fakeReleases(t *testing.T, tag string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			hits.Add(1)
			if tag == "" {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
		case "/releases/tag/" + tag:
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CERNE_BASE_URL", srv.URL)
	return &hits
}

// updatePanel builds a panel host whose command is a fake binary, with the
// running version ver, the plan and the swap stubbed.
func updatePanel(t *testing.T, ver string, plan func(string) (bool, string, string), swap func(io.Writer, string) BinaryOutcome) (*httptest.Server, chan struct{}, string) {
	t.Helper()
	t.Setenv("CERNE_TEST_FAKE_IDENTITY", "1")
	prevVer, prevPlan, prevSwap := version.Version, PlanBinary, ReplaceBinary
	version.Version, PlanBinary, ReplaceBinary = ver, plan, swap
	t.Cleanup(func() { version.Version, PlanBinary, ReplaceBinary = prevVer, prevPlan, prevSwap })
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(string) (string, string, string, string, error) {
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	cmd := filepath.Join(t.TempDir(), "cerne")
	installFake(t, cmd, identityJSON(ver, UIIdentity()), time.Now().Add(-time.Hour))
	restarts := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h, err := NewUIMultiHost(ctx, resolve, 0, "A", UIHost{Command: cmd, Restart: func() { restarts <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, restarts, cmd
}

func swappable(string) (bool, string, string) { return true, "", "" }

// pagePost is http.Post as the panel's page sends it: with its Origin, which
// every local ui write needs (B-0091).
func pagePost(base, path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", base)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

type updateBody struct {
	Current   string         `json:"current"`
	Latest    string         `json:"latest"`
	Newer     bool           `json:"newer"`
	Swappable bool           `json:"swappable"`
	Fix       string         `json:"fix"`
	Running   bool           `json:"running"`
	Error     string         `json:"error"`
	Last      *BinaryOutcome `json:"last"`
}

func getUpdate(t *testing.T, base string) updateBody {
	t.Helper()
	var b updateBody
	getJSON(t, base+"/api/update", &b)
	return b
}

func waitUpdate(t *testing.T, base string, ok func(updateBody) bool) updateBody {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var b updateBody
	for time.Now().Before(deadline) {
		if b = getUpdate(t, base); ok(b) {
			return b
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("update state never matched: %+v", b)
	return b
}

func withCheckInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := updateCheckInterval
	updateCheckInterval = d
	t.Cleanup(func() { updateCheckInterval = prev })
}

// F-0480 criterion 1: one check when the panel starts, the next only after
// the interval, and none at all with ui.update_check = false on the host.
func TestUpdateCheckCadence(t *testing.T) {
	withCheckInterval(t, 400*time.Millisecond)
	hits := fakeReleases(t, "v0.15.0")
	srv, _, _ := updatePanel(t, "v0.14.3", swappable, nil)
	waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Latest == "v0.15.0" })
	time.Sleep(150 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d checks before the interval, want 1", n)
	}
	deadline := time.Now().Add(3 * time.Second)
	for hits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("%d checks after one interval, want 2", n)
	}
}

func TestUpdateCheckOff(t *testing.T) {
	withCheckInterval(t, 50*time.Millisecond)
	host := filepath.Join(t.TempDir(), "cerne")
	if err := os.MkdirAll(host, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, "settings.toml"), []byte("[ui]\nupdate_check = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_CONFIG_DIR", host)
	hits := fakeReleases(t, "v0.15.0")
	srv, _, _ := updatePanel(t, "v0.14.3", swappable, nil)
	time.Sleep(300 * time.Millisecond)
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d checks with ui.update_check = false, want none", n)
	}
	if b := getUpdate(t, srv.URL); b.Newer {
		t.Fatalf("newer with the check off: %+v", b)
	}
}

// F-0480 criteria 2 and 6: newer only for a release above the installed
// binary; nothing when the check failed; a binary that cannot be replaced
// from inside carries the command instead.
func TestUpdateState(t *testing.T) {
	cases := []struct {
		name, tag string
		newer     bool
		failed    bool
	}{
		{"newer release", "v0.15.0", true, false},
		{"same release", "v0.14.3", false, false},
		{"older release", "v0.14.0", false, false},
		{"check failed", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits := fakeReleases(t, c.tag)
			srv, _, _ := updatePanel(t, "v0.14.3", swappable, nil)
			waitUpdate(t, srv.URL, func(updateBody) bool { return hits.Load() > 0 })
			b := waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Latest != "" || b.Error != "" })
			if b.Newer != c.newer || (b.Error != "") != c.failed || b.Current != "v0.14.3" || !b.Swappable {
				t.Fatalf("state = %+v, want newer %v failed %v", b, c.newer, c.failed)
			}
		})
	}
	t.Run("cannot replace", func(t *testing.T) {
		fakeReleases(t, "v0.15.0")
		plan := func(string) (bool, string, string) { return false, "this is a local build", "go build ./cmd/cerne" }
		srv, _, _ := updatePanel(t, "v0.14.3", plan, nil)
		b := waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Latest != "" })
		if !b.Newer || b.Swappable || b.Fix != "go build ./cmd/cerne" {
			t.Fatalf("state = %+v, want newer, not swappable, with the command", b)
		}
	})
}

// F-0480 criteria 3, 4 and 5: the swap's lines go out on the stream as
// "update" events; a swap that lands restarts the panel into the new binary,
// one that fails keeps it and leaves the reason and the command; a second
// click while one runs is refused.
func TestUpdatePost(t *testing.T) {
	fakeReleases(t, "v0.15.0")
	var mu sync.Mutex
	outcome := BinaryOutcome{State: "updated", Detail: "v0.14.3 → v0.15.0"}
	release := make(chan struct{})
	swap := func(w io.Writer, exe string) BinaryOutcome {
		fmt.Fprintln(w, "downloading v0.15.0")
		<-release
		mu.Lock()
		defer mu.Unlock()
		if outcome.State == "updated" {
			installFake(t, exe, identityJSON("v0.15.0", "newui"), time.Now())
			outcome.NewExe = exe
		}
		return outcome
	}
	srv, restarts, _ := updatePanel(t, "v0.14.3", swappable, swap)
	waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Newer })

	stream, err := http.Get(srv.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	lines := make(chan string, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stream.Body.Read(buf)
			if n > 0 {
				lines <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	post := func() int {
		r, err := pagePost(srv.URL, "/api/update")
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := post(); code != http.StatusAccepted {
		t.Fatalf("update: %d, want 202", code)
	}
	if code := post(); code != http.StatusConflict {
		t.Fatalf("second update while one runs: %d, want 409", code)
	}
	got := ""
	deadline := time.After(5 * time.Second)
	// The panel's own line first (a script install prints nothing by itself),
	// then the swap's.
	for !strings.Contains(got, "event: update\ndata: installing v0.15.0 over v0.14.3\n") ||
		!strings.Contains(got, "event: update\ndata: downloading v0.15.0\n") {
		select {
		case s := <-lines:
			got += s
		case <-deadline:
			t.Fatalf("no update event with the swap's line; stream:\n%s", got)
		}
	}
	close(release)
	select {
	case <-restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("a swap that landed did not restart the panel")
	}
	b := waitUpdate(t, srv.URL, func(b updateBody) bool { return !b.Running && b.Last != nil })
	if b.Last.State != "updated" {
		t.Fatalf("last = %+v, want updated", b.Last)
	}
}

func TestUpdatePostFailureKeepsPanel(t *testing.T) {
	fakeReleases(t, "v0.15.0")
	swap := func(w io.Writer, exe string) BinaryOutcome {
		return BinaryOutcome{State: "not done", Detail: "checksum mismatch; the old executable is untouched", Fix: "curl -fsSL https://raw.githubusercontent.com/henriquesakata/cerne/main/install.sh | sh"}
	}
	srv, restarts, _ := updatePanel(t, "v0.14.3", swappable, swap)
	waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Newer })
	r, err := pagePost(srv.URL, "/api/update")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	b := waitUpdate(t, srv.URL, func(b updateBody) bool { return !b.Running && b.Last != nil })
	if b.Last.State != "not done" || b.Last.Fix == "" || !b.Newer {
		t.Fatalf("state = %+v, want not done with the command, still newer", b)
	}
	select {
	case <-restarts:
		t.Fatal("a failed swap restarted the panel")
	case <-time.After(100 * time.Millisecond):
	}
}

// Critic 9 (F-0480 criterion 6): a local build (dev) counts as older than any
// release, so the panel shows the new version with the command.
func TestUpdateStateDevBuild(t *testing.T) {
	fakeReleases(t, "v0.15.0")
	plan := func(string) (bool, string, string) {
		return false, "this is a local build (version dev)", "go build ./cmd/cerne"
	}
	srv, _, _ := updatePanel(t, "dev", plan, nil)
	b := waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Latest != "" })
	if !b.Newer || b.Swappable || b.Fix == "" {
		t.Fatalf("dev build state = %+v, want newer with the command", b)
	}
}

// Critic 7 (F-0480 criterion 1): turning ui.update_check off hides the
// button at once, without waiting for the next tick.
func TestUpdateStateFollowsTheSettingNow(t *testing.T) {
	host := filepath.Join(t.TempDir(), "cerne")
	if err := os.MkdirAll(host, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_CONFIG_DIR", host)
	fakeReleases(t, "v0.15.0")
	srv, _, _ := updatePanel(t, "v0.14.3", swappable, nil)
	waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Newer })
	if err := os.WriteFile(filepath.Join(host, "settings.toml"), []byte("[ui]\nupdate_check = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b := getUpdate(t, srv.URL); b.Newer || b.Latest != "" {
		t.Fatalf("state with the check just turned off = %+v, want nothing", b)
	}
}

// Critic 3: a swap that landed but whose restart was refused says so.
func TestUpdatePostRestartRefused(t *testing.T) {
	fakeReleases(t, "v0.15.0")
	swap := func(w io.Writer, exe string) BinaryOutcome {
		return BinaryOutcome{State: "updated", Detail: "v0.14.3 → v0.15.0"} // the file did not change: nothing to restart into
	}
	srv, _, _ := updatePanel(t, "v0.14.3", swappable, swap)
	waitUpdate(t, srv.URL, func(b updateBody) bool { return b.Newer })
	r, err := pagePost(srv.URL, "/api/update")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	b := waitUpdate(t, srv.URL, func(b updateBody) bool { return !b.Running && b.Last != nil })
	if b.Last.RestartError == "" {
		t.Fatalf("last = %+v, want the restart's refusal", b.Last)
	}
}

// Critic 6: a carriage return inside the swap's output becomes its own line
// (an SSE data field must hold no line break), and a last line without a
// newline is still sent.
func TestEventLinesSplitAndFlush(t *testing.T) {
	var got []string
	e := &eventLines{announce: func(ev liveEvent) { got = append(got, ev.data) }}
	fmt.Fprint(e, "==> Downloading\r###  10%\r### 100%\nPouring cerne")
	e.Flush()
	want := []string{"==> Downloading", "###  10%", "### 100%", "Pouring cerne"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

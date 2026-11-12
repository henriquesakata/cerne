package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/server"
	"github.com/henriquesakata/cerne/internal/version"
)

// same is what a host of this very build reports.
var same = hostID{version.Version, server.UIIdentity()}

type hostID struct{ version, ui string }

// syncBuf is a goroutine-safe output sink (joinUI writes while the test polls).
type syncBuf struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

// fakeHost is a stand-in cerne ui: it answers the probe with hostVersion
// (each field omitted when empty — a panel from before F-0373), registers any dir as
// project "proj-x", and holds /api/stream open like the real host.
func fakeHost(t *testing.T, id hostID) (base string, port int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"projects": []any{}, "default": ""}
		if id.version != "" {
			body["version"] = id.version
		}
		if id.ui != "" {
			body["ui"] = id.ui
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"pid": "proj x/é", "name": "Proj X"})
	})
	mux.HandleFunc("DELETE /api/projects/{pid}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]bool{"removed": true})
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ = strconv.Atoi(u.Port())
	return srv.URL, port
}

// runJoin joins the fake host, waits until joinUI printed its registration
// line (added or reconnected), leaves, and returns the output plus the URLs
// the fake opener was asked to open.
func runJoin(t *testing.T, port int, firstJoin, noBrowser bool) (string, []string) {
	t.Helper()
	return runJoinWarned(t, port, firstJoin, noBrowser, map[string]bool{})
}

// runJoinWarned is runJoin with the per-process warned set supplied, to test
// warn-once across re-joins.
func runJoinWarned(t *testing.T, port int, firstJoin, noBrowser bool, warned map[string]bool) (string, []string) {
	t.Helper()
	var mu sync.Mutex
	var opened []string
	prev := openURL
	openURL = func(u string) { mu.Lock(); opened = append(opened, u); mu.Unlock() }
	t.Cleanup(func() { openURL = prev })

	out := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := joinUI(ctx, out, t.TempDir(), port, firstJoin, noBrowser, warned)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	// The registration line is the last thing joinUI prints before it starts
	// monitoring, and the warning (when due) comes before it returns there.
	for !strings.Contains(out.String(), "Proj X") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("joinUI: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return out.String(), append([]string(nil), opened...)
}

const wantLink = "/?project=proj+x%2F%C3%A9"

// criterion 1: the project's own link (pid escaped), not the bare base.
func TestJoinPrintsProjectLink(t *testing.T) {
	base, port := fakeHost(t, same)
	out, _ := runJoin(t, port, true, true)
	if !strings.Contains(out, base+wantLink) {
		t.Fatalf("output lacks the project link %q:\n%s", base+wantLink, out)
	}
}

// criterion 2: opens that link on the first join without --no-browser.
func TestJoinOpensProjectLinkInBrowser(t *testing.T) {
	base, port := fakeHost(t, same)
	_, opened := runJoin(t, port, true, false)
	if len(opened) != 1 || opened[0] != base+wantLink {
		t.Fatalf("opened = %v, want exactly the project link", opened)
	}
}

func TestJoinNoBrowserFlagDoesNotOpen(t *testing.T) {
	_, port := fakeHost(t, same)
	if _, opened := runJoin(t, port, true, true); len(opened) != 0 {
		t.Fatalf("opened = %v with --no-browser, want none", opened)
	}
}

// criterion 7: a re-join after a takeover never reopens the browser, but the
// reconnected line still carries the project's link.
func TestJoinRejoinDoesNotReopenBrowser(t *testing.T) {
	base, port := fakeHost(t, same)
	out, opened := runJoin(t, port, false, false)
	if len(opened) != 0 {
		t.Fatalf("opened = %v on re-join, want none", opened)
	}
	if !strings.Contains(out, "reconnected") || !strings.Contains(out, base+wantLink) {
		t.Fatalf("reconnected line lacks the link:\n%s", out)
	}
}

func warnLine(base string, hv, hu, mine, myUI string) string {
	return fmt.Sprintf("warning: the cerne ui on %s is version %s (ui %s), this cerne is %s (ui %s); stop that ui (it serves its own embedded UI) and run cerne ui again", base, hv, hu, mine, myUI)
}

// criterion 3: another patch of the same MAJOR.MINOR is another build (N-0022).
func TestJoinWarnsOnDifferentPatchVersion(t *testing.T) {
	base, port := fakeHost(t, hostID{"v0.4.9", server.UIIdentity()})
	out, _ := runJoin(t, port, true, true)
	want := warnLine(base, "v0.4.9", server.UIIdentity(), version.Version, server.UIIdentity())
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
}

// Same version string, another embedded UI (a go install dev build): warns.
func TestJoinWarnsOnSameVersionDifferentUI(t *testing.T) {
	base, port := fakeHost(t, hostID{version.Version, "deadbeef0000"})
	out, _ := runJoin(t, port, true, true)
	want := warnLine(base, version.Version, "deadbeef0000", version.Version, server.UIIdentity())
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
}

// criterion 4: identical build, silent.
func TestJoinSameBuildIsSilent(t *testing.T) {
	_, port := fakeHost(t, same)
	out, _ := runJoin(t, port, true, true)
	if strings.Contains(out, "warning") {
		t.Fatalf("unexpected warning:\n%s", out)
	}
}

// criterion 5: a panel with no identity (older than F-0373) is a different
// one; it also ignores ?project=, so the BASE url is printed and opened, after
// the warning that says why.
func TestJoinOldPanelWarnsAndOpensBase(t *testing.T) {
	base, port := fakeHost(t, hostID{})
	out, opened := runJoin(t, port, true, false)
	if !strings.Contains(out, warnLine(base, "unknown", "unknown", version.Version, server.UIIdentity())) {
		t.Fatalf("output lacks the unknown-panel warning:\n%s", out)
	}
	if !strings.Contains(out, "ignores project links") {
		t.Fatalf("warning does not say why the base is opened:\n%s", out)
	}
	if strings.Contains(out, "?project=") {
		t.Fatalf("old panel must get the base url, not a project link:\n%s", out)
	}
	if len(opened) != 1 || opened[0] != base {
		t.Fatalf("opened = %v, want the base %q", opened, base)
	}
}

// The warning comes before the browser opens: the opener sees it already printed.
func TestJoinWarnsBeforeOpeningBrowser(t *testing.T) {
	_, port := fakeHost(t, hostID{"v0.0.1", "x"})
	out := &syncBuf{}
	prev := openURL
	var atOpen string
	openURL = func(string) { atOpen = out.String() }
	t.Cleanup(func() { openURL = prev })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := joinUI(ctx, out, t.TempDir(), port, true, false, map[string]bool{})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for atOpen == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(atOpen, "warning:") {
		t.Fatalf("browser opened before the warning was printed:\n%s", atOpen)
	}
}

// A host identity warns once per process, not on every re-join.
func TestJoinWarnsOncePerHostIdentity(t *testing.T) {
	_, port := fakeHost(t, hostID{"v0.0.1", "x"})
	warned := map[string]bool{}
	first, _ := runJoinWarned(t, port, true, true, warned)
	second, _ := runJoinWarned(t, port, false, true, warned)
	if !strings.Contains(first, "warning:") || strings.Contains(second, "warning:") {
		t.Fatalf("want a warning on the first join only:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

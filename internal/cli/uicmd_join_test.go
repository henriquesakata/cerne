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

	"github.com/henriquesakata/cerne/internal/version"
)

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
// (omitted when empty — a panel from before F-0373), registers any dir as
// project "proj-x", and holds /api/stream open like the real host.
func fakeHost(t *testing.T, hostVersion string) (base string, port int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"projects": []any{}, "default": ""}
		if hostVersion != "" {
			body["version"] = hostVersion
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"pid": "proj-x", "name": "Proj X"})
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
		_, err := joinUI(ctx, out, t.TempDir(), port, firstJoin, noBrowser)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
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

// criterion 1: the project's own link, not the bare base.
func TestJoinPrintsProjectLink(t *testing.T) {
	base, port := fakeHost(t, version.Short())
	out, _ := runJoin(t, port, true, true)
	want := base + "/?project=proj-x"
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks the project link %q:\n%s", want, out)
	}
}

// criterion 2: opens that link on the first join without --no-browser.
func TestJoinOpensProjectLinkInBrowser(t *testing.T) {
	base, port := fakeHost(t, version.Short())
	_, opened := runJoin(t, port, true, false)
	if len(opened) != 1 || opened[0] != base+"/?project=proj-x" {
		t.Fatalf("opened = %v, want exactly the project link", opened)
	}
}

func TestJoinNoBrowserFlagDoesNotOpen(t *testing.T) {
	_, port := fakeHost(t, version.Short())
	if _, opened := runJoin(t, port, true, true); len(opened) != 0 {
		t.Fatalf("opened = %v with --no-browser, want none", opened)
	}
}

// criterion 7: a re-join after a takeover never reopens the browser.
func TestJoinRejoinDoesNotReopenBrowser(t *testing.T) {
	_, port := fakeHost(t, version.Short())
	if _, opened := runJoin(t, port, false, false); len(opened) != 0 {
		t.Fatalf("opened = %v on re-join, want none", opened)
	}
}

// criterion 3: a different host version warns with both versions and the fix.
func TestJoinWarnsOnDifferentVersion(t *testing.T) {
	base, port := fakeHost(t, "9.9")
	out, _ := runJoin(t, port, true, true)
	want := fmt.Sprintf("warning: the cerne ui on %s is version 9.9, this cerne is %s; stop that ui (it serves its own embedded UI) and run cerne ui again", base, version.Short())
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks the warning %q:\n%s", want, out)
	}
}

// criterion 4: same version, no warning.
func TestJoinSameVersionIsSilent(t *testing.T) {
	_, port := fakeHost(t, version.Short())
	out, _ := runJoin(t, port, true, true)
	if strings.Contains(out, "warning") {
		t.Fatalf("unexpected warning:\n%s", out)
	}
}

// criterion 5: a host that does not report its version is a different one.
func TestJoinWarnsWhenHostReportsNoVersion(t *testing.T) {
	_, port := fakeHost(t, "")
	out, _ := runJoin(t, port, true, true)
	if !strings.Contains(out, "warning: the cerne ui on") || !strings.Contains(out, "is version unknown") {
		t.Fatalf("output lacks the unknown-version warning:\n%s", out)
	}
}

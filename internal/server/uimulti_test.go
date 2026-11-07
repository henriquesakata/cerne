package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	r, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, r.StatusCode)
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("GET %s decode: %v", url, err)
	}
}

func postJSON(t *testing.T, url string, body, v any) {
	t.Helper()
	b, _ := json.Marshal(body)
	r, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status %d", url, r.StatusCode)
	}
	if v != nil {
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			t.Fatalf("POST %s decode: %v", url, err)
		}
	}
}

type projectsList struct {
	Projects []projectInfo `json:"projects"`
	Default  string        `json:"default"`
}

func TestMultiHubStreamFormatting(t *testing.T) {
	hub := newMultiHub(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		hub.handleStream(w, r)
	})
	srv := httptest.NewServer(mux)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatalf("GET /api/stream: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
		srv.Close()
	})
	events := make(chan string, 8)
	go func() {
		br := bufio.NewReader(resp.Body)
		for {
			d, err := readSSEData(br)
			if err != nil {
				return
			}
			events <- d
		}
	}()

	if got := waitEvent(t, events); got != "" {
		t.Fatalf("connect frame data = %q, want empty (projects event)", got)
	}
	hub.broadcast(liveEvent{kind: "change", pid: "proj-x", sha: "abc123"})
	if got := waitEvent(t, events); got != "proj-x abc123" {
		t.Fatalf("change frame data = %q, want %q", got, "proj-x abc123")
	}
}

func TestRegistryWatchBroadcastsChange(t *testing.T) {
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(dir string) (string, string, string, string, error) {
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	hub := newMultiHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := &registry{
		projects: make(map[string]*registryEntry),
		hub:      hub,
		resolve:  resolve,
		parent:   ctx,
		poll:     10 * time.Millisecond,
	}
	sub := hub.subscribe()
	if _, _, err := reg.add("A"); err != nil {
		t.Fatalf("add: %v", err)
	}

	seedFeature(t, repoA, "F-0002", "Feature B", domain.StateCandidate)
	sha, err := gitback.CommitAndPush(repoA, "second")
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	deadline := time.After(broadcastWait)
	for {
		select {
		case ev := <-sub:
			if ev.kind != "change" {
				continue // skip the projects event from add()
			}
			if ev.pid != "proj-a" || ev.sha != sha {
				t.Fatalf("change ev = %+v, want pid proj-a sha %s", ev, sha)
			}
			return
		case <-deadline:
			t.Fatal("no change event for the committed HEAD")
		}
	}
}

// TestWatchHeadMultiDetectsFromBaseline pins the fix deterministically: the
// watcher is handed a stale baseline (h0) while HEAD is already at h1, so its
// first poll must detect the advance and broadcast exactly h1 — no dependence on
// when the goroutine is scheduled relative to the commit (the old flakiness).
func TestWatchHeadMultiDetectsFromBaseline(t *testing.T) {
	repoA, _, _ := newGitBackendNoClean(t)
	h0, err := gitback.HeadSHA(repoA)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	seedFeature(t, repoA, "F-0002", "Feature B", domain.StateCandidate)
	h1, err := gitback.CommitAndPush(repoA, "second")
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if h1 == h0 {
		t.Fatal("commit did not advance HEAD")
	}

	hub := newMultiHub(nil)
	sub := hub.subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchHeadMulti(ctx, hub, "proj-a", repoA, 10*time.Millisecond, h0)

	deadline := time.After(broadcastWait)
	for {
		select {
		case ev := <-sub:
			if ev.kind != "change" {
				continue
			}
			if ev.pid != "proj-a" || ev.sha != h1 {
				t.Fatalf("change ev = %+v, want pid proj-a sha %s", ev, h1)
			}
			return
		case <-deadline:
			t.Fatal("no change event from the stale baseline")
		}
	}
}

func TestUIMultiRegisterListDeregister(t *testing.T) {
	repoA, projA, _ := newGitBackendNoClean(t)
	repoB, projB, _ := newGitBackendNoClean(t)
	resolve := func(dir string) (string, string, string, string, error) {
		switch dir {
		case "A":
			return repoA, projA, "proj-a", "Projeto A", nil
		case "B":
			return repoB, projB, "proj-b", "Projeto B", nil
		}
		return "", "", "", "", fmt.Errorf("dir desconhecido: %s", dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	var list projectsList
	getJSON(t, srv.URL+"/api/projects", &list)
	if len(list.Projects) != 1 || list.Default != "proj-a" {
		t.Fatalf("initial list = %+v, want only proj-a (default)", list)
	}

	var added projectInfo
	postJSON(t, srv.URL+"/api/projects", map[string]string{"dir": "B"}, &added)
	if added.PID != "proj-b" || added.Name != "Projeto B" {
		t.Fatalf("added = %+v, want proj-b/Projeto B", added)
	}

	getJSON(t, srv.URL+"/api/projects", &list)
	if len(list.Projects) != 2 || list.Default != "proj-a" {
		t.Fatalf("list after register = %+v, want 2 projects, default proj-a", list)
	}

	// Both scoped state routes resolve.
	for _, pid := range []string{"proj-a", "proj-b"} {
		r, err := http.Get(srv.URL + "/api/projects/" + pid + "/state")
		if err != nil {
			t.Fatalf("GET state %s: %v", pid, err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET state %s: status %d", pid, r.StatusCode)
		}
	}

	// Deregister B.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/projects/proj-b", nil)
	dr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE proj-b: %v", err)
	}
	dr.Body.Close()
	if dr.StatusCode != http.StatusOK {
		t.Fatalf("DELETE proj-b: status %d", dr.StatusCode)
	}

	getJSON(t, srv.URL+"/api/projects", &list)
	if len(list.Projects) != 1 {
		t.Fatalf("list after deregister = %+v, want 1", list)
	}
	r, _ := http.Get(srv.URL + "/api/projects/proj-b/state")
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("GET state proj-b after deregister: status %d, want 404", r.StatusCode)
	}
}

// TestUIMultiExposesFullRouteSet: the local ui serves even the non-remote
// routes (e.g. graph), the counterpart of the serve-subset invariant.
func TestUIMultiExposesFullRouteSet(t *testing.T) {
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(dir string) (string, string, string, string, error) {
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	r, err := http.Get(srv.URL + "/api/projects/proj-a/graph")
	if err != nil {
		t.Fatalf("GET graph: %v", err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("ui /graph: status %d, want 200 (the local ui exposes the full set)", r.StatusCode)
	}
}

func TestUIMultiRegisterUnknownDir(t *testing.T) {
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(dir string) (string, string, string, string, error) {
		if dir == "A" {
			return repoA, projA, "proj-a", "Projeto A", nil
		}
		return "", "", "", "", fmt.Errorf("dir desconhecido: %s", dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	b, _ := json.Marshal(map[string]string{"dir": "Z"})
	r, err := http.Post(srv.URL+"/api/projects", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("register unknown dir: status %d, want 422", r.StatusCode)
	}
}

// TestProjectsListReadsNameFromBackend pins F-0331 crit. 3: GET /api/projects
// serves the backend's project.json name on every list — a rename landing by
// pull shows without restarting the ui; when project.json cannot be read, the
// last name read stands in.
func TestProjectsListReadsNameFromBackend(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	repoA, projA, _ := newGitBackendNoClean(t)
	resolve := func(string) (string, string, string, string, error) {
		return repoA, projA, "proj-a", "Projeto A", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	name := func() string {
		t.Helper()
		var list projectsList
		getJSON(t, srv.URL+"/api/projects", &list)
		if len(list.Projects) != 1 {
			t.Fatalf("list = %+v, want one project", list)
		}
		return list.Projects[0].Name
	}
	if got := name(); got != "Projeto Demo" {
		t.Fatalf("name = %q, want the backend's %q", got, "Projeto Demo")
	}

	if err := store.SaveProject(repoA, store.Project{Name: "Renomeado"}); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}
	commitBackend(t, repoA) // the state a pull would bring (N-0023)
	if got := name(); got != "Renomeado" {
		t.Fatalf("name after backend rename = %q, want %q without restart", got, "Renomeado")
	}

	if err := os.Remove(filepath.Join(repoA, "project.json")); err != nil {
		t.Fatalf("Remove project.json: %v", err)
	}
	// The fallback is the last good read (critic C11), never the stale
	// registration-time name this feature removes.
	if got := name(); got != "Renomeado" {
		t.Fatalf("name without project.json = %q, want the last good read %q", got, "Renomeado")
	}
}

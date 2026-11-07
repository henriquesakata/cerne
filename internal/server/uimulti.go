package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// This is the multi-project variant of `cerne ui`: one process serves N local
// projects, each resolved from a directory (not cloned from a URL like the
// remote `cerne serve`). It reuses the single-project handlers (which already
// take repoDir/projDir) under project-scoped routes, adds a registry with the
// per-project live-refresh of case 0031, and a multiplexed SSE stream.

// liveEvent is one server-sent signal: a backend change for a project (kind
// "change", with pid+sha), a change to the set of registered projects (kind
// "projects") or a model catalog refresh that changed it (kind "models", with
// the JSON diff in data — F-0311).
type liveEvent struct {
	kind string // "change" | "projects" | "models"
	pid  string
	sha  string
	data string
}

// multiHub fans liveEvents out to every /api/stream subscriber. done is the
// server's lifetime: when it closes, every stream ends — an SSE connection
// never goes idle on its own, so without this a graceful shutdown would wait
// on streams that only the process death would close.
type multiHub struct {
	mu   sync.Mutex
	subs map[chan liveEvent]struct{}
	done <-chan struct{}
	// models is the last catalog refresh announcement, replayed to every page
	// that connects later: the boot refresh usually lands before the browser
	// opens the stream, and the bell must still hear about it.
	models *liveEvent
}

func newMultiHub(done <-chan struct{}) *multiHub {
	return &multiHub{subs: make(map[chan liveEvent]struct{}), done: done}
}

func (h *multiHub) subscribe() chan liveEvent {
	ch := make(chan liveEvent, 8)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *multiHub) unsubscribe(ch chan liveEvent) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// broadcast delivers ev to every subscriber without blocking.
func (h *multiHub) broadcast(ev liveEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.kind == "models" {
		h.models = &ev
	}
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// handleStream is the multiplexed SSE endpoint (GET /api/stream): "change"
// events carry "<pid> <sha>" so the page refetches the active project (and
// badges the others); a "projects" event tells the page to reload the project
// list. Emits a projects event on connect (resync), keeps the connection warm,
// and unsubscribes on disconnect.
func (h *multiHub) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorBody("streaming not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.subscribe()
	defer h.unsubscribe(ch)
	h.mu.Lock()
	replay := h.models
	h.mu.Unlock()
	telemetry.Default.SSEClients.Inc()
	defer telemetry.Default.SSEClients.Dec()

	fmt.Fprint(w, "event: projects\ndata: \n\n") // resync the list on connect
	if replay != nil {
		fmt.Fprintf(w, "event: models\ndata: %s\n\n", replay.data)
	}
	flusher.Flush()

	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			switch ev.kind {
			case "change":
				fmt.Fprintf(w, "event: change\ndata: %s %s\n\n", ev.pid, ev.sha)
			case "projects":
				fmt.Fprint(w, "event: projects\ndata: \n\n")
			case "models":
				fmt.Fprintf(w, "event: models\ndata: %s\n\n", ev.data)
			}
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// Resolver maps a project directory to its backend locations and identity.
// Injected by the CLI (resolveProject) so this package stays free of project
// config concerns. A non-nil error means the directory is not a cerne project.
type Resolver func(dir string) (repoDir, projDir, projectID, name string, err error)

// registryEntry is one registered project and the cancel that stops its
// live-refresh goroutines.
type registryEntry struct {
	pid     string
	name    string // the name read at registration; list's fallback when project.json can't be read
	repoDir string
	projDir string
	cancel  context.CancelFunc
}

// projectInfo is the public shape of a registered project (GET /api/projects).
// Graph and Mindmap carry the project's effective graph.enabled (F-0315) and
// mindmap.enabled (F-0187, from the settings registry since F-0314): false
// hides that tab in the UI.
type projectInfo struct {
	PID     string `json:"pid"`
	Name    string `json:"name"`
	Graph   bool   `json:"graph"`
	Mindmap bool   `json:"mindmap"`
}

// newProjectInfo resolves the project's tab gates once (project > host >
// default). Fail-open: no projDir or an absent key leaves the tab on; a bad
// value or unreadable file elsewhere does not touch a gate, and an invalid
// gate value falls through to the next scope, then the default.
func newProjectInfo(pid, name, projDir string) projectInfo {
	info := projectInfo{PID: pid, Name: name, Graph: true, Mindmap: true}
	if projDir == "" {
		return info
	}
	for _, s := range project.ResolveSettings(projDir).Settings {
		switch s.ID {
		case "graph.enabled":
			info.Graph = s.Value != false
		case "mindmap.enabled":
			info.Mindmap = s.Value != false
		}
	}
	return info
}

// registry holds the registered projects and starts/stops the per-project
// HEAD watcher (and optional pull loop) as projects come and go.
type registry struct {
	mu       sync.Mutex
	projects map[string]*registryEntry
	order    []string // registration order; order[0] is the default project
	hub      *multiHub
	resolve  Resolver
	parent   context.Context
	pull     time.Duration
	poll     time.Duration // HEAD watch interval; defaults to headPollInterval
}

// add resolves dir and registers it, starting its live-refresh watcher.
// Idempotent: a directory whose project is already registered returns the
// existing entry. Broadcasts a projects event on a genuine add.
func (r *registry) add(dir string) (pid, name string, err error) {
	repoDir, projDir, projectID, name, err := r.resolve(dir)
	if err != nil {
		return "", "", err
	}
	r.mu.Lock()
	if e, ok := r.projects[projectID]; ok {
		r.mu.Unlock()
		return e.pid, e.name, nil
	}
	ctx, cancel := context.WithCancel(r.parent)
	r.projects[projectID] = &registryEntry{pid: projectID, name: name, repoDir: repoDir, projDir: projDir, cancel: cancel}
	r.order = append(r.order, projectID)
	interval := r.poll
	r.mu.Unlock()
	if interval == 0 {
		interval = headPollInterval
	}

	baseline, _ := gitback.HeadSHA(repoDir)
	go watchHeadMulti(ctx, r.hub, projectID, repoDir, interval, baseline)
	if r.pull > 0 {
		go pullLoop(ctx, repoDir, r.pull)
	}
	r.hub.broadcast(liveEvent{kind: "projects"})
	return projectID, name, nil
}

// remove deregisters pid, stopping its watcher. Returns false when pid was
// not registered. Broadcasts a projects event on a genuine removal.
func (r *registry) remove(pid string) bool {
	r.mu.Lock()
	e, ok := r.projects[pid]
	if ok {
		e.cancel()
		delete(r.projects, pid)
		r.order = slices.DeleteFunc(r.order, func(p string) bool { return p == pid })
	}
	r.mu.Unlock()
	if ok {
		r.hub.broadcast(liveEvent{kind: "projects"})
	}
	return ok
}

// get returns the registered project for pid.
func (r *registry) get(pid string) (*registryEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.projects[pid]
	return e, ok
}

// list returns the registered projects in registration order and the default
// pid (the first registered), empty when none.
// projDir returns pid's project directory, or "" when not registered.
func (r *registry) projDir(pid string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.projects[pid]; ok {
		return e.projDir
	}
	return ""
}

func (r *registry) list() (infos []projectInfo, def string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	infos = make([]projectInfo, 0, len(r.order))
	for _, pid := range r.order {
		if e, ok := r.projects[pid]; ok {
			// Read the name per list (F-0331): a rename by this ui, another
			// clone (pulled) or a hand edit shows without a restart.
			name := e.name
			if p, err := store.LoadProject(e.repoDir); err == nil && p.Name != "" {
				name = p.Name
			}
			infos = append(infos, newProjectInfo(e.pid, name, e.projDir))
		}
	}
	if len(r.order) > 0 {
		def = r.order[0]
	}
	return infos, def
}

// watchHeadMulti polls one project's HEAD and broadcasts a change (tagged with
// pid) whenever it advances past last, routing through the multiplexed hub. The
// caller (add) captures last synchronously at registration time, so a HEAD
// advance right after add() is never missed by a late goroutine start. Stops
// when ctx is cancelled (project removed / server down).
func watchHeadMulti(ctx context.Context, hub *multiHub, pid, repoDir string, interval time.Duration, last string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sha, err := gitback.HeadSHA(repoDir)
			if err != nil {
				continue
			}
			if sha != last {
				last = sha
				hub.broadcast(liveEvent{kind: "change", pid: pid, sha: sha})
			}
		}
	}
}

// NewUIMulti builds the multi-project `cerne ui` handler: project-scoped data
// routes (full parity with the single-project mux), the projects registry
// endpoints, the multiplexed SSE stream, and the embedded SPA. It registers
// initialDir as the first (default) project; resolve provides per-project
// locations. The watchers stop when ctx is cancelled.
func NewUIMulti(ctx context.Context, resolve Resolver, pullInterval time.Duration, initialDir string) (http.Handler, error) {
	hub := newMultiHub(ctx.Done())
	// A finished lint revalidation (F-0155) announces like any backend change:
	// the page refetches and receives the fresh findings (stale: false).
	lintBroadcast = func(pid string) {
		hub.broadcast(liveEvent{kind: "change", pid: pid})
	}
	// A settings write (F-0315) changes the tab gates projectInfo carries:
	// every tab re-reads /api/projects and shows or hides Graph/Mindmap.
	settingsBroadcast = func() { hub.broadcast(liveEvent{kind: "projects"}) }
	reg := &registry{
		projects: make(map[string]*registryEntry),
		hub:      hub,
		resolve:  resolve,
		parent:   ctx,
		pull:     pullInterval,
		poll:     headPollInterval,
	}
	pid, _, err := reg.add(initialDir)
	if err != nil {
		return nil, err
	}
	// F-0311: the model catalog refresh rides the process boot — here, once,
	// never in registry.add (a second project must not re-trigger it).
	startCatalogRefresh(ctx, hub, reg.projDir(pid))

	mux := http.NewServeMux()

	// Project set: list, register (by dir), deregister (by pid).
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		infos, def := reg.list()
		writeJSON(w, http.StatusOK, map[string]any{"projects": infos, "default": def})
	})
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Dir string `json:"dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Dir == "" {
			writeJSON(w, http.StatusBadRequest, errorBody("provide the project directory (field dir)"))
			return
		}
		pid, name, err := reg.add(req.Dir)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, newProjectInfo(pid, name, reg.projDir(pid)))
	})
	mux.HandleFunc("DELETE /api/projects/{pid}", func(w http.ResponseWriter, r *http.Request) {
		if !reg.remove(r.PathValue("pid")) {
			writeJSON(w, http.StatusNotFound, errorBody("project not registered"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
	})

	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		hub.handleStream(w, r)
	})

	// withProject resolves {pid} from the registry and binds the project's
	// locations into a data handler; 404 when the project is not registered.
	withProject := func(h scopedHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			e, ok := reg.get(r.PathValue("pid"))
			if !ok {
				writeJSON(w, http.StatusNotFound, errorBody("project not registered: "+r.PathValue("pid")))
				return
			}
			h(w, r, e.repoDir, e.projDir, e.pid)
		}
	}
	// The local ui exposes every data route (the embedded UI uses all of them).
	mountDataRoutes(mux, "/projects/{pid}", withProject, false)
	mountTelemetry(mux)
	mountPprof(mux)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody("route not found"))
	})
	mux.Handle("/", spaHandler())
	return mux, nil
}

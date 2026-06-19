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
)

// This is the multi-project variant of `cerne ui`: one process serves N local
// projects, each resolved from a directory (not cloned from a URL like the
// remote `cerne serve`). It reuses the single-project handlers (which already
// take repoDir/projDir) under project-scoped routes, adds a registry with the
// per-project live-refresh of case 0031, and a multiplexed SSE stream.

// liveEvent is one server-sent signal: a backend change for a project (kind
// "change", with pid+sha) or a change to the set of registered projects (kind
// "projects").
type liveEvent struct {
	kind string // "change" | "projects"
	pid  string
	sha  string
}

// multiHub fans liveEvents out to every /api/stream subscriber.
type multiHub struct {
	mu   sync.Mutex
	subs map[chan liveEvent]struct{}
}

func newMultiHub() *multiHub {
	return &multiHub{subs: make(map[chan liveEvent]struct{})}
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
		writeJSON(w, http.StatusInternalServerError, errorBody("streaming não suportado"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.subscribe()
	defer h.unsubscribe(ch)

	fmt.Fprint(w, "event: projects\ndata: \n\n") // resync the list on connect
	flusher.Flush()

	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
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
	name    string
	repoDir string
	projDir string
	cancel  context.CancelFunc
}

// projectInfo is the public shape of a registered project (GET /api/projects).
type projectInfo struct {
	PID  string `json:"pid"`
	Name string `json:"name"`
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

	go watchHeadMulti(ctx, r.hub, projectID, repoDir, interval)
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
func (r *registry) list() (infos []projectInfo, def string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	infos = make([]projectInfo, 0, len(r.order))
	for _, pid := range r.order {
		if e, ok := r.projects[pid]; ok {
			infos = append(infos, projectInfo{PID: e.pid, Name: e.name})
		}
	}
	if len(r.order) > 0 {
		def = r.order[0]
	}
	return infos, def
}

// watchHeadMulti polls one project's HEAD and broadcasts a change (tagged with
// pid) whenever it advances. Mirrors liveHub.watchHead but routes through the
// multiplexed hub. Stops when ctx is cancelled (project removed / server down).
func watchHeadMulti(ctx context.Context, hub *multiHub, pid, repoDir string, interval time.Duration) {
	last, _ := gitback.HeadSHA(repoDir)
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
	hub := newMultiHub()
	reg := &registry{
		projects: make(map[string]*registryEntry),
		hub:      hub,
		resolve:  resolve,
		parent:   ctx,
		pull:     pullInterval,
		poll:     headPollInterval,
	}
	if _, _, err := reg.add(initialDir); err != nil {
		return nil, err
	}

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
			writeJSON(w, http.StatusBadRequest, errorBody("informe o diretório do projeto (campo dir)"))
			return
		}
		pid, name, err := reg.add(req.Dir)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, projectInfo{PID: pid, Name: name})
	})
	mux.HandleFunc("DELETE /api/projects/{pid}", func(w http.ResponseWriter, r *http.Request) {
		if !reg.remove(r.PathValue("pid")) {
			writeJSON(w, http.StatusNotFound, errorBody("projeto não registrado"))
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
				writeJSON(w, http.StatusNotFound, errorBody("projeto não registrado: "+r.PathValue("pid")))
				return
			}
			h(w, r, e.repoDir, e.projDir, e.pid)
		}
	}
	// The local ui exposes every data route (the embedded UI uses all of them).
	mountDataRoutes(mux, "/projects/{pid}", withProject, false)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody("rota não encontrada"))
	})
	mux.Handle("/", spaHandler())
	return mux, nil
}

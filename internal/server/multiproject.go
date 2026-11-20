package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// MultiServer serves many projects from a single process under
// /api/projects/{pid}/*, reusing the single-project handlers with (repoDir,
// projDir, projectID) resolved per request from {pid}. Each project's cache
// (clone + index + docs.lock) lives under project.CacheDir(pid); the backend
// git URL is registered per project via PUT /api/projects/{pid}. Every /api
// route requires the S2S shared secret. The host (e.g. the Studio BFF) owns
// per-user/per-project authorization and the git-host provisioning.
type MultiServer struct {
	secret string
}

// NewMultiProject builds the multi-project handler. secret is the S2S
// shared secret required on every request (Bearer).
func NewMultiProject(secret string) http.Handler {
	s := &MultiServer{secret: secret}
	return s.requireSecret(s.mux())
}

// pidRe is the shape of a project id: cerne init's slug ([a-z0-9-]) or a
// host's own id, such as the hub's UUID. No dot, slash or backslash, so a pid
// is always one directory under the cache root (B-0045).
var pidRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// errBadPID is a {pid} outside pidRe: it names no project, so a 404.
var errBadPID = errors.New("not a project id")

// projectPaths resolves the per-project cache locations for pid: the backend
// clone (repo/) and the project dir holding docs.lock (proj/), both under
// CacheDir(pid) so the index (CacheDir/index.db) is their sibling. The mux
// decodes %2F inside a segment, so pid is checked before it becomes a path.
func projectPaths(pid string) (repoDir, projDir string, err error) {
	if !pidRe.MatchString(pid) {
		return "", "", fmt.Errorf("%w: %q", errBadPID, pid)
	}
	cache, err := project.CacheDir(pid)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(cache, "repo"), filepath.Join(cache, "proj"), nil
}

// projectRegistered reports whether pid's backend has been cloned.
func projectRegistered(pid string) bool {
	repoDir, _, err := projectPaths(pid)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(repoDir, ".git"))
	return err == nil
}

// requireSecret rejects any request whose Bearer token is not the configured
// S2S secret.
func (s *MultiServer) requireSecret(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.secret {
			writeJSON(w, http.StatusUnauthorized, errorBody("unauthorized"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *MultiServer) mux() http.Handler {
	mux := http.NewServeMux()

	// Register/ensure a project's backend (clone if missing). Idempotent.
	mux.HandleFunc("PUT /api/projects/{pid}", func(w http.ResponseWriter, r *http.Request) {
		s.handleRegister(w, r, r.PathValue("pid"))
	})

	// withProject resolves (repoDir, projDir, pid) for a registered project and
	// binds them into a data handler; 404 when the project was never registered.
	withProject := func(h scopedHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			pid := r.PathValue("pid")
			repoDir, projDir, err := projectPaths(pid)
			if err != nil {
				writePathsError(w, err)
				return
			}
			// A clone lost with the cache (emptyDir + pod reschedule) is
			// re-created from CERNE_SERVE_PROJECTS instead of 404-ing until
			// someone re-runs the PUT by hand.
			if !projectRegistered(pid) && !ensureConfiguredProject(pid) {
				writeJSON(w, http.StatusNotFound, errorBody("project not registered: "+pid))
				return
			}
			// The clone is created once, at register time; without this the
			// served docs stay pinned to that day's SHA forever (client project F-0517).
			// Throttled to one fetch per TTL per clone.
			// F-0434: the registered backend is the one docs.lock pins, else the
			// CERNE_SERVE_PROJECTS one. A known backend that differs from the
			// clone's origin is refused; an unreadable lock with no configured
			// backend is too. A registration made before the lock carried the
			// backend (empty) keeps serving, with a warning to re-register.
			backend, known := registeredBackend(projDir, pid)
			if !known {
				writeJSON(w, http.StatusConflict, errorBody("cannot verify the backend of project "+pid+": docs.lock is unreadable and no backend is configured; register it again"))
				return
			}
			if backend == "" {
				warnUnverified(pid)
			} else if err := checkOriginTTL(repoDir, backend); err != nil {
				writeJSON(w, http.StatusConflict, errorBody(err.Error()))
				return
			}
			refreshProject(repoDir)
			h(w, r, repoDir, projDir, pid)
		}
	}
	// Remote serve exposes the deliberate subset — routes flagged remote.
	mountDataRoutes(mux, "/projects/{pid}", withProject, true)
	mountTelemetry(mux)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody("route not found"))
	})
	return mux
}

// handleRegister ensures pid's backend is cloned and a docs.lock exists,
// cloning on first call and idempotent thereafter.
func (s *MultiServer) handleRegister(w http.ResponseWriter, r *http.Request, pid string) {
	var req struct {
		Backend string   `json:"backend"`
		Name    string   `json:"name"`
		Modules []string `json:"modules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid body: "+err.Error()))
		return
	}
	if req.Backend == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("provide the backend (git URL)"))
		return
	}
	repoDir, projDir, err := projectPaths(pid)
	if err != nil {
		writePathsError(w, err)
		return
	}
	// A freshly provisioned backend has no cerne layout yet; name (fallback pid)
	// and modules seed project.json when ensureProjectClone initializes it.
	name := req.Name
	if name == "" {
		name = pid
	}
	if err := ensureProjectClone(repoDir, projDir, req.Backend, name, req.Modules); err != nil {
		if errors.Is(err, gitback.ErrForeignClone) || errors.Is(err, gitback.ErrUnverifiableClone) {
			writeJSON(w, http.StatusConflict, errorBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"project": pid, "backend": req.Backend})
}

// writePathsError answers a projectPaths failure: 404 for a malformed pid,
// 500 for a cache dir the OS cannot give.
func writePathsError(w http.ResponseWriter, err error) {
	if errors.Is(err, errBadPID) {
		writeJSON(w, http.StatusNotFound, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
}

// ensureProjectClone clones backend into repoDir when absent, initializes the
// cerne layout when the backend is empty (no project.json — a host just created
// the repo), and ensures a docs.lock pinning that backend exists in projDir.
// Idempotent: an already-cloned, already-initialized backend is left intact.
func ensureProjectClone(repoDir, projDir, backend, name string, modules []string) error {
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
			return err
		}
		if err := gitback.Clone(backend, repoDir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := gitback.CheckOrigin(repoDir, backend); err != nil {
		return err // F-0434: a cache clone of another backend is never reused
	}
	// Freshly provisioned backend: no layout yet. Initialize and push it so the
	// project is usable immediately (mirrors the join path of `cerne init`).
	if _, err := store.LoadProject(repoDir); errors.Is(err, store.ErrNotFound) {
		if err := store.InitLayout(repoDir, store.Project{Name: name, Modules: modules}); err != nil {
			return err
		}
		if _, err := gitback.CommitAndPush(repoDir, "cerne: initialize backend layout"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		return err
	}
	if _, err := project.LoadLock(projDir); err != nil {
		if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: backend}); err != nil {
			return err
		}
	}
	return nil
}

// registeredBackend is pid's backend: docs.lock's, else the configured one.
// known is false only when the lock is unreadable and nothing is configured;
// an empty backend with known true is a legacy registration.
func registeredBackend(projDir, pid string) (backend string, known bool) {
	lock, err := project.LoadLock(projDir)
	if err == nil && lock.Backend != "" {
		return lock.Backend, true
	}
	if b := configuredBackends()[pid]; b != "" {
		return b, true
	}
	return "", err == nil
}

var warnedUnverified sync.Map

func warnUnverified(pid string) {
	if _, dup := warnedUnverified.LoadOrStore(pid, true); !dup {
		log.Printf("cerne: project %s has no backend in docs.lock, so its cache clone cannot be verified; register it again (PUT /api/projects/%s)", pid, pid)
	}
}

type originVerdict struct {
	backend string
	at      time.Time
	err     error
}

var originChecks sync.Map // repoDir -> originVerdict

// checkOriginTTL is gitback.CheckOrigin, remembered per clone for the refresh
// TTL (none when the refresh is off) so a burst of requests reads .git/config
// once.
func checkOriginTTL(repoDir, backend string) error {
	ttl := pullTTL()
	if v, ok := originChecks.Load(repoDir); ok && ttl > 0 {
		if c := v.(originVerdict); c.backend == backend && nowFunc().Sub(c.at) < ttl {
			return c.err
		}
	}
	err := gitback.CheckOrigin(repoDir, backend)
	originChecks.Store(repoDir, originVerdict{backend, nowFunc(), err})
	return err
}

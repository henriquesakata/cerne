package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedBareBackend creates a bare git origin seeded with one feature titled
// `title`, returning the origin path (a backend the multi-server can clone).
func seedBareBackend(t *testing.T, title string) string {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	work := filepath.Join(base, "work")
	if err := gitback.Clone(origin, work); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := store.InitLayout(work, store.Project{Name: title}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveFeature(work, store.Feature{ID: "F-0001", Title: title, State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(work, "seed"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	return origin
}

// doMP performs a request against the multi-server, optionally with the Bearer
// secret, decoding the JSON body into out when provided.
func doMP(t *testing.T, h http.Handler, method, target, body, secret string, out any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec
}

// regBody builds a JSON register body, escaping the backend path (Windows
// paths carry backslashes that would break a hand-concatenated JSON string).
func regBody(t *testing.T, backend string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"backend": backend})
	if err != nil {
		t.Fatalf("marshal backend: %v", err)
	}
	return string(b)
}

type mpState struct {
	Features []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"features"`
}

const mpSecret = "s2s-secret"

// TestMultiProjectIsolation: two registered projects serve their own state.
func TestMultiProjectIsolation(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	originA := seedBareBackend(t, "Projeto A")
	originB := seedBareBackend(t, "Projeto B")

	for pid, origin := range map[string]string{"proj-a": originA, "proj-b": originB} {
		rec := doMP(t, h, http.MethodPut, "/api/projects/"+pid, regBody(t, origin), mpSecret, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("register %s: status %d, body %s", pid, rec.Code, rec.Body.String())
		}
	}

	var a, b mpState
	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-a/state", "", mpSecret, &a); rec.Code != http.StatusOK {
		t.Fatalf("state a: %d %s", rec.Code, rec.Body.String())
	}
	doMP(t, h, http.MethodGet, "/api/projects/proj-b/state", "", mpSecret, &b)
	if len(a.Features) != 1 || a.Features[0].Title != "Projeto A" {
		t.Fatalf("proj-a features = %+v, want [Projeto A]", a.Features)
	}
	if len(b.Features) != 1 || b.Features[0].Title != "Projeto B" {
		t.Fatalf("proj-b features = %+v, want [Projeto B]", b.Features)
	}
}

// TestMultiProjectCreateIsolated: creating a feature in one project does not
// leak into another.
func TestMultiProjectCreateIsolated(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	originA := seedBareBackend(t, "Projeto A")
	originB := seedBareBackend(t, "Projeto B")
	doMP(t, h, http.MethodPut, "/api/projects/proj-a", regBody(t, originA), mpSecret, nil)
	doMP(t, h, http.MethodPut, "/api/projects/proj-b", regBody(t, originB), mpSecret, nil)

	rec := doMP(t, h, http.MethodPost, "/api/projects/proj-a/features", `{"title":"Nova em A"}`, mpSecret, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create in a: %d %s", rec.Code, rec.Body.String())
	}
	var a, b mpState
	doMP(t, h, http.MethodGet, "/api/projects/proj-a/state", "", mpSecret, &a)
	doMP(t, h, http.MethodGet, "/api/projects/proj-b/state", "", mpSecret, &b)
	if len(a.Features) != 2 {
		t.Fatalf("proj-a features = %d, want 2", len(a.Features))
	}
	if len(b.Features) != 1 {
		t.Fatalf("proj-b features = %d, want 1 (no leak)", len(b.Features))
	}
}

// TestMultiProjectAuth: every route requires the S2S secret.
func TestMultiProjectAuth(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	origin := seedBareBackend(t, "Projeto A")
	doMP(t, h, http.MethodPut, "/api/projects/proj-a", `{"backend":"`+origin+`"}`, mpSecret, nil)

	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-a/state", "", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no secret: status %d, want 401", rec.Code)
	}
	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-a/state", "", "wrong", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: status %d, want 401", rec.Code)
	}
}

// seedEmptyBackend returns a bare origin with NO cerne layout — an empty repo a
// host (e.g. identity-api) just created for a project's docs. Clonable, but
// uninitialized: exercises lazy InitLayout on register (case 0024).
func seedEmptyBackend(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	return origin
}

// TestMultiProjectRegisterInitsEmptyBackend: registering a freshly provisioned
// (layout-less) backend initializes the cerne layout and pushes it, so the
// project is immediately usable — state readable and features creatable (0024).
func TestMultiProjectRegisterInitsEmptyBackend(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	origin := seedEmptyBackend(t)

	body, err := json.Marshal(map[string]any{"backend": origin, "name": "Projeto Novo"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if rec := doMP(t, h, http.MethodPut, "/api/projects/proj-new", string(body), mpSecret, nil); rec.Code != http.StatusOK {
		t.Fatalf("register empty backend: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Layout exists: state is readable with zero features.
	var st mpState
	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-new/state", "", mpSecret, &st); rec.Code != http.StatusOK {
		t.Fatalf("state: %d %s", rec.Code, rec.Body.String())
	}
	if len(st.Features) != 0 {
		t.Fatalf("fresh project features = %d, want 0", len(st.Features))
	}

	// A feature can be created immediately on the freshly initialized backend.
	if rec := doMP(t, h, http.MethodPost, "/api/projects/proj-new/features", `{"title":"Primeira"}`, mpSecret, nil); rec.Code != http.StatusCreated {
		t.Fatalf("create feature: %d %s", rec.Code, rec.Body.String())
	}

	// The layout was pushed to the origin: a fresh clone sees project.json.
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("verify clone: %v", err)
	}
	if _, err := store.LoadProject(verify); err != nil {
		t.Fatalf("origin missing cerne layout after register: %v", err)
	}
}

// TestServeExposesOnlyRemoteSubset pins the `cerne serve` route subset against
// drift in the shared route table: a non-remote route (graph) is 404 even for a
// registered project, while a remote route (state) is 200.
func TestServeExposesOnlyRemoteSubset(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	origin := seedBareBackend(t, "Projeto A")
	doMP(t, h, http.MethodPut, "/api/projects/proj-a", regBody(t, origin), mpSecret, nil)

	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-a/state", "", mpSecret, nil); rec.Code != http.StatusOK {
		t.Fatalf("remote route /state: status %d, want 200", rec.Code)
	}
	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-a/graph", "", mpSecret, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("non-remote route /graph on serve: status %d, want 404", rec.Code)
	}
	// Write route the Studio host consumes (F-0281). An unknown feature is a
	// 404 from the handler, same status as an unmounted route — only the
	// error message ("feature not found" vs "route not found") separates them.
	var body map[string]string
	rec := doMP(t, h, http.MethodPost, "/api/projects/proj-a/features/F-9999/transition", `{"action":"approve"}`, mpSecret, &body)
	if body["error"] != "feature not found" {
		t.Fatalf("remote route /features/{id}/transition: status %d body %q, want the handler's JSON error", rec.Code, rec.Body.String())
	}
}

// TestMultiProjectUnknownProject: an unregistered project is a 404.
func TestMultiProjectUnknownProject(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	var got map[string]string
	rec := doMP(t, h, http.MethodGet, "/api/projects/ghost/state", "", mpSecret, &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project: status %d, want 404", rec.Code)
	}
}

// TestServer_RefusesForeignClone pins F-0434 criterion 2: a pid whose cache
// clone is another backend is never registered over it nor served.
func TestServer_RefusesForeignClone(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	originA := seedBareBackend(t, "Projeto A")
	originB := seedBareBackend(t, "Projeto B")

	if rec := doMP(t, h, http.MethodPut, "/api/projects/proj-x", regBody(t, originA), mpSecret, nil); rec.Code != http.StatusOK {
		t.Fatalf("register A: %d %s", rec.Code, rec.Body.String())
	}
	// Same pid, another backend: refused.
	if rec := doMP(t, h, http.MethodPut, "/api/projects/proj-x", regBody(t, originB), mpSecret, nil); rec.Code != http.StatusConflict {
		t.Fatalf("register B over A's clone: status %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	if o, err := gitback.OriginURL(mustRepo(t, "proj-x")); err != nil || o != originA {
		t.Fatalf("the clone origin changed after the refused register: %q, %v", o, err)
	}
	// The registered backend (docs.lock) moved to B while the clone is A's.
	_, projDir, _ := projectPaths("proj-x")
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: originB}); err != nil {
		t.Fatal(err)
	}
	rec := doMP(t, h, http.MethodGet, "/api/projects/proj-x/state", "", mpSecret, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("state: status %d, want 409", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Projeto A") {
		t.Fatalf("response leaks the other backend's data: %s", rec.Body.String())
	}
}

func mustRepo(t *testing.T, pid string) string {
	t.Helper()
	repoDir, _, err := projectPaths(pid)
	if err != nil {
		t.Fatal(err)
	}
	return repoDir
}

// A legacy registration (empty backend in docs.lock) keeps serving; an
// unreadable lock with nothing configured is refused (F-0434).
func TestServer_UnverifiableBackend(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	h := NewMultiProject(mpSecret)
	origin := seedBareBackend(t, "Projeto A")
	if rec := doMP(t, h, http.MethodPut, "/api/projects/proj-y", regBody(t, origin), mpSecret, nil); rec.Code != http.StatusOK {
		t.Fatalf("register: %d", rec.Code)
	}
	_, projDir, _ := projectPaths("proj-y")
	if err := project.SaveLock(projDir, project.Lock{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-y/state", "", mpSecret, nil); rec.Code != http.StatusOK {
		t.Fatalf("legacy empty backend: status %d, want 200 (served, warned)", rec.Code)
	}
	if err := os.WriteFile(filepath.Join(projDir, "docs.lock"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := doMP(t, h, http.MethodGet, "/api/projects/proj-y/state", "", mpSecret, nil)
	if rec.Code != http.StatusConflict || strings.Contains(rec.Body.String(), "Projeto A") {
		t.Fatalf("unreadable lock: status %d body %s, want 409 without data", rec.Code, rec.Body.String())
	}
	t.Setenv("CERNE_SERVE_PROJECTS", "proj-y="+origin)
	if rec := doMP(t, h, http.MethodGet, "/api/projects/proj-y/state", "", mpSecret, nil); rec.Code != http.StatusOK {
		t.Fatalf("configured fallback: status %d %s", rec.Code, rec.Body.String())
	}
}

// The origin check is remembered per clone for the TTL (F-0434).
func TestServer_OriginCheckIsCachedForTTL(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_SERVE_PULL_TTL", "1h")
	h := NewMultiProject(mpSecret)
	origin := seedBareBackend(t, "Projeto A")
	if rec := doMP(t, h, http.MethodPut, "/api/projects/proj-z", regBody(t, origin), mpSecret, nil); rec.Code != http.StatusOK {
		t.Fatalf("register: %d", rec.Code)
	}
	get := func() int {
		return doMP(t, h, http.MethodGet, "/api/projects/proj-z/state", "", mpSecret, nil).Code
	}
	if get() != http.StatusOK {
		t.Fatal("first read must pass")
	}
	repoDir, _, _ := projectPaths("proj-z")
	cfg := filepath.Join(repoDir, ".git", "config")
	raw, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, []byte(strings.ReplaceAll(string(raw), origin, origin+"-other")), 0o644); err != nil {
		t.Fatal(err)
	}
	if get() != http.StatusOK {
		t.Fatal("within the TTL the verdict is reused (no second read of .git/config)")
	}
	originChecks.Delete(repoDir)
	if get() != http.StatusConflict {
		t.Fatal("after the verdict expires a swapped origin is refused")
	}
}

// TestServeRefusesMalformedPID pins B-0045: the mux decodes %2F inside a
// segment, so {pid} reaches the cache path as "../../escape". A pid outside
// the project id shape is a 404 before it becomes a path: nothing is cloned
// or created outside the cache root, on the register route and the data
// routes alike.
func TestServeRefusesMalformedPID(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", filepath.Join(base, "cache", "root"))
	t.Setenv(serveProjectsEnv, "")
	origin := seedBareBackend(t, "Escape")
	h := NewMultiProject("s3cret")
	// A registered project, so a traversal that lands on its directory would
	// read it: the 404 below is the shape check's, not "not registered".
	if rec := doMP(t, h, http.MethodPut, "/api/projects/acme", `{"backend":"`+origin+`"}`, "s3cret", nil); rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/projects/acme = %d %s, want 200", rec.Code, rec.Body.String())
	}
	refused := func(method, target, body string) {
		t.Helper()
		rec := doMP(t, h, method, target, body, "s3cret", nil)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not a project id") {
			t.Errorf("%s %s = %d %s, want 404 not a project id", method, target, rec.Code, rec.Body.String())
		}
	}
	for _, pid := range []string{"..%2F..%2Fescape", "%2E%2E", "a%2Fb", "a%5Cb", "a.b", strings.Repeat("a", 129)} {
		refused(http.MethodPut, "/api/projects/"+pid, `{"backend":"`+origin+`"}`)
		refused(http.MethodGet, "/api/projects/"+pid+"/state", "")
	}
	refused(http.MethodGet, "/api/projects/x%2F..%2Facme/state", "") // resolves to acme's clone without the check
	if _, err := os.Stat(filepath.Join(base, "escape")); !os.IsNotExist(err) {
		t.Fatalf("a malformed pid created a directory outside the cache root: stat err = %v", err)
	}
}

// TestServeAcceptsProjectIDShapes: a slug (cerne init's project_id), a UUID
// (the hub's project id) and a pid at the 128-character limit register.
func TestServeAcceptsProjectIDShapes(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv(serveProjectsEnv, "")
	origin := seedBareBackend(t, "Shapes")
	h := NewMultiProject("s3cret")
	for _, pid := range []string{"acme-hub", "3f2c9a1e-7b4d-4c1a-9e2f-0a1b2c3d4e5f", "Proj_1", strings.Repeat("a", 128)} {
		if rec := doMP(t, h, http.MethodPut, "/api/projects/"+pid, `{"backend":"`+origin+`"}`, "s3cret", nil); rec.Code != http.StatusOK {
			t.Errorf("PUT /api/projects/%s = %d %s, want 200", pid, rec.Code, rec.Body.String())
		}
	}
}

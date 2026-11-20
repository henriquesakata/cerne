package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredBackends(t *testing.T) {
	t.Setenv(serveProjectsEnv, "acme-hub=https://u:t@gitlab.com/acme/hub-docs.git, ledger=/srv/ledger-docs.git")
	got := configuredBackends()
	if got["acme-hub"] != "https://u:t@gitlab.com/acme/hub-docs.git" {
		t.Fatalf("acme-hub = %q", got["acme-hub"])
	}
	if got["ledger"] != "/srv/ledger-docs.git" {
		t.Fatalf("ledger = %q", got["ledger"])
	}
}

func TestConfiguredBackendsIgnoresJunk(t *testing.T) {
	t.Setenv(serveProjectsEnv, " , =nourl, nopid=, ok=/srv/x.git")
	got := configuredBackends()
	if len(got) != 1 || got["ok"] != "/srv/x.git" {
		t.Fatalf("got %v, want only ok", got)
	}
}

func TestConfiguredBackendsUnsetIsNil(t *testing.T) {
	t.Setenv(serveProjectsEnv, "")
	if got := configuredBackends(); got != nil {
		t.Fatalf("got %v, want nil when unset", got)
	}
}

// TestServeAutoRegistersConfiguredProject: a read for a declared project whose
// cache is gone (fresh pod, emptyDir) clones the backend and serves, instead
// of 404-ing until a human re-runs the PUT.
func TestServeAutoRegistersConfiguredProject(t *testing.T) {
	_, _, origin := newGitBackend(t) // a real bare repo to clone from
	cache := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	t.Setenv(serveProjectsEnv, "acme-hub="+origin)
	t.Setenv("CERNE_SERVE_PULL_TTL", "0") // the clone is fresh; skip the refresh

	req := localRequest(http.MethodGet, "/api/projects/acme-hub/conventions-export", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	NewMultiProject("s3cret").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	repoDir, _, err := projectPaths("acme-hub")
	if err != nil {
		t.Fatalf("projectPaths: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Fatalf("backend was not cloned: %v", err)
	}
}

// TestServeStill404sUndeclaredProject: auto-registration is scoped to what the
// operator declared — an unknown pid must not reach for anything.
func TestServeStill404sUndeclaredProject(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv(serveProjectsEnv, "")

	req := localRequest(http.MethodGet, "/api/projects/ghost/conventions-export", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	NewMultiProject("s3cret").ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

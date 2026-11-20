package server

import (
	"context"
	"maps"
	"net/http"
	"regexp"
	"strconv"
	"testing"
)

// writeRoutes lists every data route that is not a read, under prefix, its
// {params} filled with a well-formed id so the request reaches the handler.
func writeRoutes(prefix string) [][2]string {
	param := regexp.MustCompile(`\{[^}]+\}`)
	var out [][2]string
	for _, rt := range dataRoutes() {
		if rt.method != http.MethodGet && rt.method != http.MethodHead {
			out = append(out, [2]string{rt.method, "/api" + prefix + param.ReplaceAllString(rt.pattern, "F-0001")})
		}
	}
	return out
}

// foreignWrites are the requests a page that is not the local ui can make: a
// name rebound to 127.0.0.1 (its own Host and Origin, so any body type), a
// cross-site page (its own Origin, a body a form can send) and a bare foreign
// Host.
var foreignWrites = map[string]map[string]string{
	"rebound name":   {"Host": "evil.example:7741", "Origin": "http://evil.example:7741", "Content-Type": "application/json"},
	"foreign origin": {"Host": "127.0.0.1:7717", "Origin": "http://evil.example", "Content-Type": "text/plain"},
	"foreign host":   {"Host": "evil.example:7717"},
}

// isolatedWriteDirs points the host config and cache at temp dirs and returns
// every place a write could land, to compare before and after.
func isolatedWriteDirs(t *testing.T, dirs ...string) []string {
	t.Helper()
	cfg, cache := t.TempDir(), t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", cfg)
	t.Setenv("CERNE_CACHE_DIR", cache)
	return append(dirs, cfg, cache)
}

func treesOf(t *testing.T, dirs []string) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, d := range dirs {
		out = append(out, cloneTree(t, d))
	}
	return out
}

// assertForeignWritesRefused sends every foreign request to every route: each
// is a 403 and nothing in dirs changes.
func assertForeignWritesRefused(t *testing.T, h http.Handler, routes [][2]string, dirs []string) {
	t.Helper()
	before := treesOf(t, dirs)
	for name, hdr := range foreignWrites {
		for _, rt := range routes {
			if rec := doJSONWith(t, h, rt[0], rt[1], "{}", hdr, nil); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s (%s) = %d %s, want 403", rt[0], rt[1], name, rec.Code, rec.Body.String())
			}
		}
	}
	for i, tree := range treesOf(t, dirs) {
		if !maps.Equal(tree, before[i]) {
			t.Errorf("a refused write changed %s", dirs[i])
		}
	}
}

// TestEveryWriteRouteRefusesForeignHostOrOrigin pins B-0043: every write the
// local server routes — not only /settings — refuses a Host that is not the
// loopback and an Origin of another host with 403, writing nothing. The list is
// the route table itself, so a route added later is covered.
func TestEveryWriteRouteRefusesForeignHostOrOrigin(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	dirs := isolatedWriteDirs(t, repoDir, projDir)
	routes := writeRoutes("")
	if len(routes) < 28 {
		t.Fatalf("found %d write routes, the table has at least 28", len(routes))
	}
	assertForeignWritesRefused(t, New(repoDir, projDir, ""), routes, dirs)
}

// TestUIMultiWriteRoutesRefuseForeignHostOrOrigin: the same for `cerne ui`'s
// handler, its project registry writes included.
func TestUIMultiWriteRoutesRefuseForeignHostOrOrigin(t *testing.T) {
	repoDir, projDir, _ := newGitBackendNoClean(t)
	dirs := isolatedWriteDirs(t, repoDir, projDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewUIMulti(ctx, func(string) (string, string, string, string, error) {
		return repoDir, projDir, "proj-a", "Projeto A", nil
	}, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	routes := append(writeRoutes("/projects/proj-a"), [2]string{"POST", "/api/projects"}, [2]string{"DELETE", "/api/projects/proj-a"})
	assertForeignWritesRefused(t, h, routes, dirs)
}

// TestLocalUIWritesPass: the embedded UI's own writes — same-origin on
// localhost or 127.0.0.1, or with no Origin (the CLI, curl) — reach the handler.
func TestLocalUIWritesPass(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	for i, hdr := range []map[string]string{
		{"Host": "localhost:7717", "Origin": "http://localhost:7717", "Content-Type": "application/json"},
		{"Host": "127.0.0.1:7717", "Origin": "http://127.0.0.1:7717", "Content-Type": "application/json"},
		{"Host": "127.0.0.1:7717"},
	} {
		body := `{"title":"Renomeada ` + strconv.Itoa(i) + `"}`
		if rec := doJSONWith(t, h, http.MethodPut, "/api/features/F-0001/title", body, hdr, nil); rec.Code != http.StatusOK {
			t.Errorf("PUT title %v = %d %s, want 200", hdr, rec.Code, rec.Body.String())
		}
	}
}

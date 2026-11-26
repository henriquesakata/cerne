package server

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
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
// localhost or 127.0.0.1 — reach the handler. A write with no Origin is
// refused since B-0091 (TestHumanActWritesNeedThePageOrigin).
func TestLocalUIWritesPass(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	for i, hdr := range []map[string]string{
		{"Host": "localhost:7717", "Origin": "http://localhost:7717", "Content-Type": "application/json"},
		{"Host": "127.0.0.1:7717", "Origin": "http://127.0.0.1:7717", "Content-Type": "application/json"},
	} {
		body := `{"title":"Renomeada ` + strconv.Itoa(i) + `"}`
		if rec := doJSONWith(t, h, http.MethodPut, "/api/features/F-0001/title", body, hdr, nil); rec.Code != http.StatusOK {
			t.Errorf("PUT title %v = %d %s, want 200", hdr, rec.Code, rec.Body.String())
		}
	}
}

// TestHumanActWritesNeedThePageOrigin pins B-0091: the local ui counts a data
// write as the human's act — a bug report born approved, an approve, an
// accept, a human-only setting — so the write needs the page's Origin. With
// none (curl, an agent's script) or another one it is a 403 that names the
// cause, and nothing changes; the page's own Origin passes, and a read needs
// no Origin.
func TestHumanActWritesNeedThePageOrigin(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	dirs := isolatedWriteDirs(t, repoDir, projDir)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real descrito aqui."))
	seedFeature(t, repoDir, "F-0002", "Feature B", domain.StateImplemented)
	commitBackend(t, repoDir)
	h := New(repoDir, projDir, "")
	acts := []struct {
		name, method, target, body string
		ok                         int
	}{
		{"bug report", http.MethodPost, "/api/features", `{"prefix":"B","title":"Relato","context":"o salvar falha"}`, http.StatusCreated},
		{"approve", http.MethodPost, "/api/features/F-0001/transition", `{"action":"approve"}`, http.StatusOK},
		{"accept", http.MethodPost, "/api/features/F-0002/transition", `{"action":"accept"}`, http.StatusOK},
		{"human-only setting", http.MethodPut, "/api/settings/links.suggest", `{"scope":"host","value":true}`, http.StatusOK},
	}
	before := treesOf(t, dirs)
	for _, origin := range []string{"", "http://evil.example"} {
		for _, a := range acts {
			rec := doJSONWith(t, h, a.method, a.target, a.body, map[string]string{"Origin": origin, "Content-Type": "application/json"}, nil)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), errNotFromPage.Error()) {
				t.Errorf("%s (Origin %q) = %d %s, want 403 naming %q", a.name, origin, rec.Code, rec.Body.String(), errNotFromPage)
			}
		}
	}
	for i, tree := range treesOf(t, dirs) {
		if !maps.Equal(tree, before[i]) {
			t.Errorf("a refused human act changed %s", dirs[i])
		}
	}
	for _, a := range acts {
		hdr := map[string]string{"Origin": "http://127.0.0.1:7717", "Content-Type": "application/json"}
		if rec := doJSONWith(t, h, a.method, a.target, a.body, hdr, nil); rec.Code != a.ok {
			t.Errorf("%s from the page = %d %s, want %d", a.name, rec.Code, rec.Body.String(), a.ok)
		}
	}
	if rec := doJSONWith(t, h, http.MethodGet, "/api/state", "", map[string]string{"Origin": ""}, nil); rec.Code != http.StatusOK {
		t.Errorf("GET /api/state with no Origin = %d, want 200", rec.Code)
	}
}

// TestEveryWriteRouteRefusesNoOrigin pins B-0091 over the route table: every
// write the local ui routes, but the ones roomClientWrites lists, refuses a
// request with no Origin and writes nothing — a route added later is covered,
// `cerne ui`'s own update button included.
func TestEveryWriteRouteRefusesNoOrigin(t *testing.T) {
	param := regexp.MustCompile(`\{[^}]+\}`)
	exempt := map[[2]string]bool{}
	for _, w := range roomClientWrites {
		method, pattern, _ := strings.Cut(w, " ")
		for _, prefix := range []string{"", "/projects/proj-a"} {
			exempt[[2]string{method, "/api" + prefix + param.ReplaceAllString(pattern, "F-0001")}] = true
		}
	}
	repoDir, projDir, _ := newGitBackendNoClean(t)
	dirs := isolatedWriteDirs(t, repoDir, projDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	multi, err := NewUIMulti(ctx, func(string) (string, string, string, string, error) {
		return repoDir, projDir, "proj-a", "Projeto A", nil
	}, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	before := treesOf(t, dirs)
	for _, s := range []struct {
		h      http.Handler
		routes [][2]string
	}{
		{New(repoDir, projDir, ""), writeRoutes("")},
		{multi, append(writeRoutes("/projects/proj-a"), [2]string{"POST", "/api/update"})},
	} {
		for _, rt := range s.routes {
			if exempt[rt] {
				continue
			}
			rec := doJSONWith(t, s.h, rt[0], rt[1], "{}", map[string]string{"Origin": "", "Content-Type": "application/json"}, nil)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), errNotFromPage.Error()) {
				t.Errorf("%s %s with no Origin = %d %s, want 403 naming %q", rt[0], rt[1], rec.Code, rec.Body.String(), errNotFromPage)
			}
		}
	}
	for i, tree := range treesOf(t, dirs) {
		if !maps.Equal(tree, before[i]) {
			t.Errorf("a refused write changed %s", dirs[i])
		}
	}
}

// TestPageWrite pins B-0091's rule with errors.Is: no Origin, or another
// one, is errNotFromPage — except a no-Origin write to a route cerne's own
// processes call: another cerne ui registering, deregistering or restarting
// this panel (N-0113), and the room client.
func TestPageWrite(t *testing.T) {
	for _, c := range []struct {
		name, origin, pattern string
		refused               bool
	}{
		{"the page", "http://127.0.0.1:7717", "POST /api/features", false},
		{"no Origin", "", "POST /api/features", true},
		{"foreign Origin", "http://evil.example", "POST /api/projects/{pid}/features/{id}/transition", true},
		{"no Origin, human-only setting", "", "PUT /api/projects/{pid}/settings/{id}", true},
		{"panel registry, no Origin", "", "POST /api/projects", false},
		{"panel deregister, no Origin", "", "DELETE /api/projects/{pid}", false},
		{"panel restart, no Origin", "", "POST /api/restart", false},
		{"room client, no Origin", "", "POST /api/projects/{pid}/rooms/{id}/messages", false},
		{"room client, single project, no Origin", "", "POST /api/rooms/{id}/items/{itemID}/resolve", false},
		{"room, foreign Origin", "http://evil.example", "POST /api/rooms", true},
		{"a room route the client does not call, no Origin", "", "POST /api/projects/{pid}/rooms/{id}/archive", true},
		{"a rooms segment elsewhere, no Origin", "", "POST /api/projects/{pid}/features/{id}/rooms", true},
	} {
		r := localRequest(http.MethodPost, "/api/x", nil)
		r.Header.Del("Origin")
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if err := pageWrite(r, c.pattern); errors.Is(err, errNotFromPage) != c.refused {
			t.Errorf("%s: pageWrite = %v, want refused %v", c.name, err, c.refused)
		}
	}
	table := map[string]bool{}
	for _, rt := range dataRoutes() {
		table[rt.method+" "+rt.pattern] = true
	}
	for _, w := range roomClientWrites {
		if !table[w] {
			t.Errorf("roomClientWrites lists %q, which the route table does not have", w)
		}
	}
}

// TestCLIWritesWithoutOriginStillLand (B-0091): the writes cerne's own
// processes send with no Origin reach their handlers — the room client
// opening a room, another cerne ui deregistering its project.
func TestCLIWritesWithoutOriginStillLand(t *testing.T) {
	noOrigin := map[string]string{"Origin": ""}
	if rec := doJSONWith(t, newRoomServer(t, "proj-b0091"), http.MethodPost, "/api/rooms", `{"topic":"t"}`, noOrigin, nil); rec.Code != http.StatusCreated {
		t.Errorf("POST /api/rooms with no Origin = %d %s, want 201", rec.Code, rec.Body.String())
	}
	repoDir, projDir, _ := newGitBackendNoClean(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewUIMulti(ctx, func(string) (string, string, string, string, error) {
		return repoDir, projDir, "proj-a", "Projeto A", nil
	}, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	if rec := doJSONWith(t, h, http.MethodDelete, "/api/projects/proj-a", "", noOrigin, nil); rec.Code != http.StatusOK {
		t.Errorf("DELETE /api/projects/proj-a with no Origin = %d %s, want 200", rec.Code, rec.Body.String())
	}
}

// readRoutes lists every GET data route under prefix, its {params} filled as
// writeRoutes does, plus the routes mounted outside the data table.
func readRoutes(prefix string) []string {
	param := regexp.MustCompile(`\{[^}]+\}`)
	out := []string{"/", "/metrics", "/api/debug/timings"}
	for _, rt := range dataRoutes() {
		if rt.method == http.MethodGet {
			out = append(out, "/api"+prefix+param.ReplaceAllString(rt.pattern, "F-0001"))
		}
	}
	return out
}

// foreignHosts are Host headers a page that is not the local ui can send: a
// rebound name, names that only start or end like the loopback, a bare name
// and none at all.
var foreignHosts = []string{"evil.example:7741", "localhost.evil.example:7717", "127.0.0.1.nip.io:7717", "evil.example", ""}

// assertForeignReadsRefused sends GET and HEAD with every foreign Host to
// every route: each is a 403.
func assertForeignReadsRefused(t *testing.T, h http.Handler, routes []string) {
	t.Helper()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, host := range foreignHosts {
			for _, target := range routes {
				if rec := doJSONWith(t, h, method, target, "", map[string]string{"Host": host}, nil); rec.Code != http.StatusForbidden {
					t.Errorf("%s %s (Host %q) = %d, want 403", method, target, host, rec.Code)
				}
			}
		}
	}
}

// assertForeignStreamRefused: the live stream refuses a foreign Host too. The
// request carries a deadline, so a stream that answers ends the test in
// seconds instead of hanging it.
func assertForeignStreamRefused(t *testing.T, h http.Handler, target string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := localRequest(http.MethodGet, target, nil).WithContext(ctx)
	req.Host = "evil.example:7741"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET %s (foreign host) = %d, want 403", target, rec.Code)
	}
}

// TestEveryReadRouteRefusesForeignHost pins B-0044: a name rebound to
// 127.0.0.1 reads nothing from the local ui — every GET and HEAD with a Host
// that is not the loopback is a 403, like the writes (B-0043).
func TestEveryReadRouteRefusesForeignHost(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	isolatedWriteDirs(t)
	t.Setenv("CERNE_PPROF", "1") // mount /debug/pprof/ too
	routes := readRoutes("")
	if len(routes) < 30 {
		t.Fatalf("found %d read routes, the table has at least 30", len(routes))
	}
	assertForeignReadsRefused(t, New(repoDir, projDir, ""), append(routes, "/debug/pprof/"))
}

// TestUIMultiReadRoutesRefuseForeignHost: the same for `cerne ui`'s handler,
// its project registry and live stream included.
func TestUIMultiReadRoutesRefuseForeignHost(t *testing.T) {
	repoDir, projDir, _ := newGitBackendNoClean(t)
	isolatedWriteDirs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := NewUIMulti(ctx, func(string) (string, string, string, string, error) {
		return repoDir, projDir, "proj-a", "Projeto A", nil
	}, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	assertForeignReadsRefused(t, h, append(readRoutes("/projects/proj-a"), "/api/projects"))
	assertForeignStreamRefused(t, h, "/api/stream")
}

// TestLocalUIReadsPass: a loopback Host reads, whatever the Origin — the
// Origin check stays on the writes (B-0044), a cross-site read is the
// browser's CORS to refuse.
func TestLocalUIReadsPass(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "")
	for _, hdr := range []map[string]string{
		{"Host": "localhost:7717"},
		{"Host": "127.0.0.1:7717", "Origin": "http://evil.example"},
		{"Host": "[::1]:7717"},
	} {
		if rec := doJSONWith(t, h, http.MethodGet, "/api/state", "", hdr, nil); rec.Code != http.StatusOK {
			t.Errorf("GET /api/state %v = %d %s, want 200", hdr, rec.Code, rec.Body.String())
		}
	}
}

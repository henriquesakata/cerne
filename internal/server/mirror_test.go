package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// mirrorFixture is a project with two fake trackers, two mirrored cards (one on
// each), an imported idea, and the mirror switched on (on=false leaves it off).
func mirrorFixture(t *testing.T, on bool) (h http.Handler, repoDir, projDir string, fakes [2]*trackertest.Fake) {
	t.Helper()
	repoDir, projDir, _ = newGitBackend(t)
	t.Setenv("CERNE_ACTOR", "")
	os.Unsetenv("CERNE_ACTOR")
	fakes = [2]*trackertest.Fake{{Name: "a"}, {Name: "b"}}
	trackertest.Register(t, fakes[0], fakes[1])
	rc := "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	var recs []any
	for _, f := range fakes {
		recs = append(recs, map[string]any{"name": f.Name, "kind": "fake", "url": "https://fake.test/o/" + f.Name})
	}
	if err := project.SetSettingValue(projDir, "mirror.trackers", project.ScopeProject, recs); err != nil {
		t.Fatal(err)
	}
	if on {
		if err := project.SetSettingValue(projDir, "mirror.enabled", project.ScopeProject, true); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []string{"F-0001", "F-0002"} {
		f := fakes[i]
		r, err := f.Create(context.Background(), tracker.Item{CardID: id, State: domain.StateCandidate, Title: "T", Summary: "S"})
		if err != nil {
			t.Fatal(err)
		}
		state := domain.StateCandidate
		if id == "F-0002" {
			state = domain.StateDone
		}
		seedFeature(t, repoDir, id, "Card "+id, state)
		card, err := store.LoadFeature(repoDir, id)
		if err != nil {
			t.Fatal(err)
		}
		card.Mirror = &store.MirrorRef{Tracker: f.Name, Kind: "fake", ID: r.ID, URL: r.URL}
		if err := store.SaveFeature(repoDir, card); err != nil {
			t.Fatal(err)
		}
	}
	idea := store.Idea{ID: "I-0001", Title: "Crash on start", State: domain.IdeaActive,
		Mirror: &store.MirrorRef{Tracker: "b", Kind: "fake", ID: "9", URL: "https://fake.test/b/issues/9"}}
	if err := store.SaveIdea(repoDir, idea); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed mirror"); err != nil {
		t.Fatal(err)
	}
	for _, f := range fakes {
		f.Calls = nil
	}
	return New(repoDir, projDir, ""), repoDir, projDir, fakes
}

func do(h http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMirrorOverviewRoute(t *testing.T) {
	h, _, _, fakes := mirrorFixture(t, true)
	rec := do(h, "GET", "/api/mirror", nil)
	if rec.Code != 200 {
		t.Fatalf("GET /api/mirror = %d: %s", rec.Code, rec.Body)
	}
	var v struct {
		Enabled  bool `json:"enabled"`
		Trackers []struct{ Name, Kind, URL string }
		Cards    []struct {
			ID, Title, State, Tracker string
			Ref                       struct{ ID, URL string }
		}
		Ideas []struct{ ID, Title, State, Tracker, URL, IssueID string }
		Last  *json.RawMessage `json:"lastSync"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if !v.Enabled || len(v.Trackers) != 2 || len(v.Cards) != 2 || len(v.Ideas) != 1 || v.Last != nil {
		t.Fatalf("view = %s", rec.Body)
	}
	if c := v.Cards[1]; c.ID != "F-0002" || c.Tracker != "b" || c.State != "done" || c.Ref.URL == "" {
		t.Errorf("card = %+v", c)
	}
	if i := v.Ideas[0]; i.Tracker != "b" || i.State != "active" || i.URL == "" || i.IssueID != "9" {
		t.Errorf("idea = %+v", i)
	}
	if len(fakes[0].Calls)+len(fakes[1].Calls) != 0 {
		t.Fatal("opening the tab called a tracker")
	}
}

func TestMirrorOverviewRouteOff(t *testing.T) {
	h, _, _, _ := mirrorFixture(t, false)
	rec := do(h, "GET", "/api/mirror", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"enabled": false`) || !strings.Contains(rec.Body.String(), `"cards": []`) {
		t.Fatalf("GET /api/mirror = %d: %s", rec.Code, rec.Body)
	}
}

func TestMirrorCheckRoute(t *testing.T) {
	h, _, _, fakes := mirrorFixture(t, true)
	// A closed card whose issue is still open is a drift; an agent may ask.
	if rec := do(h, "POST", "/api/mirror/check", map[string]string{"Content-Type": "application/json"}); rec.Code != 200 {
		t.Fatalf("human check = %d: %s", rec.Code, rec.Body)
	}
	t.Setenv("CERNE_ACTOR", "agent")
	rec := do(h, "POST", "/api/mirror/check", map[string]string{"Content-Type": "application/json"})
	if rec.Code != 200 {
		t.Fatalf("agent check = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Results []struct{ ID, Status, Reason, Detail string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Results) != 2 {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
	if r := out.Results[0]; r.ID != "F-0001" || r.Status != "ok" {
		t.Errorf("F-0001 = %+v", r)
	}
	if r := out.Results[1]; r.ID != "F-0002" || r.Status != "drift" || r.Reason != "open-but-card-closed" {
		t.Errorf("F-0002 = %+v", r)
	}
	for _, f := range fakes {
		for _, c := range f.Calls {
			if c.Op != "ListOpen" {
				t.Fatalf("%s got %s, want only ListOpen", f.Name, c.Op)
			}
		}
	}
	// Another origin is refused; no Origin (CLI, curl) passes.
	if rec := do(h, "POST", "/api/mirror/check", map[string]string{"Origin": "http://evil.test"}); rec.Code != 403 {
		t.Errorf("cross-origin = %d, want 403", rec.Code)
	}
	if rec := do(h, "POST", "/api/mirror/check", nil); rec.Code != 200 {
		t.Errorf("no Origin = %d, want 200", rec.Code)
	}
}

func TestMirrorCheckRouteOff(t *testing.T) {
	h, _, _, _ := mirrorFixture(t, false)
	rec := do(h, "POST", "/api/mirror/check", nil)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "mirror is off") {
		t.Fatalf("check off = %d: %s", rec.Code, rec.Body)
	}
}

func TestMirrorCommitsRoute(t *testing.T) {
	h, _, projDir, _ := mirrorFixture(t, true)
	// No repository yet.
	rec := do(h, "GET", "/api/mirror/commits/F-0001", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"repo": false`) || !strings.Contains(rec.Body.String(), `"commits": []`) {
		t.Fatalf("no repo = %d: %s", rec.Code, rec.Body)
	}
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", projDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("config", "commit.gpgsign", "false")
	git("commit", "-q", "--allow-empty", "-m", "feat: x\n\nCerne: F-0001")
	rec = do(h, "GET", "/api/mirror/commits/F-0001", nil)
	var out struct {
		Repo    bool
		Commits []struct{ SHA, Subject, URL string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 || !out.Repo || len(out.Commits) != 1 || out.Commits[0].Subject != "feat: x" {
		t.Fatalf("commits = %d: %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/mirror/commits/nope", nil); rec.Code != 400 {
		t.Errorf("bad id = %d, want 400", rec.Code)
	}
}

// The tab reads: no mirror route but GET and the check, and none on the remote server.
func TestMirrorRoutesReadOnly(t *testing.T) {
	seen := 0
	for _, r := range dataRoutes() {
		if !strings.HasPrefix(r.pattern, "/mirror") {
			continue
		}
		seen++
		if r.remote {
			t.Errorf("%s %s is remote, want local-only", r.method, r.pattern)
		}
		if r.method != "GET" && !(r.method == "POST" && r.pattern == "/mirror/check") {
			t.Errorf("%s %s writes; the Mirror tab is read-only", r.method, r.pattern)
		}
	}
	if seen != 3 {
		t.Errorf("mirror routes = %d, want 3", seen)
	}
}

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/room"
	"github.com/henriquesakata/cerne/internal/store"
)

// newAgentsServer is a channel server whose F-0001 was opened by s-tl and
// implemented by s-dev, with s-tl's transcript in a private Claude root.
func newAgentsServer(t *testing.T, projectID string) (h http.Handler, cacheDir string) {
	t.Helper()
	h, repoDir, cacheDir := newChannelServer(t, projectID)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "x", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cost.AppendMark(cacheDir, cost.Bind{Session: "s-tl", Entity: "F-0001", Role: "opened", Harness: cost.HarnessClaudeCode, TS: now}); err != nil {
		t.Fatal(err)
	}
	if err := cost.AppendBind(cacheDir, cost.Bind{Session: "s-dev", Entity: "F-0001", Harness: cost.HarnessClaudeCode, TS: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".claude", "projects", "-w")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s-tl.jsonl"), []byte(`{"type":"custom-title","customTitle":"TL"}`+"\n"+`{"type":"user","cwd":"/w/tl","gitBranch":"main"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return h, cacheDir
}

// TestFeatureAgentsRoute (F-0486 crit. 2, 4): the page reads the card's
// sessions, each with what it did and where it is; an unknown card is 404.
func TestFeatureAgentsRoute(t *testing.T) {
	h, _ := newAgentsServer(t, "proj-agents")
	var got struct {
		Sessions []struct {
			Session string `json:"session"`
			Roles   []struct {
				Kind string `json:"kind"`
			} `json:"roles"`
			Info struct {
				Title string `json:"title"`
				Cwd   string `json:"cwd"`
				Found bool   `json:"found"`
			} `json:"info"`
		} `json:"sessions"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/features/F-0001/agents", "", &got)
	if rec.Code != http.StatusOK || len(got.Sessions) != 2 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	tl, dev := got.Sessions[0], got.Sessions[1]
	if tl.Session != "s-tl" || tl.Roles[0].Kind != "opened" || tl.Info.Title != "TL" || tl.Info.Cwd != "/w/tl" || !tl.Info.Found {
		t.Fatalf("s-tl = %+v", tl)
	}
	if dev.Session != "s-dev" || dev.Roles[0].Kind != "implemented" || dev.Info.Found {
		t.Fatalf("s-dev = %+v, want implemented with no transcript here", dev)
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/features/F-0099/agents", "", &map[string]any{}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown card: status %d", rec.Code)
	}
}

// TestFeatureAgentsRouteFindsCursorSession (F-0513 crit. 4): the agents panel
// finds a Cursor session in its agent-transcripts, with its subagent.
func TestFeatureAgentsRouteFindsCursorSession(t *testing.T) {
	h, _ := newAgentsServer(t, "proj-agents-cursor")
	cursorRoot := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", cursorRoot)
	dir := filepath.Join(cursorRoot, "projects", "Users-w", "agent-transcripts", "s-dev")
	if err := os.MkdirAll(filepath.Join(dir, "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(dir, "s-dev.jsonl"), filepath.Join(dir, "subagents", "sub-1.jsonl")} {
		if err := os.WriteFile(f, []byte(`{"role":"user","message":{"content":"x"}}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got struct {
		Sessions []struct {
			Session string `json:"session"`
			Info    struct {
				Found  bool              `json:"found"`
				Agents map[string]string `json:"agents"`
			} `json:"info"`
		} `json:"sessions"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/features/F-0001/agents", "", &got)
	if rec.Code != http.StatusOK || len(got.Sessions) != 2 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	dev := got.Sessions[1]
	if _, ok := dev.Info.Agents["sub-1"]; dev.Session != "s-dev" || !dev.Info.Found || !ok {
		t.Fatalf("s-dev = %+v, want found in Cursor's agent-transcripts with subagent sub-1", dev)
	}
}

// TestFeatureAgentsRouteIsLocalOnly (F-0486 crit. 14): the route that serves
// session ids, folders and resume commands never joins `cerne serve`.
func TestFeatureAgentsRouteIsLocalOnly(t *testing.T) {
	for _, r := range dataRoutes() {
		if r.method+" "+r.pattern == "GET /features/{id}/agents" {
			if r.remote {
				t.Fatal("GET /features/{id}/agents is remote, want local-only")
			}
			return
		}
	}
	t.Fatal("GET /features/{id}/agents not routed")
}

// TestChannelPostToCardSessions (F-0486 crit. 7, 12): a post from the card's
// page names its card and only sessions that worked on it — anything else is
// refused unwritten — and the read filters the card's thread.
func TestChannelPostToCardSessions(t *testing.T) {
	h, cacheDir := newAgentsServer(t, "proj-agents-post")
	seedServerChannel(t, cacheDir, "W-0002", "outra conversa", time.Now().UTC())
	for _, bad := range []string{
		`{"body":"x","ref":"F-0001","to":["s-ghost"]}`,
		`{"body":"x","ref":"F-0001","to":[]}`,
		`{"body":"x","to":["s-tl"]}`,
		`{"body":"x","ref":"F-0099","to":["s-tl"]}`,
	} {
		if rec := doJSON(t, h, http.MethodPost, "/api/rooms/channel/messages", bad, &map[string]any{}); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", bad, rec.Code)
		}
	}
	if msgs, _ := room.OpenChannel(filepath.Join(cacheDir, "rooms")).Messages(); len(msgs) != 1 {
		t.Fatalf("refused posts were written: %+v", msgs)
	}
	var posted map[string]any
	if rec := doJSON(t, h, http.MethodPost, "/api/rooms/channel/messages", `{"body":"corrige?","ref":"F-0001","to":["s-dev"]}`, &posted); rec.Code != http.StatusCreated {
		t.Fatalf("valid post: status %d: %v", rec.Code, posted)
	}
	if _, leaked := posted["to"]; leaked {
		t.Fatalf("post response serves the session ids: %v", posted)
	}
	var thread channelReadJSON
	doJSON(t, h, http.MethodGet, "/api/rooms/channel/messages?ref=F-0001", "", &thread)
	if len(thread.Messages) != 1 || thread.Messages[0].Body != "corrige?" || len(thread.Messages[0].Targets) != 1 {
		t.Fatalf("F-0001 thread = %+v", thread.Messages)
	}
	var all channelReadJSON
	doJSON(t, h, http.MethodGet, "/api/rooms/channel/messages", "", &all)
	if len(all.Messages) != 2 {
		t.Fatalf("whole channel = %d messages, want both", len(all.Messages))
	}
}

// TestChannelRoutesAreLocalOnly (F-0486 crit. 14, critic finding 2): the
// channel routes serve short session ids in targets and nicks; none of them
// joins `cerne serve`.
func TestChannelRoutesAreLocalOnly(t *testing.T) {
	seen := 0
	for _, r := range dataRoutes() {
		if strings.HasPrefix(r.pattern, "/rooms") {
			seen++
			if r.remote {
				t.Errorf("%s %s is remote, want local-only", r.method, r.pattern)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no /rooms route found")
	}
}

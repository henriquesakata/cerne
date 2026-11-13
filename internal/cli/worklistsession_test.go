package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// listFixture: four active works (F-0361 crit. 8) — W-0001 bound 2 min ago,
// W-0002 bound 3 days ago, W-0003 unbound with a local actor, W-0004 unbound
// with an actor from another machine.
func listFixture(t *testing.T) (projDir, cacheDir string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	cacheDir = filepath.Dir(repoDir)
	host, _ := os.Hostname()
	now := time.Now().UTC()
	for _, w := range []store.Work{
		{ID: "W-0001", Title: "Quente", Actor: "agent:u@" + host},
		{ID: "W-0002", Title: "Velho", Actor: "u@" + host},
		{ID: "W-0003", Title: "Sem vinculo", Actor: "agent:u@" + host},
		{ID: "W-0004", Title: "Remoto", Actor: "agent:u@other-box"},
	} {
		w.State, w.CreatedAt, w.UpdatedAt = domain.WorkActive, now, now
		if err := store.SaveWork(repoDir, w); err != nil {
			t.Fatal(err)
		}
	}
	bind := func(sess, id string, ago time.Duration) {
		if err := app.BindSession(cacheDir, sess, id); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-ago)
		if err := os.Chtimes(filepath.Join(cacheDir, "sessions", sess), mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	bind("aaaaaaaa-1111", "W-0001", 150*time.Second)
	bind("bbbbbbbb-2222", "W-0002", 72*time.Hour+time.Minute)
	return
}

func sessionsSnapshot(t *testing.T, cacheDir string) map[string]time.Time {
	t.Helper()
	out := map[string]time.Time{}
	es, _ := os.ReadDir(filepath.Join(cacheDir, "sessions"))
	for _, e := range es {
		fi, _ := e.Info()
		out[e.Name()] = fi.ModTime()
	}
	return out
}

// TestWorkListSessionText (F-0361 crit. 3, 4, 6, 8) — also saves the text for the Gauntlet.
func TestWorkListSessionText(t *testing.T) {
	projDir, cacheDir := listFixture(t)
	// Non-live bindings that only CurrentBinding/LiveBindings may clean (crit. 6).
	repoDir := filepath.Join(cacheDir, "repo")
	dw, err := store.LoadWork(repoDir, "W-0004")
	if err != nil {
		t.Fatal(err)
	}
	dw.ID, dw.State = "W-0009", domain.WorkDone
	if err := store.SaveWork(repoDir, dw); err != nil {
		t.Fatal(err)
	}
	for sess, id := range map[string]string{"dead-done": "W-0009", "dead-nof": "F-9999"} {
		if err := app.BindSession(cacheDir, sess, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "sessions", "corrupt"), []byte("\x00garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "sessions", "empty"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before := sessionsSnapshot(t, cacheDir)
	if len(before) != 6 {
		t.Fatalf("fixture sessions = %d, want 6", len(before))
	}
	out, err := execCerne(t, "work", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("work list: %v\n%s", err, out)
	}
	if dir := os.Getenv("CERNE_F0361_FIXTURE_OUT"); dir != "" {
		_ = os.WriteFile(dir, []byte(out), 0o644)
	}
	line := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			line[f[0]] = l
		}
	}
	for id, want := range map[string]string{
		"W-0001": "session: aaaaaaaa signal 2m ago",
		"W-0002": "session: bbbbbbbb signal 3d ago",
		"W-0003": "session: none on this machine",
		"W-0004": "session: on another machine (other-box)",
	} {
		if !strings.Contains(line[id], want) {
			t.Errorf("%s line missing %q:\n%s", id, want, line[id])
		}
	}
	if after := sessionsSnapshot(t, cacheDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("work list changed sessions/: %v -> %v", before, after)
	}
}

// TestWorkListSessionMultiAndFeatureBinding (crit. 3): an F- binding lands on the
// auto work; several sessions are joined newest first.
func TestWorkListSessionMultiAndFeatureBinding(t *testing.T) {
	projDir, cacheDir := listFixture(t)
	repoDir := filepath.Join(cacheDir, "repo")
	w, err := store.LoadWork(repoDir, "W-0003")
	if err != nil {
		t.Fatal(err)
	}
	w.Auto, w.Refs = true, []string{"F-0100"}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		sess string
		ago  time.Duration
	}{{"cccccccc-1", 3 * time.Hour}, {"dddddddd-1", 10 * time.Minute}} {
		if err := app.BindSession(cacheDir, s.sess, "F-0100"); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-s.ago)
		if err := os.Chtimes(filepath.Join(cacheDir, "sessions", s.sess), mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	out, err := execCerne(t, "work", "list", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "session: dddddddd signal 10m ago, cccccccc signal 3h ago") {
		t.Fatalf("want newest-first joined sessions:\n%s", out)
	}
}

// TestWorkListSessionJSON (crit. 5): works unchanged, sessions present only with bindings.
func TestWorkListSessionJSON(t *testing.T) {
	projDir, cacheDir := listFixture(t)
	out, err := execCerne(t, "work", "list", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Works    json.RawMessage `json:"works"`
		Sessions map[string][]struct {
			Session    string    `json:"session"`
			LastSignal time.Time `json:"last_signal"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if l := got.Sessions["W-0001"]; len(l) != 1 || l[0].Session != "aaaaaaaa-1111" || l[0].LastSignal.Location() != time.UTC {
		t.Fatalf("sessions = %+v", got.Sessions)
	}
	if _, ok := got.Sessions["W-0003"]; ok {
		t.Fatal("unbound work must not appear in sessions")
	}
	// works must be byte-for-byte what the pre-F-0361 shape emitted.
	var works []store.Work
	if err := json.Unmarshal(got.Works, &works); err != nil || len(works) != 4 {
		t.Fatalf("works: %v %d", err, len(works))
	}
	var old bytes.Buffer
	enc := json.NewEncoder(&old)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		Works []store.Work `json:"works"`
	}{works}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, strings.TrimSuffix(old.String(), "\n}\n")) {
		t.Fatalf("works part drifted from the old shape:\n%s\nvs\n%s", out, old.String())
	}
	// no bindings at all: key omitted.
	if err := os.RemoveAll(filepath.Join(cacheDir, "sessions")); err != nil {
		t.Fatal(err)
	}
	out, err = execCerne(t, "work", "list", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"sessions"`) {
		t.Fatalf("sessions must be omitted when empty:\n%s", out)
	}
}

// TestWorkListSessionHostFromRealActor (crit. 4): an actor built by `work
// start` is local; a case-folded host still is; so is an actor with no host.
func TestWorkListSessionHostFromRealActor(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Real actor", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Actor, "@") {
		t.Skipf("actor %q carries no host on this machine", w.Actor)
	}
	check := func(actor string) {
		t.Helper()
		w.Actor = actor
		if err := store.SaveWork(repoDir, w); err != nil {
			t.Fatal(err)
		}
		out, err := execCerne(t, "work", "list", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "session: none on this machine") {
			t.Fatalf("actor %q: want local, got:\n%s", actor, out)
		}
	}
	check(w.Actor)
	i := strings.LastIndex(w.Actor, "@")
	check(w.Actor[:i+1] + strings.ToUpper(w.Actor[i+1:]))
	check("agent")
}

// TestWorkListSessionAgent (F-0376, the F-0361 signal per agent): a
// subagent's binding shows as its session plus the agent, in text and JSON.
func TestWorkListSessionAgent(t *testing.T) {
	projDir, cacheDir := listFixture(t)
	if err := app.BindSessionAgent(cacheDir, "aaaaaaaa-1111", "a17d7f8bcc197381c", "", "W-0002"); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "work", "list", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "session: aaaaaaaa agent a17d7f8b signal 0m ago, bbbbbbbb signal 3d ago") {
		t.Fatalf("want the subagent as session + agent:\n%s", out)
	}
	if !strings.Contains(out, "session: aaaaaaaa signal 2m ago") {
		t.Fatalf("main thread's line drifted:\n%s", out)
	}
	out, err = execCerne(t, "work", "list", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Sessions map[string][]map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	l := got.Sessions["W-0002"]
	if len(l) != 2 || l[0]["session"] != "aaaaaaaa-1111" || l[0]["agent"] != "a17d7f8bcc197381c" {
		t.Fatalf("W-0002 sessions = %+v", l)
	}
	if _, ok := l[1]["agent"]; ok {
		t.Fatalf("a main-thread session carries no agent key: %+v", l[1])
	}
}

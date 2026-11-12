package cli

import (
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
	before := sessionsSnapshot(t, cacheDir)
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
	w, _ := store.LoadWork(repoDir, "W-0003")
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
		_ = os.Chtimes(filepath.Join(cacheDir, "sessions", s.sess), mt, mt)
	}
	out, _ := execCerne(t, "work", "list", "--dir", projDir)
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
	// works byte-for-byte what the old shape would emit.
	var works []store.Work
	if err := json.Unmarshal(got.Works, &works); err != nil || len(works) != 4 {
		t.Fatalf("works: %v %d", err, len(works))
	}
	want, _ := json.MarshalIndent(works, "  ", "  ")
	if string(got.Works) != string(want) {
		t.Fatalf("works encoding drifted:\n%s\nvs\n%s", got.Works, want)
	}
	// no bindings at all: key omitted.
	if err := os.RemoveAll(filepath.Join(cacheDir, "sessions")); err != nil {
		t.Fatal(err)
	}
	out, _ = execCerne(t, "work", "list", "--json", "--dir", projDir)
	if strings.Contains(out, `"sessions"`) {
		t.Fatalf("sessions must be omitted when empty:\n%s", out)
	}
}

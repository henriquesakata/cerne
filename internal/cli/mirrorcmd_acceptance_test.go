package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// mirrorCLIProject inits a project, registers the fakes and configures one
// kind "fake" tracker per fake, mirror on, through the settings registry.
func mirrorCLIProject(t *testing.T, fakes ...*trackertest.Fake) (projDir, repoDir string) {
	t.Helper()
	t.Setenv("CERNE_ACTOR", "")
	os.Unsetenv("CERNE_ACTOR")
	t.Setenv("COPILOT_CLI", "")
	projDir, _, repoDir = setupProject(t)
	trackertest.Register(t, fakes...)
	setMirrorTrackers(t, projDir, fakes...)
	if err := project.SetSettingValue(projDir, "mirror.enabled", project.ScopeProject, true); err != nil {
		t.Fatalf("set mirror.enabled: %v", err)
	}
	return projDir, repoDir
}

func setMirrorTrackers(t *testing.T, projDir string, fakes ...*trackertest.Fake) {
	t.Helper()
	var recs []any
	for _, f := range fakes {
		recs = append(recs, map[string]any{"name": f.Name, "kind": "fake", "url": "https://fake.test/o/" + f.Name})
	}
	if err := project.SetSettingValue(projDir, "mirror.trackers", project.ScopeProject, recs); err != nil {
		t.Fatalf("set mirror.trackers: %v", err)
	}
}

// F-0449 crit. 4 and 8: cerne feature mirror publishes a card to the first
// tracker, or the one --tracker names, and prints where; an empty title and an
// agent actor are refused with nothing sent.
func TestFeatureMirrorCmd(t *testing.T) {
	pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
	projDir, repoDir := mirrorCLIProject(t, pub, second)
	mustCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir)
	mustCerne(t, "feature", "new", "Exportar relatório em CSV", "--context", "ctx de teste", "--dir", projDir)
	mustCerne(t, "feature", "new", "Tema escuro no painel", "--context", "ctx de teste", "--dir", projDir)

	out, err := execCerne(t, "feature", "mirror", "F-0001", "--title", "Sign in with Google", "--summary", "Adds Google sign-in.", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature mirror F-0001: %v\n%s", err, out)
	}
	if !strings.Contains(out, "mirrored F-0001 to pub: https://fake.test/pub/issues/1") {
		t.Fatalf("output %q lacks the mirrored line", out)
	}
	if iss := pub.Issues["1"]; iss == nil || iss.Title != "Sign in with Google" {
		t.Fatalf("pub issues = %+v, want the public title", pub.Issues)
	}
	if f, err := store.LoadFeature(repoDir, "F-0001"); err != nil || f.Mirror == nil || f.Mirror.URL != "https://fake.test/pub/issues/1" {
		t.Fatalf("F-0001 mirror = %+v (%v), want the pub issue", f.Mirror, err)
	}

	out, err = execCerne(t, "feature", "mirror", "F-0002", "--title", "CSV export", "--summary", "Exports the report as CSV.", "--tracker", "second", "--dir", projDir)
	if err != nil || !strings.Contains(out, "mirrored F-0002 to second: https://fake.test/second/issues/1") {
		t.Fatalf("feature mirror --tracker second: %v\n%s", err, out)
	}

	calls := len(pub.Calls) + len(second.Calls)
	if _, err := execCerne(t, "feature", "mirror", "F-0003", "--title", "", "--summary", "Dark theme.", "--dir", projDir); !errors.Is(err, app.ErrMirrorText) {
		t.Errorf("empty --title: err = %v, want ErrMirrorText", err)
	}
	t.Setenv("CERNE_ACTOR", "agent")
	if _, err := execCerne(t, "feature", "mirror", "F-0003", "--title", "Dark theme", "--summary", "Dark theme.", "--dir", projDir); !errors.Is(err, app.ErrHumanGate) {
		t.Errorf("agent: err = %v, want ErrHumanGate", err)
	}
	if n := len(pub.Calls) + len(second.Calls); n != calls {
		t.Fatalf("a refused mirror reached a tracker: %+v / %+v", pub.Calls, second.Calls)
	}
}

// F-0449 crit. 6: cerne mirror sync prints each imported idea and a summary
// line, imports nothing the second time, and exits non-zero with the warning
// on stderr when a mirrored card's tracker is gone from the configuration.
func TestMirrorSyncCmd(t *testing.T) {
	pub, other := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "other"}
	projDir, _ := mirrorCLIProject(t, pub, other)
	r := pub.Open("Crash on start", "Steps: run it twice", "")

	stdout, stderr, err := execCerneSplit(t, "mirror", "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("mirror sync: %v\n%s%s", err, stdout, stderr)
	}
	if !regexp.MustCompile(`imported I-\d+ from ` + regexp.QuoteMeta(r.URL)).MatchString(stdout) {
		t.Errorf("stdout lacks the imported line for %s:\n%s", r.URL, stdout)
	}
	if !strings.Contains(stdout, "synced 0 card(s), imported 1 idea(s)") {
		t.Errorf("stdout lacks the summary line:\n%s", stdout)
	}

	stdout, stderr, err = execCerneSplit(t, "mirror", "sync", "--dir", projDir)
	if err != nil || strings.Contains(stdout, "imported I-") || !strings.Contains(stdout, "synced 0 card(s), imported 0 idea(s)") {
		t.Fatalf("second mirror sync: %v, want nothing imported\n%s%s", err, stdout, stderr)
	}

	mustCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir)
	mustCerne(t, "feature", "mirror", "F-0001", "--title", "Sign in with Google", "--summary", "Adds Google sign-in.", "--dir", projDir)
	setMirrorTrackers(t, projDir, other) // pub is gone from the configuration
	stdout, stderr, err = execCerneSplit(t, "mirror", "sync", "--dir", projDir)
	if err == nil {
		t.Fatalf("mirror sync with a warning exited 0\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "F-0001") || !strings.Contains(stderr, "tracker pub not configured") {
		t.Fatalf("stderr lacks F-0001's warning:\n%s", stderr)
	}
}

// F-0469 crit. 1: a tracker list at the 100-issue cap is noted on stderr and
// the sync still exits 0.
func TestMirrorSyncCmdNotesListCap(t *testing.T) {
	pub := &trackertest.Fake{Name: "pub"}
	projDir, _ := mirrorCLIProject(t, pub)
	for i := 0; i < 100; i++ {
		pub.Open("Issue "+strconv.Itoa(i), "", "")
	}
	stdout, stderr, err := execCerneSplit(t, "mirror", "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("mirror sync: %v\n%s%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "pub") || !strings.Contains(stderr, "not imported") {
		t.Fatalf("stderr lacks the cap note:\n%s", stderr)
	}
}

// F-0465 crit. 4: a last-sync record that cannot be written is printed, and
// the sync still exits 0.
func TestMirrorSyncCmdRecordFailureDoesNotFail(t *testing.T) {
	pub := &trackertest.Fake{Name: "pub"}
	projDir, repoDir := mirrorCLIProject(t, pub)
	if err := os.Mkdir(filepath.Join(filepath.Dir(repoDir), "mirror-sync.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := execCerneSplit(t, "mirror", "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("mirror sync: %v\n%s%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "last sync not recorded") {
		t.Fatalf("stderr lacks the note:\n%s", stderr)
	}
}

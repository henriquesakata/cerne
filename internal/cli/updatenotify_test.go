package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/version"
)

// notifyFixture is a project on a machine whose latest release is tag, with
// the notice set up as asked. It returns the project, the number of release
// queries and the cache dir.
func notifyFixture(t *testing.T, tag string, notify bool) (projDir string, hits *int32, cache string) {
	t.Helper()
	projDir, _, _ = setupProject(t)
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	hits = new(int32)
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/", func(http.ResponseWriter, *http.Request) {})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	t.Setenv("CERNE_BASE_URL", s.URL)
	t.Setenv("CERNE_NO_UPDATE_NOTIFIER", "")
	t.Setenv("CI", "")
	prev, prevExe := version.Version, updateExecutable
	t.Cleanup(func() { version.Version, updateExecutable = prev, prevExe })
	version.Version = "v1.0.0"
	updateExecutable = func() (string, error) { return "/usr/local/bin/cerne", nil }
	if notify {
		if err := project.SetSetting(projDir, "update.notify", project.ScopeHost, "true"); err != nil {
			t.Fatal(err)
		}
	}
	return projDir, hits, os.Getenv("CERNE_CACHE_DIR")
}

// F-0479 crits. 2 and 3: a newer release makes one line with both versions
// and the command; the next sessions, in any project, hear nothing about that
// version; a still newer one is announced once more; an up-to-date binary
// hears nothing.
func TestUpdateNoticeOncePerVersion(t *testing.T) {
	projDir, hits, _ := notifyFixture(t, "v1.1.0", true)
	got := updateNotice(projDir)
	for _, want := range []string{"v1.1.0", "v1.0.0", "ask the human to run `cerne update`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice = %q, missing %q", got, want)
		}
	}
	if again := updateNotice(projDir); again != "" {
		t.Fatalf("second session got %q, want nothing", again)
	}
	if *hits != 1 {
		t.Fatalf("release queries = %d, want 1 (cached for a day)", *hits)
	}
	version.Version = "v1.1.0"
	if n := updateNotice(projDir); n != "" {
		t.Fatalf("up to date got %q", n)
	}
}

// crits. 4, 5 and 8: where cerne update cannot swap the binary, with the
// variable, with CI, or with the setting off, there is no line and no query.
func TestUpdateNoticeStaysQuiet(t *testing.T) {
	gopath := t.TempDir()
	t.Setenv("GOPATH", gopath)
	t.Setenv("GOBIN", "")
	gobin := filepath.Join(gopath, "bin", "cerne")
	cases := []struct {
		name, exe, ver, env string
		notify, want        bool
	}{
		{"script", "/usr/local/bin/cerne", "v1.0.0", "", true, true},
		{"homebrew", "/opt/homebrew/Cellar/cerne/1.0.0/bin/cerne", "v1.0.0", "", true, true},
		{"local build", "/work/bin/cerne", "dev", "", true, false},
		{"go install", gobin, "v1.0.0", "", true, false},
		{"notifier off", "/usr/local/bin/cerne", "v1.0.0", "CERNE_NO_UPDATE_NOTIFIER", true, false},
		{"CI", "/usr/local/bin/cerne", "v1.0.0", "CI", true, false},
		{"setting off", "/usr/local/bin/cerne", "v1.0.0", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projDir, hits, _ := notifyFixture(t, "v1.1.0", c.notify)
			version.Version = c.ver
			exe := c.exe
			updateExecutable = func() (string, error) { return exe, nil }
			if c.env != "" {
				t.Setenv(c.env, "1")
			}
			got := updateNotice(projDir)
			if (got != "") != c.want {
				t.Fatalf("notice = %q, want a line: %v", got, c.want)
			}
			if !c.want && *hits != 0 {
				t.Fatalf("%d release queries, want none", *hits)
			}
		})
	}
}

// crit. 6: the notice only informs; the project and the binary stay as they were.
func TestUpdateNoticeWritesNothingInTheProject(t *testing.T) {
	projDir, _, _ := notifyFixture(t, "v1.1.0", true)
	snap := func() string {
		var b strings.Builder
		filepath.WalkDir(projDir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				data, _ := os.ReadFile(p)
				b.WriteString(p + "\x00" + string(data) + "\x00")
			}
			return nil
		})
		return b.String()
	}
	before := snap()
	if updateNotice(projDir) == "" {
		t.Fatal("no notice")
	}
	if snap() != before {
		t.Fatal("the notice changed project files")
	}
}

// crit. 2 on the real path: the session-start hook prints the line after the
// stale-files lines, and only there.
func TestSessionStartCarriesTheUpdateLine(t *testing.T) {
	projDir, _, _ := notifyFixture(t, "v1.1.0", true)
	out, err := execCerneStdin(t, sessionStartJSON(t, "s-notify", projDir), "hook", "channel", "--point", "session-start")
	if err != nil || !strings.Contains(out, "ask the human to run `cerne update`") {
		t.Fatalf("session start = %q, %v", out, err)
	}
	out, _ = execCerneStdin(t, sessionStartJSON(t, "s-notify-2", projDir), "hook", "channel", "--point", "session-start")
	if strings.Contains(out, "cerne update") {
		t.Fatalf("second session repeated the notice: %q", out)
	}
}

// F-0479: cerne next is out of scope: it neither prints the line nor spends the
// once-per-version marker, so the session start still gets it afterwards.
func TestNextNeverSpendsTheUpdateNotice(t *testing.T) {
	projDir, hits, cache := notifyFixture(t, "v1.1.0", true)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	if strings.Contains(out, "is out (this is") {
		t.Fatalf("next printed the release line:\n%s", out)
	}
	if m, _ := filepath.Glob(filepath.Join(cache, "update-notified-*")); len(m) != 0 || *hits != 0 {
		t.Fatalf("next left markers %v or asked %d times", m, *hits)
	}
	out, _ = execCerneStdin(t, sessionStartJSON(t, "s-after-next", projDir), "hook", "channel", "--point", "session-start")
	if !strings.Contains(out, "is out (this is v1.0.0)") {
		t.Fatalf("session start lost the line: %q", out)
	}
}

// The line comes last: after what the refresh renewed and the stale warnings.
func TestUpdateLineComesAfterTheStaleLines(t *testing.T) {
	projDir, _, _ := notifyFixture(t, "v1.1.0", true)
	t.Setenv("HOME", t.TempDir())
	assets := hooks.Assets()
	hookDir := filepath.Join(projDir, ".claude", "hooks")
	appendTo(t, filepath.Join(hookDir, assets[0].Script), "\n# local edit\n")
	if err := os.Remove(filepath.Join(hookDir, assets[1].Script)); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(projDir, ".claude", "skills", "cerne-authoring", "SKILL.md"), "\nold guide\n")
	out, err := execCerneStdin(t, sessionStartJSON(t, "s-order", projDir), "hook", "channel", "--point", "session-start")
	if err != nil {
		t.Fatal(err)
	}
	stale, line := strings.Index(out, "out of date"), strings.Index(out, "is out (this is")
	if stale < 0 || line < 0 || stale > line {
		t.Fatalf("stale warning at %d, update line at %d, want the line last:\n%s", stale, line, out)
	}
}

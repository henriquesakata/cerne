package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

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
	gobin := filepath.Join(os.Getenv("HOME"), "go", "bin", "cerne")
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

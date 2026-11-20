package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// F-0476 crit. 6 and 9: the install path decides who replaces the binary.
func TestDetect(t *testing.T) {
	gobin := filepath.Join("/home", "me", "go", "bin")
	cases := []struct {
		name, exe, resolved, version string
		want                         Method
	}{
		{"homebrew macOS", "/opt/homebrew/bin/cerne", "/opt/homebrew/Cellar/cerne/0.14.3/bin/cerne", "v0.14.3", Homebrew},
		{"homebrew linux", "/home/linuxbrew/.linuxbrew/Cellar/cerne/0.14.3/bin/cerne", "/home/linuxbrew/.linuxbrew/Cellar/cerne/0.14.3/bin/cerne", "v0.14.3", Homebrew},
		{"install script", "/usr/local/bin/cerne", "/usr/local/bin/cerne", "v0.14.3", Script},
		{"go install", filepath.Join(gobin, "cerne"), filepath.Join(gobin, "cerne"), "v0.14.3", GoInstall},
		{"local build", "/work/cerne/bin/cerne", "/work/cerne/bin/cerne", "dev", Dev},
		{"go install of a dev build", filepath.Join(gobin, "cerne"), filepath.Join(gobin, "cerne"), "dev", Dev},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Detect(c.exe, c.resolved, c.version, []string{gobin}); got != c.want {
				t.Fatalf("Detect = %v, want %v", got, c.want)
			}
		})
	}
}

func TestGoBins(t *testing.T) {
	t.Setenv("GOBIN", "/x/gobin")
	t.Setenv("GOPATH", strings.Join([]string{"/a", "/b"}, string(os.PathListSeparator)))
	got := strings.Join(GoBins(), ",")
	want := strings.Join([]string{"/x/gobin", filepath.Join("/a", "bin"), filepath.Join("/b", "bin")}, ",")
	if got != want {
		t.Fatalf("GoBins = %s, want %s", got, want)
	}
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "/h")
	t.Setenv("USERPROFILE", "/h")
	if got := GoBins(); len(got) != 1 || got[0] != filepath.Join("/h", "go", "bin") {
		t.Fatalf("default GoBins = %v", got)
	}
}

func TestBrewLink(t *testing.T) {
	if got := BrewLink("/opt/homebrew/Cellar/cerne/0.14.3/bin/cerne"); got != filepath.Join("/opt/homebrew", "bin", "cerne") {
		t.Fatalf("BrewLink = %s", got)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.15.0", "v0.14.3", true},
		{"v0.14.3", "v0.14.3", false},
		{"v0.14.3", "v0.15.0-rc.1", false},
		{"v0.15.0", "v0.15.0-rc.1", true},
		{"v0.15.0-rc.2", "v0.15.0-rc.1", true},
		{"v1.0.0", "v0.99.9", true},
		{"v0.14.10", "v0.14.9", true},
		{"v0.15.0", "garbage", true},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%s, %s) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

// release serves a fake GitHub release: /releases/latest redirects to the tag,
// the asset and SHA256SUMS under /releases/download/<tag>/. sums overrides the
// checksum file when non-empty.
func release(t *testing.T, tag string, asset []byte, sums string) (*httptest.Server, *int) {
	t.Helper()
	hits := new(int)
	name := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	if sums == "" {
		h := sha256.Sum256(asset)
		sums = hex.EncodeToString(h[:]) + "  " + name + "\n"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		switch r.URL.Path {
		case "/releases/latest":
			http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
		case "/releases/tag/" + tag:
			fmt.Fprint(w, "ok")
		case "/releases/download/" + tag + "/" + name:
			w.Write(asset)
		case "/releases/download/" + tag + "/SHA256SUMS":
			fmt.Fprint(w, sums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func TestLatest(t *testing.T) {
	srv, _ := release(t, "v0.15.0", nil, "x")
	got, err := Latest(srv.URL)
	if err != nil || got != "v0.15.0" {
		t.Fatalf("Latest = %q, %v", got, err)
	}
	none := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "no release") }))
	defer none.Close()
	if _, err := Latest(none.URL); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Latest without a redirect: %v, want ErrNoRelease", err)
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("v1.2.3", "linux", "arm64"); got != "cerne_v1.2.3_linux_arm64" {
		t.Fatal(got)
	}
	// install.ps1 always installs the amd64 build (ARM runs it emulated).
	if got := AssetName("v1.2.3", "windows", "arm64"); got != "cerne_v1.2.3_windows_amd64.exe" {
		t.Fatal(got)
	}
}

// F-0476 crit. 7: a good release replaces the executable; a checksum that does
// not match, or a folder that cannot be written, keeps the old one in place.
func TestFetchAndReplace(t *testing.T) {
	t.Run("good release replaces", func(t *testing.T) {
		srv, _ := release(t, "v0.15.0", []byte("new binary"), "")
		target := oldBinary(t)
		data, err := Fetch(srv.URL, "v0.15.0", runtime.GOOS, runtime.GOARCH)
		if err != nil {
			t.Fatal(err)
		}
		if err := Replace(target, data, runtime.GOOS); err != nil {
			t.Fatal(err)
		}
		assertContent(t, target, "new binary")
		if runtime.GOOS != "windows" {
			if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o755 {
				t.Fatalf("mode = %v, want 0755", fi.Mode().Perm())
			}
		}
		assertNoStaging(t, filepath.Dir(target))
	})
	t.Run("checksum mismatch keeps the old one", func(t *testing.T) {
		srv, _ := release(t, "v0.15.0", []byte("tampered"), strings.Repeat("0", 64)+"  "+AssetName("v0.15.0", runtime.GOOS, runtime.GOARCH)+"\n")
		target := oldBinary(t)
		if _, err := Fetch(srv.URL, "v0.15.0", runtime.GOOS, runtime.GOARCH); !errors.Is(err, ErrChecksum) {
			t.Fatalf("Fetch = %v, want ErrChecksum", err)
		}
		assertContent(t, target, "old binary")
	})
	t.Run("missing checksum entry keeps the old one", func(t *testing.T) {
		srv, _ := release(t, "v0.15.0", []byte("new"), "abc  some_other_file\n")
		if _, err := Fetch(srv.URL, "v0.15.0", runtime.GOOS, runtime.GOARCH); !errors.Is(err, ErrChecksum) {
			t.Fatalf("Fetch = %v, want ErrChecksum", err)
		}
	})
	t.Run("unwritable folder keeps the old one", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs POSIX permissions and a non-root user")
		}
		target := oldBinary(t)
		dir := filepath.Dir(target)
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
		if err := Writable(dir); err == nil {
			t.Fatal("Writable: nil on a read-only folder")
		}
		if err := Replace(target, []byte("new"), runtime.GOOS); err == nil {
			t.Fatal("Replace: nil on a read-only folder")
		}
		assertContent(t, target, "old binary")
	})
	t.Run("windows renames the running executable aside", func(t *testing.T) {
		target := oldBinary(t)
		if err := Replace(target, []byte("new"), "windows"); err != nil {
			t.Fatal(err)
		}
		assertContent(t, target, "new")
		olds, _ := filepath.Glob(target + ".*.old")
		if len(olds) != 1 {
			t.Fatalf("old copies = %v, want the renamed executable", olds)
		}
		assertContent(t, olds[0], "old binary")
		// The next update removes the copies left by earlier ones.
		if err := Replace(target, []byte("newer"), "windows"); err != nil {
			t.Fatal(err)
		}
		if olds, _ := filepath.Glob(target + ".*.old"); len(olds) != 1 {
			t.Fatalf("old copies after a second update = %v, want only the latest", olds)
		}
	})
}

func oldBinary(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "cerne")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", path, got, err, want)
	}
}

func assertNoStaging(t *testing.T, dir string) {
	t.Helper()
	if left, _ := filepath.Glob(filepath.Join(dir, ".cerne.update.*")); len(left) > 0 {
		t.Fatalf("staging files left: %v", left)
	}
}

// F-0476 crit. 7 on a real Windows: a running cerne.exe cannot be overwritten,
// only renamed, so the swap must work while the old one runs (CI Windows job).
func TestReplaceRunningExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the running-executable lock is Windows-only")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "cerne.exe")
	if err := os.WriteFile(target, data, 0o755); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(target, "-test.run=^TestHelperSleep$")
	run.Env = append(os.Environ(), "CERNE_SELFUPDATE_SLEEP=1")
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.Process.Kill(); run.Wait() })
	time.Sleep(500 * time.Millisecond)
	if err := Replace(target, []byte("new"), "windows"); err != nil {
		t.Fatalf("Replace while running: %v", err)
	}
	assertContent(t, target, "new")
}

// TestHelperSleep is the running executable of TestReplaceRunningExecutable.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("CERNE_SELFUPDATE_SLEEP") == "" {
		t.Skip("helper process only")
	}
	time.Sleep(30 * time.Second)
}

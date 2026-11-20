package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/server"
)

// TestBinaryPlan (F-0480 criterion 6): what replacing the binary would take,
// read without the network — the panel shows the command instead of a
// button for a local build, a go install or a folder it cannot write.
func TestBinaryPlan(t *testing.T) {
	cases := []struct {
		name          string
		setup         func(t *testing.T) string
		swappable     bool
		reason, fixIn string
	}{
		{"script install", func(t *testing.T) string { return installedBinary(t, "v0.14.3") }, true, "", ""},
		{"dev build", func(t *testing.T) string { return installedBinary(t, "dev") }, false, "local build", "go build"},
		{"go install", func(t *testing.T) string {
			exe := installedBinary(t, "v0.14.3")
			t.Setenv("GOBIN", filepath.Dir(exe))
			return exe
		}, false, "go install", "go install github.com/henriquesakata/cerne/cmd/cerne@latest"},
		{"unwritable folder", func(t *testing.T) string {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("needs POSIX permissions and a non-root user")
			}
			exe := installedBinary(t, "v0.14.3")
			os.Chmod(filepath.Dir(exe), 0o555)
			t.Cleanup(func() { os.Chmod(filepath.Dir(exe), 0o755) })
			return exe
		}, false, "cannot write", "install.sh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exe := c.setup(t)
			hits := fakeRelease(t, "v0.15.0", []byte("new"))
			ok, why, fix := server.PlanBinary(exe)
			if ok != c.swappable || !strings.Contains(why, c.reason) || !strings.Contains(fix, c.fixIn) {
				t.Fatalf("plan = %v %q %q, want %v with %q and %q", ok, why, fix, c.swappable, c.reason, c.fixIn)
			}
			if *hits != 0 {
				t.Fatalf("the plan asked the network %d time(s)", *hits)
			}
		})
	}
}

// TestReplaceBinaryForPanel (F-0480 criteria 3 and 5): the panel's swap is
// cerne update's own, through the same checks; the click in the UI is the
// human's, so an agent environment around the panel does not refuse it.
func TestReplaceBinaryForPanel(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	exe := installedBinary(t, "v0.14.3")
	fakeRelease(t, "v0.15.0", []byte("new binary"))
	var out bytes.Buffer
	got := server.ReplaceBinary(&out, exe)
	if real, _ := filepath.EvalSymlinks(exe); got.State != "updated" || got.NewExe != real {
		t.Fatalf("outcome = %+v, want updated into %s\n%s", got, exe, out.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Fatalf("executable holds %q", b)
	}

}

// Critic 2 (F-0480 criterion 3): under Homebrew the panel's swap is
// Homebrew's, through the PATH link the panel was started from; it reports
// the link, whose new keg is what the panel restarts into.
func TestReplaceBinaryForPanelHomebrew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew runs on macOS and Linux")
	}
	prefix := t.TempDir()
	oldKeg := filepath.Join(prefix, "Cellar", "cerne", "0.14.3", "bin", "cerne")
	os.MkdirAll(filepath.Dir(oldKeg), 0o755)
	mustWrite(t, oldKeg, "old")
	link := filepath.Join(prefix, "bin", "cerne")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(oldKeg, link); err != nil {
		t.Fatal(err)
	}
	brew := filepath.Join(t.TempDir(), "brew")
	os.WriteFile(brew, []byte(`#!/bin/sh
echo "==> Upgrading henriquesakata/tap/cerne"
k='`+prefix+`/Cellar/cerne/0.15.0/bin'
mkdir -p "$k" && echo new > "$k/cerne" && rm -rf '`+filepath.Dir(filepath.Dir(oldKeg))+`' && ln -sf "$k/cerne" '`+link+`'
`), 0o755)
	prevBrew := brewCommand
	brewCommand = brew
	t.Cleanup(func() { brewCommand = prevBrew })
	setRunningBinary(t, link, "v0.14.3")

	if ok, why, _ := server.PlanBinary(link); !ok {
		t.Fatalf("Homebrew install not swappable: %s", why)
	}
	var out bytes.Buffer
	got := server.ReplaceBinary(&out, link)
	realDir, _ := filepath.EvalSymlinks(filepath.Dir(link))
	if got.State != "updated" || got.NewExe != filepath.Join(realDir, "cerne") {
		t.Fatalf("outcome = %+v, want updated through the link %s\n%s", got, link, out.String())
	}
	if !strings.Contains(out.String(), "==> Upgrading henriquesakata/tap/cerne") {
		t.Fatalf("Homebrew's output did not reach the panel:\n%s", out.String())
	}
	if now, _ := filepath.EvalSymlinks(link); !strings.Contains(now, "0.15.0") {
		t.Fatalf("the link resolves to %s, want the new keg", now)
	}
}

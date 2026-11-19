package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHookScriptsFindCerneOnBarePath (F-0428): Cursor runs the hooks with the
// bare system PATH when it cannot resolve the login shell's environment, so a
// cerne installed by `go install` (~/go/bin), install.sh (~/.local/bin,
// /usr/local/bin) or Homebrew is not on it. Every hook script still finds a
// cerne in ~/go/bin, and so does the native providers' fail-open command.
func TestHookScriptsFindCerneOnBarePath(t *testing.T) {
	scripts, err := filepath.Glob("assets/*.sh")
	if err != nil || len(scripts) == 0 {
		t.Fatalf("no hook scripts: %v", err)
	}
	failOpen := filepath.Join(t.TempDir(), "failopen.sh")
	if err := os.WriteFile(failOpen, []byte(failOpenCmd("cerne hook session-context")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, script := range append(scripts, failOpen) {
		home := t.TempDir()
		bin := filepath.Join(home, "go", "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(home, "ran")
		stub := "#!/bin/sh\ntouch '" + marker + "'\n"
		if err := os.WriteFile(filepath.Join(bin, "cerne"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", script)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "CURSOR_VERSION=3.25.6"} // Cursor's bare PATH (and the Cursor-only after-tool script)
		cmd.Stdin = strings.NewReader("{}")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", script, err, out)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("%s: cerne in ~/go/bin was not run on the bare PATH", script)
		}
	}
}

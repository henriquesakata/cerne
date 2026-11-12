package hooks

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// agentHooksOffGuard is the line every managed asset runs first (F-0360).
const agentHooksOffGuard = `[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0`

// TestAssetsGuardIsFirstExecutableLine (F-0360 crit. 1): every managed asset
// runs the CERNE_NO_AGENT_HOOKS guard before anything else, and its header
// names the variable as the way to turn the hook off.
func TestAssetsGuardIsFirstExecutableLine(t *testing.T) {
	for _, a := range Assets() {
		body := mustRead("assets/" + a.Script)
		var first string
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			first = trimmed
			break
		}
		if first != agentHooksOffGuard {
			t.Errorf("%s: first executable line = %q, want %q", a.Script, first, agentHooksOffGuard)
		}
		header := body[:strings.Index(body, agentHooksOffGuard+"\n")+1]
		if !strings.Contains(header, "CERNE_NO_AGENT_HOOKS=1") {
			t.Errorf("%s: header does not name CERNE_NO_AGENT_HOOKS=1 as the off switch", a.Script)
		}
	}
}

// runAsset runs one installed asset the way settings.json does (bash <script>),
// with a fake cerne first on PATH that logs each call and echoes stdin for
// `hook envelope`. It returns the combined output and whether cerne was called.
func runAsset(t *testing.T, proj, script string, env ...string) (stdout, stderr string, called bool) {
	t.Helper()
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "calls")
	fake := "#!/bin/sh\necho \"$*\" >> \"$CERNE_FAKE_LOG\"\n[ \"$1 $2\" = \"hook envelope\" ] && cat\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "cerne"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(proj, ".claude", "hooks", script))
	cmd.Dir = proj
	cmd.Env = append([]string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"HOME=" + t.TempDir(),
		"CERNE_FAKE_LOG=" + logPath,
		"CLAUDE_PROJECT_DIR=" + proj,
	}, env...)
	cmd.Stdin = strings.NewReader(`{"session_id":"s","cwd":"` + proj + `","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: exit error %v\nstderr: %s", script, err, errb.String())
	}
	_, statErr := os.Stat(logPath)
	return out.String(), errb.String(), statErr == nil
}

func installedProject(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook assets are POSIX shell scripts")
	}
	proj := t.TempDir()
	if _, err := Install(proj); err != nil {
		t.Fatal(err)
	}
	return proj
}

// TestAssetsSilentWhenOff (F-0360 crit. 2): with CERNE_NO_AGENT_HOOKS set,
// each asset exits 0 with no output and never runs the cerne binary.
func TestAssetsSilentWhenOff(t *testing.T) {
	proj := installedProject(t)
	for _, a := range Assets() {
		stdout, stderr, called := runAsset(t, proj, a.Script, "CERNE_NO_AGENT_HOOKS=1")
		if stdout != "" || stderr != "" {
			t.Errorf("%s: output with the switch on: stdout %q stderr %q", a.Script, stdout, stderr)
		}
		if called {
			t.Errorf("%s: ran the cerne binary with the switch on", a.Script)
		}
	}
}

// TestAssetsUnchangedWhenOn (F-0360 crit. 3): with the variable absent or
// empty, each asset calls cerne as before, and the two rule printers print.
func TestAssetsUnchangedWhenOn(t *testing.T) {
	proj := installedProject(t)
	printers := map[string]bool{registerScript: true, subagentScript: true}
	for name, env := range map[string][]string{"absent": nil, "empty": {"CERNE_NO_AGENT_HOOKS="}} {
		for _, a := range Assets() {
			stdout, _, called := runAsset(t, proj, a.Script, env...)
			if !called {
				t.Errorf("%s (%s): cerne not called", a.Script, name)
			}
			if printers[a.Script] && !strings.Contains(stdout, "cerne project rules") {
				t.Errorf("%s (%s): rules not printed:\n%s", a.Script, name, stdout)
			}
		}
	}
}

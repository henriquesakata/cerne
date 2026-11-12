package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runReminder runs the installed SessionStart script with PATH limited to the
// system dirs plus binDir (where a test may place a fake cerne).
func runReminder(t *testing.T, binDir string) string {
	t.Helper()
	cmd := exec.Command("bash", "assets/cerne-register-reminder.sh")
	cmd.Env = []string{"PATH=" + binDir + ":/usr/bin:/bin", "HOME=" + t.TempDir()}
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reminder script: %v", err)
	}
	return string(out)
}

// fakeCerne writes a cerne stub: `hook gauntlet-rule` prints rule and exits
// with code; every other call exits 0 silently.
func fakeCerne(t *testing.T, rule string, code int) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$2\" = gauntlet-rule ]; then printf '%s' '" + rule + "'; exit " + string(rune('0'+code)) + "; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "cerne"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestReminder_LightMode (F-0364 critic 4): Claude Code's SessionStart text
// asks cerne for the GAUNTLET item. With the Gauntlet on — and when cerne is
// missing or fails, the fail-safe — the text is byte-for-byte the pre-light-
// mode one (golden); in light mode the item is gone.
func TestReminder_LightMode(t *testing.T) {
	golden, err := os.ReadFile("testdata/session-start-gauntlet-on.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := runReminder(t, t.TempDir()); got != string(golden) {
		t.Fatalf("no cerne on PATH: text differs from golden\n got: %q\nwant: %q", got, golden)
	}
	if got := runReminder(t, fakeCerne(t, "", 1)); got != string(golden) {
		t.Fatalf("cerne fails: text differs from golden\n got: %q", got)
	}
	light := runReminder(t, fakeCerne(t, "", 0))
	if strings.Contains(light, "GAUNTLET") || !strings.Contains(light, "HUMAN GATES") || !strings.Contains(light, "DUPLICATE GUARD") {
		t.Fatalf("light mode text:\n%s", light)
	}
	if strings.Contains(string(golden), "skip it when") {
		t.Fatal("golden carries the static skip clause")
	}
}

// TestGauntletRuleMatchesGolden: the item cerne prints is the golden's
// GAUNTLET line, so the Go copy and the script fallback cannot drift.
func TestGauntletRuleMatchesGolden(t *testing.T) {
	golden, err := os.ReadFile("testdata/session-start-gauntlet-on.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(golden), "\n"+GauntletRule+"\n") {
		t.Fatalf("GauntletRule is not the golden's GAUNTLET line:\n%s", GauntletRule)
	}
}

package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// TestHookInstallCodexSaysHowToTrustAndCheck (F-0474 crit. 3): Codex reads
// .codex/ only in a trusted project, so the output names the trust line and the
// two checks.
func TestHookInstallCodexSaysHowToTrustAndCheck(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex")
	if err != nil {
		t.Fatalf("hook install: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"only reads .codex/ in a trusted project",
		"[projects.\"" + dir + "\"]",
		`trust_level = "trusted"`,
		"printenv CERNE_ACTOR",
		"cerne next",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Other providers do not get the Codex guidance.
	out, err = execCerne(t, "hook", "install", "--dir", dir, "--provider", "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "trusted project") {
		t.Errorf("codex guidance printed for gemini:\n%s", out)
	}
}

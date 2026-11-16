package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
)

var vscodeSkillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// TestSkillInstallCopilot (F-0444 crit. 2): the skill under .github/skills with
// a VS Code-valid name, the instructions in cerne's own file, the team's
// copilot-instructions.md untouched, idempotent.
func TestSkillInstallCopilot(t *testing.T) {
	dir := t.TempDir()
	team := []byte("team rules\n")
	if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "copilot-instructions.md"), team, 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := Install(dir, hooks.ProviderCopilot)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Status == StatusSkipped {
			t.Fatalf("unexpected skip: %+v", r)
		}
	}
	skill := filepath.Join(dir, ".github", "skills", guideName)
	if got := read(t, filepath.Join(skill, "SKILL.md")); got != authoring(t) {
		t.Fatal("installed SKILL.md differs from the canonical one")
	}
	if _, err := os.Stat(filepath.Join(skill, "reference")); err != nil {
		t.Fatalf("reference files not installed: %v", err)
	}
	m := regexp.MustCompile(`(?m)^name:\s*(\S+)\s*$`).FindStringSubmatch(read(t, filepath.Join(skill, "SKILL.md")))
	if m == nil || m[1] != filepath.Base(skill) || len(m[1]) > 64 || !vscodeSkillName.MatchString(m[1]) {
		t.Fatalf("skill name %v is not valid for VS Code / equal to its folder", m)
	}

	ins := read(t, filepath.Join(dir, ".github", "instructions", "cerne.instructions.md"))
	for _, want := range []string{"applyTo: \"**\"", "cerne feature new", ".github/skills/cerne-authoring/SKILL.md"} {
		if !strings.Contains(ins, want) {
			t.Fatalf("instructions missing %q:\n%s", want, ins)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".github", "copilot-instructions.md")); string(got) != string(team) {
		t.Fatalf("project copilot-instructions.md touched: %q", got)
	}

	rs, err = Install(dir, hooks.ProviderCopilot)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Agent == hooks.ProviderCopilot && r.Status != StatusUnchanged {
			t.Fatalf("second run not idempotent: %+v", r)
		}
	}
}

// TestSkillInstallCopilotSymlinkInstructionsSkipped: a symlinked instructions
// file is not written through.
func TestSkillInstallCopilotSymlinkInstructionsSkipped(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.md")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github", "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ".github", "instructions", "cerne.instructions.md")); err != nil {
		t.Skip("symlinks unavailable")
	}
	rs, _ := Install(dir, hooks.ProviderCopilot)
	skipped := false
	for _, r := range rs {
		if r.Agent == hooks.ProviderCopilot && r.Status == StatusSkipped {
			skipped = true
		}
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" || !skipped {
		t.Fatalf("symlink target written or no skip: %q skipped=%v", b, skipped)
	}
}

// TestCopilotStates (F-0444 crit. 5): skill and instructions report ok /
// divergent / missing; a divergent copy is stale, a missing one is not.
func TestCopilotStates(t *testing.T) {
	dir := t.TempDir()
	if CopilotSkillState(dir) != hooks.FileMissing || CopilotInstructionsState(dir) != hooks.FileMissing {
		t.Fatal("empty project must be missing")
	}
	for _, n := range Stale(dir) {
		if strings.Contains(n, "copilot") {
			t.Fatalf("missing copilot install must not be stale: %v", Stale(dir))
		}
	}
	if _, err := Install(dir, hooks.ProviderCopilot); err != nil {
		t.Fatal(err)
	}
	if CopilotSkillState(dir) != hooks.FileOK || CopilotInstructionsState(dir) != hooks.FileOK {
		t.Fatal("fresh install must be ok")
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "skills", guideName, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "instructions", "cerne.instructions.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if CopilotSkillState(dir) != hooks.FileDivergent || CopilotInstructionsState(dir) != hooks.FileDivergent {
		t.Fatal("edited copies must be divergent")
	}
	want := map[string]bool{"cerne-authoring (copilot)": false, "copilot instructions": false}
	for _, n := range Stale(dir) {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, ok := range want {
		if !ok {
			t.Errorf("%q not in Stale: %v", n, Stale(dir))
		}
	}
}

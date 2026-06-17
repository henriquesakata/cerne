package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillInstall(t *testing.T) {
	dir := t.TempDir()

	out, err := execCerne(t, "skill", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("skill install: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "cerne-authoring: criada em .claude/skills/cerne-authoring") {
		t.Fatalf("output missing created line:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "cerne-authoring", "SKILL.md")); err != nil {
		t.Fatalf("skill file not written: %v", err)
	}

	out, err = execCerne(t, "skill", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("rerun: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "cerne-authoring: já atual") {
		t.Fatalf("rerun output missing 'já atual':\n%s", out)
	}
}

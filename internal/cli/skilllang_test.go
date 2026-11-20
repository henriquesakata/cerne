package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// installedSkill returns the files `cerne skill install` wrote under
// .claude/skills/cerne-authoring, by relative path.
func installedSkill(t *testing.T, projDir string) map[string]string {
	t.Helper()
	if out, err := execCerne(t, "skill", "install", "--dir", projDir); err != nil {
		t.Fatalf("skill install: %v\n%s", err, out)
	}
	root := filepath.Join(projDir, ".claude", "skills", "cerne-authoring")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		files[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestSkillInstall_SameInEveryDocsLanguage (F-0475 crit. 5): the installed
// guide has no variant per documentation language; the project's language
// reaches the agent through the session-start line, not through the skill.
// It pins existing behaviour: it passed on its first run.
func TestSkillInstall_SameInEveryDocsLanguage(t *testing.T) {
	enDir, _, _ := setupProjectIn(t, "en")
	en := installedSkill(t, enDir)
	ptDir, _, _ := setupProjectIn(t, "pt-BR")
	pt := installedSkill(t, ptDir)
	if len(en) == 0 || len(en) != len(pt) {
		t.Fatalf("file sets differ: en %d files, pt-BR %d", len(en), len(pt))
	}
	for name, body := range en {
		if pt[name] != body {
			t.Errorf("%s differs between an en and a pt-BR project", name)
		}
	}
}

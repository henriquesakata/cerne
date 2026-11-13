package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// referenceNames are the files under cerne-authoring/reference on disk, the
// source the install must deliver.
func referenceNames(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(referenceDir)
	if err != nil || len(es) == 0 {
		t.Fatalf("reference dir: %v (%d entries)", err, len(es))
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// TestInstallWritesTheReferenceFiles pins F-0399 criterion 3 and 7: the
// reference folder lands next to SKILL.md for Claude Code and for every other
// agent (F-0362), byte for byte, and a rerun reports unchanged.
func TestInstallWritesTheReferenceFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, hooks.ProviderCursor, hooks.ProviderCodex, hooks.ProviderGemini); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{".claude/skills", ".cursor/skills", ".agents/skills", ".gemini/skills"} {
		for _, name := range referenceNames(t) {
			want, _ := os.ReadFile(filepath.Join(referenceDir, name))
			got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(base), "cerne-authoring", "reference", name))
			if err != nil {
				t.Errorf("%s: %v", base, err)
				continue
			}
			if string(got) != string(want) {
				t.Errorf("%s/%s differs from the canonical file", base, name)
			}
		}
	}
	rs, err := Install(dir, hooks.ProviderCursor, hooks.ProviderCodex, hooks.ProviderGemini)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Status != StatusUnchanged {
			t.Errorf("rerun: %s %s = %s, want unchanged", r.Skill, r.Path, r.Status)
		}
	}
}

// TestInstallReportsAChangedReferenceAsUpdated: SKILL.md identical but a
// reference file edited locally is repaired and reported, never "unchanged".
func TestInstallReportsAChangedReferenceAsUpdated(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, ".claude", "skills", "cerne-authoring", "reference", referenceNames(t)[0])
	if err := os.WriteFile(f, []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Skill == "cerne-authoring" && r.Status != StatusUpdated {
			t.Errorf("status = %s, want updated", r.Status)
		}
	}
	if b, _ := os.ReadFile(f); string(b) == "local edit\n" {
		t.Error("reference file not restored")
	}
}

// TestSkillPointsToEveryReferenceFile pins criterion 3: SKILL.md names each
// reference file, so an agent finds it.
func TestSkillPointsToEveryReferenceFile(t *testing.T) {
	c := All()[0].Content
	for _, name := range referenceNames(t) {
		if !strings.Contains(c, "`"+name+"`") && !strings.Contains(c, "reference/"+name) {
			t.Errorf("SKILL.md does not name %s", name)
		}
	}
}

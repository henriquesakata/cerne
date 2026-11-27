package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func learnedItems() []Learned {
	return []Learned{
		{
			ID:         "N-0022",
			Title:      "cerne ui: processo vivo na 7717 serve UI velha",
			Body:       "Um processo `cerne ui` já em execução segue com o embed velho.\n\nMatar e reiniciar.\n",
			Components: []string{"ui", "install", "golden-path"},
		},
	}
}

func TestSyncLearnedCreatesUpdatesRemoves(t *testing.T) {
	dir := t.TempDir()

	res, err := SyncLearned(dir, learnedItems())
	if err != nil {
		t.Fatalf("SyncLearned: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusCreated {
		t.Fatalf("first sync = %+v, want 1 created", res)
	}
	dst := filepath.Join(dir, ".claude", "skills", res[0].Skill, "SKILL.md")
	if !strings.HasPrefix(res[0].Skill, "learned-") {
		t.Fatalf("skill dir %q missing learned- prefix", res[0].Skill)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	content := string(got)
	for _, want := range []string{
		"name: " + res[0].Skill,
		"description:",
		"N-0022",
		"cerne skill sync",
		"processo vivo na 7717",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated skill missing %q", want)
		}
	}

	// Idempotent: same items → unchanged.
	res, err = SyncLearned(dir, learnedItems())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusUnchanged {
		t.Fatalf("second sync = %+v, want 1 unchanged", res)
	}

	// Changed body → updated.
	items := learnedItems()
	items[0].Body = "Corpo revisado.\n"
	res, err = SyncLearned(dir, items)
	if err != nil {
		t.Fatalf("third sync: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusUpdated {
		t.Fatalf("third sync = %+v, want 1 updated", res)
	}

	// Item gone → generated dir removed.
	res, err = SyncLearned(dir, nil)
	if err != nil {
		t.Fatalf("empty sync: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusRemoved {
		t.Fatalf("empty sync = %+v, want 1 removed", res)
	}
	if _, err := os.Stat(filepath.Dir(dst)); !os.IsNotExist(err) {
		t.Fatalf("generated dir still present after removal sync")
	}
}

// TestSyncLearnedCursorOnly (F-0527 crit. 4): without .claude/skills, golden paths
// materialize under .cursor/skills/.
func TestSyncLearnedCursorOnly(t *testing.T) {
	// F-0527 crit. 4
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := SyncLearned(dir, learnedItems())
	if err != nil {
		t.Fatalf("SyncLearned: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusCreated {
		t.Fatalf("sync = %+v", res)
	}
	dst := filepath.Join(dir, ".cursor", "skills", res[0].Skill, "SKILL.md")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("cursor learned skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatal(".claude/skills should not be created")
	}
}

// TestSyncLearnedPrunesManagedCursorCopy (F-0527 crit. 4): when both trees
// exist, managed learned-* copies are removed from .cursor/skills/.
func TestSyncLearnedPrunesManagedCursorCopy(t *testing.T) {
	// F-0527 crit. 4
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	name := learnedPrefix + "stale-copy"
	managed := filepath.Join(dir, ".cursor", "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: x\n---\n\n> Generated from N-0001 by `cerne skill sync`\n"
	if err := os.WriteFile(managed, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, ".cursor", "skills", learnedPrefix+"user-owned", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := SyncLearned(dir, learnedItems())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatal("managed copy should be pruned from .cursor/skills")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("user-owned learned skill must remain in .cursor/skills")
	}
	if len(res) != 1 {
		t.Fatalf("sync = %+v", res)
	}
	claudePath := filepath.Join(dir, ".claude", "skills", res[0].Skill, "SKILL.md")
	if _, err := os.Stat(claudePath); err != nil {
		t.Fatal("learned skill should live under .claude/skills when both exist")
	}
}

func TestSyncLearnedLeavesForeignSkillsAlone(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	userSkill := filepath.Join(dir, ".claude", "skills", "my-own", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userSkill, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncLearned(dir, nil); err != nil {
		t.Fatalf("SyncLearned: %v", err)
	}
	for _, keep := range []string{
		filepath.Join(dir, ".claude", "skills", "cerne-authoring", "SKILL.md"),
		filepath.Join(dir, ".claude", "skills", "cerne-self-learning", "SKILL.md"),
		userSkill,
	} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("foreign skill touched by sync: %v", err)
		}
	}
}

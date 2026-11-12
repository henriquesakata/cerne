package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestSkillInstall(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil { // F-0354: --dir must name a project
		t.Fatal(err)
	}

	out, err := execCerne(t, "skill", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("skill install: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "cerne-authoring: created in .claude/skills/cerne-authoring") {
		t.Fatalf("output missing created line:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "cerne-authoring", "SKILL.md")); err != nil {
		t.Fatalf("skill file not written: %v", err)
	}

	out, err = execCerne(t, "skill", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("rerun: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "cerne-authoring: up to date") {
		t.Fatalf("rerun output missing 'up to date':\n%s", out)
	}
}

// syncNoteBody writes a real body for a note straight into the cached backend
// clone (the sync reads the clone as-is; no push cycle needed in the test).
func syncNoteBody(t *testing.T, repoDir, id, body string) {
	t.Helper()
	dst := filepath.Join(repoDir, "content", "notes", id+".md")
	if err := os.WriteFile(dst, []byte("---\ncerne_id: "+id+"\n---\n\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillSyncMaterializesGoldenPaths(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	if out, err := execCerne(t, "note", "new", "Deploy gotcha", "--component", "deploy,golden-path", "--dir", projDir); err != nil {
		t.Fatalf("note new marked: %v\noutput: %s", err, out)
	}
	syncNoteBody(t, repoDir, "N-0001", "# Deploy gotcha\n\nRoute that worked.\n")
	if out, err := execCerne(t, "note", "new", "Unmarked note", "--dir", projDir); err != nil {
		t.Fatalf("note new unmarked: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "skill", "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("skill sync: %v\noutput: %s", err, out)
	}
	noteSkill := filepath.Join(projDir, ".claude", "skills", "learned-n-0001-deploy-gotcha", "SKILL.md")
	if _, err := os.Stat(noteSkill); err != nil {
		t.Errorf("generated skill missing: %v", err)
	}
	if !strings.Contains(out, "learned-n-0001-deploy-gotcha: created") {
		t.Fatalf("sync output missing created line:\n%s", out)
	}
	got, err := os.ReadFile(noteSkill)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "cerne_id:") {
		t.Fatalf("generated skill leaked entity frontmatter:\n%s", got)
	}
	// Only the 1 generated skill beyond the canonical ones init installed.
	entries, _ := os.ReadDir(filepath.Join(projDir, ".claude", "skills"))
	var learned []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "learned-") {
			learned = append(learned, e.Name())
		}
	}
	if len(learned) != 1 {
		t.Fatalf("learned skills = %v, want exactly 1", learned)
	}

	// Unmark the note → next sync removes its generated skill.
	if out, err := execCerne(t, "note", "retag", "N-0001", "--component", "deploy", "--dir", projDir); err != nil {
		t.Fatalf("note retag: %v\noutput: %s", err, out)
	}
	out, err = execCerne(t, "skill", "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("second sync: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "learned-n-0001-deploy-gotcha: removed") {
		t.Fatalf("second sync output missing removed line:\n%s", out)
	}
	if _, err := os.Stat(filepath.Dir(noteSkill)); !os.IsNotExist(err) {
		t.Fatalf("unmarked note's skill still present")
	}
}

// loadGoldenPaths must include active golden-path runbooks too; pinned at the
// store-reading layer because the CLI flow can't tag a runbook yet (no
// 'runbook retag'), and direct writes into the cached clone don't survive the
// next cerne operation.
func TestLoadGoldenPathsIncludesRunbooks(t *testing.T) {
	repoDir := t.TempDir()
	rb := store.Runbook{ID: "RB-0001", Title: "Migration dance", State: domain.RunbookActive, Components: []string{"golden-path"}}
	if err := store.SaveRunbook(repoDir, rb); err != nil {
		t.Fatal(err)
	}
	other := store.Runbook{ID: "RB-0002", Title: "Unmarked", State: domain.RunbookActive}
	if err := store.SaveRunbook(repoDir, other); err != nil {
		t.Fatal(err)
	}
	docDir := filepath.Join(repoDir, "content", "runbooks")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ncerne_id: RB-0001\n---\n\n# Migration dance\n\n1. in order.\n"
	if err := os.WriteFile(filepath.Join(docDir, "RB-0001.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := skills.LoadGoldenPaths(repoDir)
	if err != nil {
		t.Fatalf("loadGoldenPaths: %v", err)
	}
	if len(items) != 1 || items[0].ID != "RB-0001" {
		t.Fatalf("items = %+v, want only RB-0001", items)
	}
	if strings.Contains(items[0].Body, "cerne_id:") {
		t.Fatalf("body kept entity frontmatter: %q", items[0].Body)
	}
}

// F-0362 crit. 3: a project with .cursor/, .codex/ and .gemini/ gets the guide
// where each agent reads skills, and the managed pointers.
func TestSkillInstallDeliversToDetectedAgents(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".cursor", ".codex", ".gemini"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, err := execCerne(t, "skill", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("skill install: %v\n%s", err, out)
	}
	for _, want := range []string{
		"cerne-authoring: created in .cursor/skills/cerne-authoring",
		"cerne-authoring: created in .agents/skills/cerne-authoring",
		"cerne-authoring: created in .gemini/skills/cerne-authoring",
		"cerne-authoring pointer: created in AGENTS.md",
		"cerne-authoring pointer: created in GEMINI.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	out, err = execCerne(t, "skill", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("rerun: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 11 { // 7 asset lines + 4 per-agent lines
		t.Fatalf("want 11 lines, got %d:\n%s", len(lines), out)
	}
	for _, l := range lines[:7] {
		if !strings.Contains(l, ": up to date in ") {
			t.Errorf("rerun line not unchanged: %q", l)
		}
	}
}

// F-0362 round 2: every agent gets a line, detected or not; nothing claims
// "loaded".
func TestSkillInstallPrintsOneLinePerAgent(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "skill", "install", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"claude: guide delivered to .claude/skills/cerne-authoring/SKILL.md",
		"cursor: guide delivered to .cursor/skills/cerne-authoring/SKILL.md — Cursor documents project skills as loaded automatically",
		"codex: not detected (no .codex/) — run 'cerne hook install --provider codex' first",
		"gemini: not detected (no .gemini/) — run 'cerne hook install --provider gemini' first",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "is loaded") || strings.Contains(out, "guide loaded") {
		t.Errorf("output claims loaded:\n%s", out)
	}
}

package harness

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/skills"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestCanonicalSkillStates(t *testing.T) {
	proj := t.TempDir()
	if _, err := skills.Install(proj); err != nil {
		t.Fatal(err)
	}
	inv := Inspect(Options{ProjDir: proj})
	name := skills.All()[0].Name
	it, ok := findItem(inv, "skills", name)
	if !ok {
		t.Fatalf("canonical skill %s absent", name)
	}
	if it.State != StateOK || it.Origin != OriginLocal {
		t.Errorf("installed canonical = %s/%s, want ok/local", it.State, it.Origin)
	}

	path := filepath.Join(proj, ".claude", "skills", filepath.FromSlash(skills.All()[0].RelPath))
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "skills", name)
	if it.State != StateDivergent || it.Fix != "cerne skill install" {
		t.Errorf("tampered canonical = %s fix=%q, want divergent + cerne skill install", it.State, it.Fix)
	}

	os.Remove(path)
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "skills", name)
	if it.State != StateMissing {
		t.Errorf("removed canonical = %s, want missing", it.State)
	}
}

// seedGoldenNote writes one active golden-path note into a fresh backend dir.
func seedGoldenNote(t *testing.T, repo string) skills.Learned {
	t.Helper()
	n := store.Note{ID: "N-0001", Title: "atalho de teste", State: domain.NoteActive, Components: []string{skills.GoldenPathComponent}}
	if err := store.SaveNote(repo, n); err != nil {
		t.Fatal(err)
	}
	body := "corpo do atalho\n"
	if err := os.MkdirAll(filepath.Join(repo, "content", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "content", "notes", "N-0001.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return skills.Learned{ID: n.ID, Title: n.Title, Body: body, Components: n.Components}
}

func TestLearnedSkillReconcileStates(t *testing.T) {
	proj := t.TempDir()
	repo := t.TempDir()
	item := seedGoldenNote(t, repo)

	// Entity exists, skill not materialized → missing, fix = skill sync.
	inv := Inspect(Options{ProjDir: proj, RepoDir: repo})
	var learned *Item
	for _, c := range inv.Classes {
		if c.Name != "skills" {
			continue
		}
		for i, it := range c.Items {
			if it.State == StateMissing && it.Fix == "cerne skill sync" {
				learned = &c.Items[i]
			}
		}
	}
	if learned == nil {
		t.Fatal("unmaterialized golden path not reported missing with skill sync fix")
	}

	// After sync → ok.
	if _, err := skills.SyncLearned(proj, []skills.Learned{item}); err != nil {
		t.Fatal(err)
	}
	inv = Inspect(Options{ProjDir: proj, RepoDir: repo})
	it, ok := findItem(inv, "skills", learned.Name)
	if !ok || it.State != StateOK {
		t.Errorf("synced learned skill = %+v, want ok", it)
	}

	// Orphan learned dir (no backing entity) → divergent with sync fix.
	writeFile(t, filepath.Join(proj, ".claude", "skills", "learned-orfao", "SKILL.md"), "x")
	inv = Inspect(Options{ProjDir: proj, RepoDir: repo})
	it, ok = findItem(inv, "skills", "learned-orfao")
	if !ok || it.State != StateDivergent || it.Fix != "cerne skill sync" {
		t.Errorf("orphan learned dir = %+v, want divergent + cerne skill sync", it)
	}
}

func TestUserSkillConfiguredOutsideRepo(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "skills", "graphify", "SKILL.md"), "# graphify")
	inv := Inspect(Options{ProjDir: proj, Home: home})
	it, ok := findItem(inv, "skills", "graphify")
	if !ok {
		t.Fatal("user skill absent")
	}
	if it.State != StateConfigured || it.Origin != OriginUser || !it.OutsideRepo {
		t.Errorf("user skill = %s/%s outside=%v, want configured/user/true", it.State, it.Origin, it.OutsideRepo)
	}
}

func TestThirdPartyProjectSkillUnmanaged(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "skills", "minha-skill", "SKILL.md"), "# minha")
	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "skills", "minha-skill")
	if !ok {
		t.Fatal("project third-party skill absent")
	}
	if it.State != StateUnmanaged || it.Fix != "" {
		t.Errorf("third-party project skill = %s fix=%q, want unmanaged, no fix", it.State, it.Fix)
	}
}

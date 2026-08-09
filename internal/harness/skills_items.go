package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/skills"
)

const (
	fixSkillInstall = "cerne skill install"
	fixSkillSync    = "cerne skill sync"
)

// skillItems builds the Skills class: canonical cerne skills diffed against
// the embed, learned-* dirs reconciled (dry-run) against the backend's
// golden-path entities, third-party project skills and user-level skills.
// cerne-managed skills are gitignored → origin local.
func skillItems(opts Options, ls layerSet) []Item {
	var items []Item
	skillsDir := filepath.Join(opts.ProjDir, ".claude", "skills")

	canonical := map[string]bool{}
	for _, s := range skills.All() {
		canonical[s.Name] = true
		it := Item{
			Class:  "skills",
			Name:   s.Name,
			Path:   filepath.Join(".claude", "skills", filepath.FromSlash(s.RelPath)),
			Origin: OriginLocal,
		}
		disk, err := os.ReadFile(filepath.Join(skillsDir, filepath.FromSlash(s.RelPath)))
		switch {
		case err != nil:
			it.State = StateMissing
			it.Fix = fixSkillInstall
		case bytes.Equal(disk, []byte(s.Content)):
			it.State = StateOK
		default:
			it.State = StateDivergent
			it.Detail = "differs from the canonical skill of this cerne build"
			it.Fix = fixSkillInstall
		}
		items = append(items, it)
	}

	// Learned skills: what SyncLearned would write vs what is on disk.
	want := map[string]string{}
	learnedKnown := opts.RepoDir != ""
	if learnedKnown {
		if learned, err := skills.LoadGoldenPaths(opts.RepoDir); err == nil {
			want = skills.WantLearned(learned)
		} else {
			learnedKnown = false
		}
	}
	if learnedKnown {
		names := make([]string, 0, len(want))
		for name := range want {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			it := Item{
				Class:  "skills",
				Name:   name,
				Path:   filepath.Join(".claude", "skills", name, "SKILL.md"),
				Origin: OriginLocal,
			}
			disk, err := os.ReadFile(filepath.Join(skillsDir, name, "SKILL.md"))
			switch {
			case err != nil:
				it.State = StateMissing
				it.Detail = "golden-path entity not materialized"
				it.Fix = fixSkillSync
			case string(disk) == want[name]:
				it.State = StateOK
			default:
				it.State = StateDivergent
				it.Detail = "differs from the golden-path entity's render"
				it.Fix = fixSkillSync
			}
			items = append(items, it)
		}
	}

	// Remaining project skill dirs: learned orphans (backing entity gone) or
	// third-party skills the project carries.
	if entries, err := os.ReadDir(skillsDir); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() && !canonical[e.Name()] {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if _, expected := want[name]; expected {
				continue
			}
			it := Item{
				Class:  "skills",
				Name:   name,
				Path:   filepath.Join(".claude", "skills", name),
				Origin: OriginProject,
			}
			if strings.HasPrefix(name, skills.LearnedPrefix) && learnedKnown {
				it.Origin = OriginLocal
				it.State = StateDivergent
				it.Detail = "no backing golden-path entity in the backend"
				it.Fix = fixSkillSync
			} else {
				it.State = StateUnmanaged
			}
			items = append(items, it)
		}
	}

	// User-level skills: present on this machine only.
	if opts.Home != "" {
		userDir := filepath.Join(opts.Home, ".claude", "skills")
		if entries, err := os.ReadDir(userDir); err == nil {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				if e.IsDir() {
					names = append(names, e.Name())
				}
			}
			sort.Strings(names)
			for _, name := range names {
				items = append(items, Item{
					Class:       "skills",
					Name:        name,
					Path:        filepath.Join(userDir, name),
					Origin:      OriginUser,
					State:       StateConfigured,
					OutsideRepo: true,
				})
			}
		}
	}

	return items
}

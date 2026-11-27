package harness

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/skills"
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

	stale := map[string]bool{}
	for _, n := range skills.Stale(opts.ProjDir) {
		stale[n] = true
	}
	canonical := map[string]bool{}
	for _, s := range skills.All() {
		canonical[s.Name] = true
		it := Item{
			Class:  "skills",
			Name:   s.Name,
			Path:   filepath.Join(".claude", "skills", filepath.FromSlash(s.RelPath)),
			Origin: OriginLocal,
		}
		switch {
		case !stale[s.Name]:
			it.State = StateOK
		case !fileExists(filepath.Join(skillsDir, filepath.FromSlash(s.RelPath))):
			it.State = StateMissing
			it.Fix = fixSkillInstall
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

	// Project .cursor/skills/ — Cursor reads natively (R-0066).
	// cerne-authoring, if installed, is compared against the canonical embed;
	// third-party skills are listed as unmanaged.
	items = append(items, cursorSkillItems(opts.ProjDir, false)...)

	// User ~/.cursor/skills/ — Cursor reads natively.
	if opts.Home != "" {
		items = append(items, cursorSkillItems(opts.Home, true)...)
	}

	// Project .agents/skills/ — read by Cursor, Codex, Gemini, Copilot
	// (agent-skills standard, R-0064).
	items = append(items, agentsSkillItems(opts.ProjDir, false)...)

	// User ~/.agents/skills/.
	if opts.Home != "" {
		items = append(items, agentsSkillItems(opts.Home, true)...)
	}

	return items
}

// cursorSkillItems lists skills under <base>/.cursor/skills/ — the cerne-authoring
// skill is compared against the embed; others are unmanaged.
func cursorSkillItems(base string, outside bool) []Item {
	dir := filepath.Join(base, ".cursor", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	origin := OriginProject
	if outside {
		origin = OriginUser
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var items []Item
	const cursorAuthoringName = "cerne-authoring"
	for _, name := range names {
		skillDir := filepath.Join(dir, name)
		it := Item{
			Class:       "skills",
			Name:        name,
			Path:        filepath.Join(skillDir, "SKILL.md"),
			Origin:      origin,
			OutsideRepo: outside,
		}
		if name == cursorAuthoringName && !outside {
			// Managed copy: the same check as the "cursor: skill" row.
			switch skills.CursorSkillState(base) {
			case hooks.FileOK:
				it.State = StateOK
			case hooks.FileDivergent:
				it.State, it.Fix = StateDivergent, fixSkillInstall
				it.Detail = "differs from the canonical skill of this cerne build"
			default:
				it.State, it.Fix = StateMissing, fixSkillInstall
			}
		} else {
			it.State = StateConfigured
			if name == cursorAuthoringName {
				// User-level cerne-authoring — not managed by this project's cerne.
				it.State = StateUnmanaged
			}
		}
		items = append(items, it)
	}
	return items
}

// agentsSkillItems lists skills under <base>/.agents/skills/ as unmanaged.
func agentsSkillItems(base string, outside bool) []Item {
	dir := filepath.Join(base, ".agents", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	origin := OriginProject
	if outside {
		origin = OriginUser
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	items := make([]Item, 0, len(names))
	for _, name := range names {
		items = append(items, Item{
			Class:       "skills",
			Name:        name,
			Path:        filepath.Join(dir, name),
			Origin:      origin,
			State:       StateUnmanaged,
			OutsideRepo: outside,
		})
	}
	return items
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

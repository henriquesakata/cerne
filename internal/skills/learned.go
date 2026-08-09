package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/slug"
)

// StatusRemoved reports a generated skill deleted because its source entity is
// no longer active or no longer marked golden-path.
const StatusRemoved Status = "removed"

// learnedPrefix namespaces the generated-skill directories SyncLearned owns
// under .claude/skills/. Everything else in the tree is out of bounds.
const learnedPrefix = "learned-"

// Learned is one golden-path entity to materialize as a generated agent skill.
type Learned struct {
	ID         string // cerne id (N-NNNN / RB-NNNN), the provenance anchor
	Title      string
	Body       string // full entity markdown body
	Components []string
}

// SyncLearned reconciles the learned-* namespace of <projDir>/.claude/skills/
// against items: one generated skill per entity (created/updated/unchanged),
// and any learned-* directory without a backing item is removed. Directories
// outside the learned- prefix are never touched.
func SyncLearned(projDir string, items []Learned) ([]Result, error) {
	skillsDir := filepath.Join(projDir, ".claude", "skills")
	want := WantLearned(items)

	results := make([]Result, 0, len(items))
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		status, err := writeSkill(filepath.Join(skillsDir, name, "SKILL.md"), want[name])
		if err != nil {
			return nil, err
		}
		results = append(results, Result{Skill: name, Status: status})
	}

	entries, err := os.ReadDir(skillsDir)
	if os.IsNotExist(err) {
		return results, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), learnedPrefix) {
			continue
		}
		if _, ok := want[e.Name()]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(skillsDir, e.Name())); err != nil {
			return nil, err
		}
		results = append(results, Result{Skill: e.Name(), Status: StatusRemoved})
	}
	return results, nil
}

// WantLearned returns the target learned-* namespace for items — dir name to
// rendered SKILL.md content, exactly what SyncLearned writes. The harness
// inspection (F-0251) diffs the disk against this without writing.
func WantLearned(items []Learned) map[string]string {
	want := make(map[string]string, len(items))
	for _, it := range items {
		name := learnedPrefix + slug.Make(it.ID+" "+it.Title)
		want[name] = renderLearned(name, it)
	}
	return want
}

// LearnedPrefix exposes the namespace prefix generated skills live under.
const LearnedPrefix = learnedPrefix

// renderLearned builds the generated SKILL.md: deterministic frontmatter (the
// description is what makes the skill fire, so it carries the title, the first
// paragraph of the body, and the components) plus the entity body under a
// provenance banner.
func renderLearned(name string, it Learned) string {
	desc := it.Title + ". " + firstParagraph(it.Body)
	if len(it.Components) > 0 {
		desc += " Applies to: " + strings.Join(it.Components, ", ") + "."
	}
	desc += fmt.Sprintf(" Golden path harvested from the cerne backend (%s).", it.ID)

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	b.WriteString("description: " + strconvQuote(desc) + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + it.Title + "\n\n")
	b.WriteString(fmt.Sprintf("> Generated from %s by `cerne skill sync` — do not edit; update the entity (`cerne show %s`) and re-sync.\n\n", it.ID, it.ID))
	b.WriteString(strings.TrimRight(it.Body, "\n"))
	b.WriteString("\n")
	return b.String()
}

// firstParagraph flattens the body's first paragraph to one line, truncated so
// the description stays a trigger, not a dump.
func firstParagraph(body string) string {
	para := strings.TrimSpace(body)
	if i := strings.Index(para, "\n\n"); i >= 0 {
		para = para[:i]
	}
	para = strings.Join(strings.Fields(para), " ")
	const max = 300
	if len(para) > max {
		cut := para[:max]
		if i := strings.LastIndex(cut, " "); i > 0 {
			cut = cut[:i]
		}
		para = cut + "…"
	}
	return para
}

// strconvQuote wraps s in double quotes with YAML-safe escaping of quotes and
// backslashes (the description may contain backticks and punctuation freely).
func strconvQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

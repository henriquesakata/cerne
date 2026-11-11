package materialize

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/store"
)

// okfIndexEntry is one concept row of the root index.md: title linked to the
// materialized rel path, with the derived description when the concept has one.
type okfIndexEntry struct {
	title, rel, desc string
}

// okfIndexSection groups the root index entries of one entity kind under its
// display heading; empty sections are omitted from the output.
type okfIndexSection struct {
	name    string
	entries []okfIndexEntry
}

// okfIndexSections assembles the root index sections in the fixed kind order.
// Only entities materialized by this pull (idPath membership, which is itself
// manifest-gated) are listed, so index links never dangle by construction.
func okfIndexSections(features []store.Feature, decisions []store.Decision, references []store.Reference, notes []store.Note, runbooks []store.Runbook, idPath, descs map[string]string) []okfIndexSection {
	sections := make([]okfIndexSection, 5)
	sections[0].name = "Features"
	for _, f := range features {
		if rel, ok := idPath[f.ID]; ok {
			sections[0].entries = append(sections[0].entries, okfIndexEntry{f.Title, rel, descs[f.ID]})
		}
	}
	sections[1].name = "Decisions"
	for _, d := range decisions {
		if rel, ok := idPath[d.ID]; ok {
			sections[1].entries = append(sections[1].entries, okfIndexEntry{d.Title, rel, descs[d.ID]})
		}
	}
	sections[2].name = "References"
	for _, r := range references {
		if rel, ok := idPath[r.ID]; ok {
			sections[2].entries = append(sections[2].entries, okfIndexEntry{r.Title, rel, descs[r.ID]})
		}
	}
	sections[3].name = "Notes"
	for _, n := range notes {
		if rel, ok := idPath[n.ID]; ok {
			sections[3].entries = append(sections[3].entries, okfIndexEntry{n.Title, rel, descs[n.ID]})
		}
	}
	sections[4].name = "Runbooks"
	for _, rb := range runbooks {
		if rel, ok := idPath[rb.ID]; ok {
			sections[4].entries = append(sections[4].entries, okfIndexEntry{rb.Title, rel, descs[rb.ID]})
		}
	}
	return sections
}

// writeRootIndex emits the bundle-root index.md (F-0166): the OKF navigable
// entry point. Its frontmatter carries only okf_version — §6 of the spec says
// index files have no frontmatter, with the versioning section's single
// exception for the bundle root — followed by one section per non-empty kind,
// each concept as `* [title](rel) — description` (dash omitted without one).
func writeRootIndex(docsDir, projectName string, sections []okfIndexSection, manifest map[string]string) error {
	var b strings.Builder
	b.WriteString("---\nokf_version: \"0.1\"\n---\n\n# " + projectName + "\n")
	for _, sec := range sections {
		if len(sec.entries) == 0 {
			continue
		}
		b.WriteString("\n## " + sec.name + "\n\n")
		for _, e := range sec.entries {
			b.WriteString("* [" + e.title + "](" + e.rel + ")")
			if e.desc != "" {
				b.WriteString(" — " + e.desc)
			}
			b.WriteString("\n")
		}
	}
	sum, err := writeFile(docsDir, "index.md", []byte(b.String()))
	if err != nil {
		return err
	}
	manifest["index.md"] = sum
	return nil
}

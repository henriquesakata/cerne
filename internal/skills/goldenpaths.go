package skills

import (
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// GoldenPathComponent marks a note/runbook for materialization as a generated
// agent skill (F-0178).
const GoldenPathComponent = "golden-path"

// LoadGoldenPaths collects the active notes and runbooks tagged golden-path,
// with their bodies stripped of the entity frontmatter — the input SyncLearned
// reconciles against. Shared by `cerne pull`, `cerne skill sync` and the
// harness inspection (F-0251).
func LoadGoldenPaths(repoDir string) ([]Learned, error) {
	var items []Learned
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		if n.State != domain.NoteActive || !hasGoldenPath(n.Components) {
			continue
		}
		body, err := store.LoadNoteContent(repoDir, n.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, Learned{ID: n.ID, Title: n.Title, Body: stripEntityFrontmatter(body), Components: n.Components})
	}
	rbs, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return nil, err
	}
	for _, rb := range rbs {
		if rb.State != domain.RunbookActive || !hasGoldenPath(rb.Components) {
			continue
		}
		body, err := store.LoadRunbookContent(repoDir, rb.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, Learned{ID: rb.ID, Title: rb.Title, Body: stripEntityFrontmatter(body), Components: rb.Components})
	}
	return items, nil
}

func hasGoldenPath(components []string) bool {
	for _, c := range components {
		if c == GoldenPathComponent {
			return true
		}
	}
	return false
}

// stripEntityFrontmatter drops a leading ----delimited block (the cerne_id
// frontmatter of the stored document) so it doesn't collide with the generated
// skill's own frontmatter.
func stripEntityFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return body
	}
	return strings.TrimLeft(rest[end+len("\n---\n"):], "\n")
}

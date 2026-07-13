package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/skills"
	"gitlab.com/rockt/cerne/internal/store"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install cerne's Claude Code skills into a project",
	}
	cmd.AddCommand(newSkillInstallCmd())
	cmd.AddCommand(newSkillSyncCmd())
	return cmd
}

func newSkillInstallCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install/update the canonical cerne skills under .claude/skills/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillInstall(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	return cmd
}

// goldenPathComponent marks a note/runbook for materialization as a generated
// agent skill.
const goldenPathComponent = "golden-path"

func newSkillSyncCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Materialize golden-path notes/runbooks as generated agent skills",
		Long:  "Reconciles the learned-* namespace under .claude/skills/ against the active notes and runbooks tagged with the 'golden-path' component: one generated skill per entity, orphans removed. Generated skills are tool-managed — edit the entity in the backend and re-run the sync, never the generated file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillSync(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	return cmd
}

func runSkillSync(cmd *cobra.Command, dir string) error {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	items, err := loadGoldenPaths(ctx.repoDir)
	if err != nil {
		return err
	}
	results, err := skills.SyncLearned(ctx.projDir, items)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No golden-path entities to materialize (tag a note/runbook with the 'golden-path' component).")
		return nil
	}
	printSkillResults(cmd.OutOrStdout(), results)
	return nil
}

// loadGoldenPaths collects the active notes and runbooks tagged golden-path,
// with their bodies stripped of the entity frontmatter.
func loadGoldenPaths(repoDir string) ([]skills.Learned, error) {
	var items []skills.Learned
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
		items = append(items, skills.Learned{ID: n.ID, Title: n.Title, Body: stripEntityFrontmatter(body), Components: n.Components})
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
		items = append(items, skills.Learned{ID: rb.ID, Title: rb.Title, Body: stripEntityFrontmatter(body), Components: rb.Components})
	}
	return items, nil
}

func hasGoldenPath(components []string) bool {
	for _, c := range components {
		if c == goldenPathComponent {
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

// skillStatusLabel maps an install outcome to its label.
var skillStatusLabel = map[skills.Status]string{
	skills.StatusCreated:   "created",
	skills.StatusUpdated:   "updated",
	skills.StatusUnchanged: "up to date",
}

func runSkillInstall(cmd *cobra.Command, dir string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	results, err := skills.Install(dir)
	if err != nil {
		return err
	}
	printSkillResults(cmd.OutOrStdout(), results)
	return nil
}

// printSkillResults writes one status line per installed skill. Shared by
// `cerne skill install` and `cerne init`.
func printSkillResults(out io.Writer, results []skills.Result) {
	for _, r := range results {
		label := skillStatusLabel[r.Status]
		if label == "" {
			label = string(r.Status)
		}
		fmt.Fprintf(out, "%s: %s in .claude/skills/%s\n", r.Skill, label, r.Skill)
	}
}

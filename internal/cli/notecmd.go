package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/slug"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

func newNoteCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Create and manage notes (durable team knowledge) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newNoteNewCmd(&dir))
	cmd.AddCommand(verbs.MarkChat(newNoteListCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newNoteTagsCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newNoteShowCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newNoteRetagCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newNoteKeepCmd(&dir)))
	cmd.AddCommand(newNoteRetireCmd(&dir))
	cmd.AddCommand(newNoteSubstituirCmd(&dir))
	cmd.AddCommand(verbs.MarkChat(newErrataCmd(&dir)))
	return cmd
}

func newNoteListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	var facet, component, onPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List notes (durable knowledge) — active by default; --all includes history",
		Long: "Lists notes read straight from the backend (store), not from the '.docs/' projection — so it sees the " +
			"knowledge even when '.docs/' is stale (no 'cerne pull'). By default shows only the " +
			"active ones; --all includes the superseded (history). Each line carries id, state, title and markers " +
			"(durable, informs F-/D-/RB-/N-, facet/component, successor). Filter by --facet and/or --component (logical AND). " +
			"--path <file> lists the active notes that cite that file (full path, then dir suffix, then a unique bare name; newest first), the same rule as the edit hook, without its cap of 3. " +
			"--json for structured output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteList(cmd, *dir, all, asJSON, facet, component, onPath)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include superseded notes (history)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.Flags().StringVar(&facet, "facet", "", "filter by facet (e.g. infra)")
	cmd.Flags().StringVar(&component, "component", "", "filter by component (e.g. gitea)")
	cmd.Flags().StringVar(&onPath, "path", "", "only the active notes that cite this file (repo-relative or absolute)")
	return cmd
}

// notesCitingPath orders notes (already filtered) by how specifically they cite
// file, dropping the ones that do not; shared rule with the work-gate hook.
func notesCitingPath(ctx projCtx, notes []store.Note, file string) ([]store.Note, error) {
	rel := filepath.ToSlash(file)
	if filepath.IsAbs(file) {
		r, err := filepath.Rel(ctx.projDir, file)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(r)
	}
	cacheDir := filepath.Dir(ctx.repoDir)
	found, err := app.NotesForPath(context.Background(), cacheDir, ctx.repoDir, rel, app.UniqueNames(context.Background(), ctx.projDir))
	if err != nil {
		return nil, err
	}
	byID := map[string]store.Note{}
	for _, n := range notes {
		byID[n.ID] = n
	}
	var out []store.Note
	for _, f := range found {
		if n, ok := byID[f.ID]; ok {
			out = append(out, n)
		}
	}
	return out, nil
}

func runNoteList(cmd *cobra.Command, dir string, all, asJSON bool, facet, component, onPath string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	notes, err := store.LoadAllNotes(ctx.repoDir)
	if err != nil {
		return err
	}
	// Normalize the filters to the stored form so '--facet Infra'/'--component
	// Gitea' match the lower/slug values persisted on the note.
	facetQ := strings.ToLower(strings.TrimSpace(facet))
	compQ := slug.Make(component)
	w := cmd.OutOrStdout()
	shown := make([]store.Note, 0, len(notes))
	for _, n := range notes {
		if !all && n.State != domain.NoteActive {
			continue
		}
		if facetQ != "" && !slices.Contains(n.Facets, facetQ) {
			continue
		}
		if compQ != "" && !slices.Contains(n.Components, compQ) {
			continue
		}
		shown = append(shown, n)
	}
	if onPath != "" {
		if shown, err = notesCitingPath(ctx, shown, onPath); err != nil {
			return err
		}
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Notes []store.Note `json:"notes"`
		}{shown})
	}

	for _, n := range shown {
		line := fmt.Sprintf("%s  [%s]  %s", n.ID, n.State, n.Title)
		var tags []string
		// The reserved component 'convention' marks a prescriptive convention
		// (F-0165): surface it first so an agent tells guidance from discovery.
		if slices.Contains(n.Components, domain.ConventionComponent) {
			tags = append(tags, "convention")
		}
		if n.Durable {
			tags = append(tags, "durable")
		}
		if len(n.Facets) > 0 {
			tags = append(tags, "facet "+strings.Join(n.Facets, ","))
		}
		if len(n.Components) > 0 {
			tags = append(tags, "comp "+strings.Join(n.Components, ","))
		}
		if len(n.Informs) > 0 {
			tags = append(tags, "informs "+strings.Join(n.Informs, ","))
		}
		if n.SupersededBy != "" {
			tags = append(tags, "→ "+n.SupersededBy)
		}
		if len(tags) > 0 {
			line += "  · " + strings.Join(tags, " · ")
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 {
		if onPath != "" {
			fmt.Fprintf(w, "No active notes cite %s (the index is rebuilt by 'cerne pull').\n", onPath)
		} else if all {
			fmt.Fprintln(w, "No notes registered. Use 'cerne note new \"<title>\"' to register durable knowledge.")
		} else {
			fmt.Fprintln(w, "No active notes. Use 'cerne note new \"<title>\"' to register one, or 'cerne note list --all' to see history.")
		}
	}
	return nil
}

func newNoteTagsCmd(dir *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "Show the tag catalog of active notes (each facet/component with counts)",
		Long: "Aggregates the tags across ACTIVE notes: each distinct facet and component with the count of notes " +
			"carrying it. This is the topic menu — read it first, then pull only the relevant knowledge with " +
			"'cerne note list --facet <f>' / '--component <c>'. Superseded notes are history and not counted. " +
			"--json for structured output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteTags(cmd, *dir, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runNoteTags(cmd *cobra.Command, dir string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	cat, err := app.NoteTagCatalog(ctx.repoDir)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(cat)
	}
	if len(cat.Facets) == 0 && len(cat.Components) == 0 {
		fmt.Fprintln(w, "No tags in use. Tag notes with 'cerne note new --facet/--component' (or 'cerne note retag').")
		return nil
	}
	printAxis := func(title string, counts map[string]int) {
		if len(counts) == 0 {
			return
		}
		fmt.Fprintln(w, title+":")
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "  %s (%d)\n", k, counts[k])
		}
	}
	printAxis("Facets", cat.Facets)
	printAxis("Components", cat.Components)
	fmt.Fprintln(w, "Pull a topic with 'cerne note list --facet <f>' or 'cerne note list --component <c>'.")
	return nil
}

func newNoteShowCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a note (metadata + body) read straight from the backend",
		Long: "Prints the metadata (id, state, durable, informs, reason/successor) and the note's markdown body, " +
			"both read straight from the backend (store) — immune to a stale '.docs/' projection.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteShow(cmd, *dir, args[0])
		},
	}
	return cmd
}

func runNoteShow(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	n, err := store.LoadNote(ctx.repoDir, id)
	if err != nil {
		return fmt.Errorf("note %s: %w", id, err)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s  [%s]  %s\n", n.ID, n.State, n.Title)
	if n.Durable {
		fmt.Fprintln(w, "durable: yes")
	}
	if len(n.Facets) > 0 {
		fmt.Fprintf(w, "facets: %s\n", strings.Join(n.Facets, ", "))
	}
	if len(n.Components) > 0 {
		fmt.Fprintf(w, "components: %s\n", strings.Join(n.Components, ", "))
	}
	if len(n.Informs) > 0 {
		fmt.Fprintf(w, "informs: %s\n", strings.Join(n.Informs, ", "))
	}
	if n.SupersededBy != "" {
		fmt.Fprintf(w, "superseded by: %s\n", n.SupersededBy)
	}
	if n.Reason != "" {
		fmt.Fprintf(w, "reason: %s\n", n.Reason)
	}
	body, err := store.LoadNoteContent(ctx.repoDir, id)
	if err != nil {
		return fmt.Errorf("note %s body: %w", id, err)
	}
	fmt.Fprintf(w, "\n%s\n", strings.TrimRight(body, "\n"))
	return nil
}

func newNoteNewCmd(dir *string) *cobra.Command {
	var informs, file, facet, component string
	var force bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Register durable team knowledge as a note (active)",
		Long: "Registers durable project knowledge (gotchas, invariants, tacit conventions) as an active " +
			"note. The body is free text, from --file or standard input (when piped); without a body the note " +
			"is born as a stub for you to fill in. Use --informs to link the note to the entities (F-/D-/RB-/N-) it " +
			"informs. Use --facet (closed axis: infra,code,data,process,auth) and --component (subsystem/tool, " +
			"e.g. gitea,kpack) to organize — both multi-valued, comma-separated. " +
			"A live note with a similar title refuses the creation (F-0324); --force overrides it. " +
			"Personal/machine knowledge (paths, preferences) does NOT go here — it stays in local memory.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteNew(cmd, *dir, args[0], informs, file, facet, component, force)
		},
	}
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/D-/RB-/N- ids this note informs")
	cmd.Flags().StringVar(&file, "file", "", "file with the note body (defaults to piped stdin)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (comma-separated; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (comma-separated subsystem/tool tags, e.g. gitea,kpack)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0324) for a consciously distinct note")
	return verbs.MarkChatTool(cmd)
}

func runNoteNew(cmd *cobra.Command, dir, title, informs, file, facet, component string, force bool) error {
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	n, err := app.CreateNote(ctx.repoDir, ctx.projDir, title, body, splitInforms(informs), splitInforms(facet), splitInforms(component), false, force)
	if err != nil {
		return err
	}
	// A note created with a body is already real content, committed and pushed
	// by CreateNote (CommitAndSnapshot) — there is nothing to fill or push.
	// Confirm it is synced and point at 'cerne pull', so the caller doesn't
	// pull+inspect just to verify a complete operation. A bodyless note is a
	// stub: keep the fill + push next step.
	if strings.TrimSpace(body) != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s created (active) with a body — already synced. Run 'cerne pull' to materialize locally.\n", n.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (active). Next step: fill in the note at '.docs/current/notes/%s-<slug>.md' and run 'cerne push'.\n", n.ID, n.ID)
	return nil
}

// readOptionalBody lives in stdinbody.go (F-0157): same contract, plus the
// readiness window that keeps an open silent stdin pipe from blocking forever.

func newNoteRetireCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retire <id>",
		Short: "Retire an obsolete note (goes to superseded, no successor; --reason required)",
		Long: "Moves a note from active to superseded WITHOUT a successor — for knowledge that no longer holds " +
			"(e.g. the feature the note informed already shipped). The note leaves the current state (.docs/CONTEXT.md) and " +
			"becomes searchable history ('cerne search'/'ask'). To swap it for a corrected version, use " +
			"'cerne note substituir --by'. 'cerne lint' flags (stale-note) notes whose --informs already closed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteRetire(cmd, *dir, args[0], reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "reason for retiring (required)")
	return verbs.MarkLifecycle(cmd)
}

func runNoteRetire(cmd *cobra.Command, dir, id, reason string) error {
	// Validate the required reason before resolveProject so bad invocations
	// fail fast without touching origin. retire is substituir with no successor.
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("--reason is required")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionNote(ctx.repoDir, ctx.projDir, id, "", reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retired: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

func newNoteKeepCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keep <id>",
		Short: "Mark a note as durable (lint stops flagging it as stale-note)",
		Long: "Marks an active note as durable knowledge: the human verdict that it still holds, even if it " +
			"informs (--informs) an already-closed feature/decision. The note stays active and materialized; " +
			"'cerne lint' stops flagging it as stale-note. Idempotent. To retire it for good, use 'cerne note retire'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteKeep(cmd, *dir, args[0])
		},
	}
	return cmd
}

func runNoteKeep(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	n, err := app.KeepNote(ctx.repoDir, ctx.projDir, id)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s marked as durable — 'cerne lint' no longer flags it (stale-note).\n", n.ID)
	return nil
}

func newNoteRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Reset the tags (facet/component) of an active note",
		Long: "Sets or replaces the tag axes of an active note, without creating a successor — preserves durable, " +
			"informs and body. Changes only the axis passed: 'retag N-0001 --component gitea' keeps the facets; " +
			"'--facet \"\"' clears the facets. A facet outside the closed set (infra,code,data,process,auth) is rejected.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"))
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "new facets (CSV; closed set) — empty clears")
	cmd.Flags().StringVar(&component, "component", "", "new components (CSV) — empty clears")
	return cmd
}

func runNoteRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("provide at least --facet or --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	n, err := app.RetagNote(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retagged — facets: [%s] · components: [%s]\n",
		n.ID, strings.Join(n.Facets, ", "), strings.Join(n.Components, ", "))
	return nil
}

func newNoteSubstituirCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "substituir <id>",
		Short: "Supersede or retire a note (--by optional; --reason required)",
		Long: "Moves a note from active to superseded. With --by, the successor note supersedes it (the corrected " +
			"knowledge materializes in its place); without --by, the note is simply retired. The old note becomes " +
			"searchable history ('cerne search'/'ask').",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteSubstituir(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id of the note that supersedes (optional; empty = just retire)")
	cmd.Flags().StringVar(&reason, "reason", "", "reason for the supersession/retirement (required)")
	return verbs.MarkLifecycle(cmd)
}

func runNoteSubstituir(cmd *cobra.Command, dir, id, by, reason string) error {
	// Validate the required reason before resolveProject so bad invocations
	// fail fast without touching origin.
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("--reason is required")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionNote(ctx.repoDir, ctx.projDir, id, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

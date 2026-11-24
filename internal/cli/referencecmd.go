package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

func newReferenceCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "reference",
		Short: "Register and manage references in the backend",
		Long: "Register and manage references in the backend. Three commands reach the network: 'reference check' " +
			"(one request per source), 'reference archive', and 'reference new' when a human turned references.archive on " +
			"(it then tries to archive the page it registers; --no-archive keeps one source out). Everything else, " +
			"lint, pull and push included, reads what is recorded.",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newReferenceNewCmd(&dir))
	cmd.AddCommand(verbs.MarkChat(newReferenceListCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newReferenceRetagCmd(&dir)))
	cmd.AddCommand(newReferenceRetireCmd(&dir))
	cmd.AddCommand(newReferenceLinkCmd(&dir))
	cmd.AddCommand(newReferenceCheckCmd(&dir))
	cmd.AddCommand(verbs.MarkChat(newReferenceKeepCmd(&dir)))
	cmd.AddCommand(newReferenceArchiveCmd(&dir))
	return cmd
}

// archiveNet is the network reference new and reference archive use (F-0503);
// a var so tests point it at fakes.
var archiveNet = app.NewArchiveNet

// referenceCheckTimeout bounds each URL of `reference check` (F-0501).
const referenceCheckTimeout = 15 * time.Second

func newReferenceCheckCmd(dir *string) *cobra.Command {
	return verbs.MarkLocal(&cobra.Command{
		Use:   "check [id...]",
		Short: "Fetch each current external source and record whether it changed or went away",
		Long: "Requests the URL of every current external reference (or only the ids given) and records the date, the " +
			"HTTP status and a hash of the visible text. The first check writes the baseline. Later ones report: ok, " +
			"changed (the text differs from the baseline; confirm with 'cerne reference keep'), gone (404, 410 or a " +
			"domain that no longer exists; 'cerne lint' then flags reference-gone) or transient (timeout, 5xx, 429 or any " +
			"other failure: nothing is recorded but the event). Each request carries just the URL and a generic cerne " +
			"User-Agent. A gone or changed line ends with the archived copy, when the reference has one.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			res, err := app.CheckReferences(ctx.repoDir, ctx.projDir, args, app.NewHTTPFetcher(referenceCheckTimeout))
			if err != nil {
				return err
			}
			printReferenceChecks(cmd.OutOrStdout(), res)
			return nil
		},
	})
}

// printReferenceChecks writes one line per reference — `R-0012  gone (HTTP
// 404)  informs F-0003` — and the summary. The line format is stable: later
// features append columns.
func printReferenceChecks(w io.Writer, res []app.CheckResult) {
	n := map[string]int{}
	for _, r := range res {
		n[r.Result]++
		line := r.ID + "  " + r.Result
		if r.Detail != "" {
			line += " (" + r.Detail + ")"
		}
		if (r.Result == "changed" || r.Result == "gone") && len(r.Informs) > 0 {
			line += "  informs " + strings.Join(r.Informs, ", ")
		}
		if (r.Result == "changed" || r.Result == "gone") && r.Archive != "" {
			line += "  archive: " + r.Archive
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "checked %d: %d ok, %d changed, %d gone, %d transient\n",
		len(res), n["ok"], n["changed"], n["gone"], n["transient"])
}

func newReferenceArchiveCmd(dir *string) *cobra.Command {
	var dryRun, optOut bool
	cmd := &cobra.Command{
		Use:   "archive [id...]",
		Short: "Keep the link of a dated copy of each public external source (Wayback Machine)",
		Long: "Sends the URL of each current external reference that has no copy yet, or whose last attempt is " +
			"pending (or only the ids given), to the Wayback Machine of the Internet Archive, and records the link " +
			"and date of the copy: the capture nearest the day the reference was registered when the archive has " +
			"one (existing), else a new one (saved), falling back on that capture when Save Page Now refuses. " +
			"Only the URL is sent. Nothing leaves the machine unless a human turned references.archive " +
			"on, and never for an internal host, an unregistered_source_ignore item, a URL with credentials or a " +
			"secret parameter, a name that resolves to a private address, a page an anonymous GET does not read " +
			"with a 200, or a reference registered with --no-archive: that opt-out holds for the URL, so it bars " +
			"every reference with the same one. --opt-out marks the ids given the same way, needs ids and reaches " +
			"no network. --dry-run runs every one of those checks and " +
			"lists what would be sent, sending nothing to the archive service and recording nothing.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			if optOut {
				done, err := app.OptOutReferences(ctx.repoDir, ctx.projDir, args)
				if err != nil {
					return err
				}
				for _, id := range done {
					fmt.Fprintln(cmd.OutOrStdout(), id+"  no-archive")
				}
				return nil
			}
			res, err := app.ArchiveReferences(ctx.repoDir, ctx.projDir, args, archiveNet(), dryRun)
			if err != nil {
				return err
			}
			printReferenceArchives(cmd.OutOrStdout(), res, dryRun)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "run every check (filters, DNS, anonymous GET) and list what would be sent; send nothing to the archive service")
	cmd.Flags().BoolVar(&optOut, "opt-out", false, "mark the ids given as never to be archived, them and any reference with the same URL; reaches no network and works with references.archive off")
	cmd.MarkFlagsMutuallyExclusive("opt-out", "dry-run")
	return verbs.MarkLocal(cmd)
}

// archiveText is an archive outcome as show and reference new print it:
// `<link> (saved 2026-11-24)`, `pending (save failed: 429)`, `not public
// (HTTP 404)`, `skipped (internal host)`.
func archiveText(a store.ReferenceArchive) string {
	switch a.Status {
	case "archived":
		return a.URL + " (" + a.Origin + " " + a.CapturedAt.Format(time.DateOnly) + ")"
	case "not_public":
		return "not public (" + a.Reason + ")"
	}
	return a.Status + " (" + a.Reason + ")"
}

// printReferenceArchives writes one line per reference — `R-0012  archived
// <link>  (saved 2026-11-24)`, `R-0013  existing  <link>  (captured
// 2025-03-02)`, `R-0014  pending  (save failed: 429)`, `R-0015  not public
// (HTTP 404)`, `R-0016  skipped (internal host)` — and the counts. A dry run
// ends each line with the source URL and says nothing was sent.
func printReferenceArchives(w io.Writer, res []app.ArchiveResult, dryRun bool) {
	if len(res) == 0 {
		fmt.Fprintln(w, "nothing to archive: every current external reference has a copy, a final outcome or --no-archive")
		return
	}
	n := map[string]int{}
	for _, r := range res {
		line := r.ID + "  "
		key := r.Status
		switch {
		case r.Status == "archived" && r.Origin == "existing":
			key = "existing"
			line += "existing  " + r.URL + "  (captured " + r.CapturedAt.Format(time.DateOnly) + ")"
		case r.Status == "archived":
			line += "archived  " + r.URL + "  (saved " + r.CapturedAt.Format(time.DateOnly) + ")"
		case r.Status == "pending":
			line += "pending  (" + r.Reason + ")"
		case r.Status == "would_send":
			line += "would send"
		default:
			line += archiveText(r.ReferenceArchive)
		}
		if dryRun {
			line += "  " + r.Source
		}
		n[key]++
		fmt.Fprintln(w, line)
	}
	if dryRun {
		fmt.Fprintf(w, "dry run, nothing sent: %d would send, %d pending, %d not public, %d skipped\n",
			n["would_send"], n["pending"], n["not_public"], n["skipped"])
		return
	}
	fmt.Fprintf(w, "processed %d: %d archived, %d existing, %d pending, %d not public, %d skipped\n",
		len(res), n["archived"], n["existing"], n["pending"], n["not_public"], n["skipped"])
}

func newReferenceKeepCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keep <id>",
		Short: "Accept the current page of a changed source as its new baseline",
		Long: "Confirms that a source flagged changed by 'cerne reference check' still supports what it informs: the " +
			"hash of the current text becomes the baseline and the changed state clears. Refused unless the last check " +
			"says changed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			r, err := app.KeepReference(ctx.repoDir, ctx.projDir, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s baseline updated: the current page is accepted\n", r.ID)
			return nil
		},
	}
	return cmd
}

func newReferenceRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	var force bool
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Reset the tags (facet/component) of a reference",
		Long: "Sets or replaces the tag axes of a reference in any state, without making it obsolete — preserves url, " +
			"informs and body. Changes only the axis passed: 'retag R-0001 --component gitea' keeps the facets; '--facet \"\"' " +
			"clears the facets. A facet outside the closed set (infra,code,data,process,auth) is rejected.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"), force)
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "new facets (CSV; closed set) — empty clears")
	cmd.Flags().StringVar(&component, "component", "", "new components (CSV) — empty clears")
	cmd.Flags().BoolVar(&force, "force", false, forceComponentHelp)
	return cmd
}

func runReferenceRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents, force bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("provide at least --facet or --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	notice := componentNotice(cmd, ctx.repoDir, component)
	r, err := app.RetagReference(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents, force)
	if err != nil {
		return err
	}
	notice()
	fmt.Fprintf(cmd.OutOrStdout(), "%s retagged — facets: [%s] · components: [%s]\n",
		r.ID, strings.Join(r.Facets, ", "), strings.Join(r.Components, ", "))
	return nil
}

func newReferenceRetireCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retire <id>",
		Short: "Retire a reference (current -> obsolete; --reason required)",
		Long: "Moves a current reference to obsolete: the source stays searchable as history but leaves the " +
			"current state. Use it for a duplicate (same URL registered twice) or a source that no longer holds.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceRetire(cmd, *dir, args[0], reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "reason for retiring (required)")
	return verbs.MarkLifecycle(cmd)
}

func runReferenceRetire(cmd *cobra.Command, dir, id, reason string) error {
	// Fail fast before touching origin, like note retire.
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("--reason is required")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.RetireReference(ctx.repoDir, ctx.projDir, id, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retired: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

func newReferenceNewCmd(dir *string) *cobra.Command {
	var url, informs, file, facet, component string
	var force, noArchive bool
	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Register an external source (article/doc) as a reference",
		Long: "Registers an external article/document as a reference (marked external + URL). " +
			"The content (text/summary) comes from --file or standard input and enters the index, " +
			"so 'ask' starts citing it. Use --informs to link the source to the entities " +
			"(F-/E-/T-/B-/D-/RB-/N-/I-) it motivated. Use --facet (closed axis: infra,code,data,process,auth) and " +
			"--component (subsystem/tool, e.g. gitea,kpack) to categorize — both multi-valued, " +
			"comma-separated. A live reference with the same normalized URL refuses the creation (F-0324); --force overrides it. The title argument may be omitted when the body (--file or stdin) opens with a `# ` heading: that heading becomes the title. " +
			"With references.archive on, the page is then archived as 'cerne reference archive' does; --no-archive marks the source as never sent to the archive service, now or later.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceNew(cmd, *dir, args, url, informs, file, facet, component, force, noArchive)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "source URL (required)")
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/E-/T-/B-/D-/RB-/N-/I- ids this source motivated")
	cmd.Flags().StringVar(&file, "file", "", "file with the source content (defaults to stdin)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (comma-separated; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (comma-separated subsystem/tool tags, e.g. gitea,kpack)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0324) for a consciously distinct source, and accept a component near an existing one (E-0005; check 'cerne tags' first)")
	cmd.Flags().BoolVar(&noArchive, "no-archive", false, "never send this source's URL to the archive service, now or later (F-0503)")
	_ = cmd.MarkFlagRequired("url")
	return verbs.MarkChatTool(cmd)
}

func runReferenceNew(cmd *cobra.Command, dir string, args []string, url, informs, file, facet, component string, force, noArchive bool) error {
	content, err := readReferenceContent(cmd, file)
	if err != nil {
		return err
	}
	title, err := newTitle(args, content)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	notice := componentNotice(cmd, ctx.repoDir, component)
	r, err := app.CreateExternalReference(ctx.repoDir, ctx.projDir, title, url, content, splitInforms(informs), splitInforms(facet), splitInforms(component), force, noArchive)
	if err != nil {
		return err
	}
	notice()
	fmt.Fprintf(cmd.OutOrStdout(), "%s registered (external: %s).\n", r.ID, r.URL)
	if noArchive || !project.ReferencesArchive(ctx.projDir) {
		return nil
	}
	// After the registration (F-0503 crit. 8): a failure here leaves the
	// reference registered and the next 'reference archive' retries it.
	res, err := app.ArchiveReferences(ctx.repoDir, ctx.projDir, []string{r.ID}, archiveNet(), false)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s is registered but its archive was not recorded: %v — run 'cerne reference archive %s'\n", r.ID, err, r.ID)
		return nil
	}
	if len(res) == 1 {
		fmt.Fprintln(cmd.OutOrStdout(), "archive: "+archiveText(res[0].ReferenceArchive))
	}
	return nil
}

func newReferenceListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	var facet, component string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List references — current by default; --all includes obsolete",
		Long: "Lists references read straight from the backend (store). By default shows only the " +
			"current ones; --all includes the obsolete. Each line carries id, state, title and markers " +
			"(external, facet/component, informs F-/E-/T-/B-/D-/RB-/N-/I-, URL). Filter by --facet and/or --component " +
			"(logical AND); --component takes a group (a,b,c), any of them, most matches first. --json for structured output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceList(cmd, *dir, all, asJSON, facet, component)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include obsolete references")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.Flags().StringVar(&facet, "facet", "", "filter by facet (e.g. infra)")
	cmd.Flags().StringVar(&component, "component", "", "filter by components, any of them (CSV, e.g. gitea,kpack)")
	return cmd
}

func runReferenceList(cmd *cobra.Command, dir string, all, asJSON bool, facet, component string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	refs, err := store.LoadAllReferences(ctx.repoDir)
	if err != nil {
		return err
	}
	// Normalize the filters to the stored form so '--facet Infra'/'--component
	// Gitea' match the lower/slug values persisted on the reference.
	facetQ := strings.ToLower(strings.TrimSpace(facet))
	compQ := domain.NormalizeComponents(splitInforms(component))
	w := cmd.OutOrStdout()
	shown := make([]store.Reference, 0, len(refs))
	for _, r := range refs {
		if !all && r.State != domain.ReferenceCurrent {
			continue
		}
		if facetQ != "" && !slices.Contains(r.Facets, facetQ) {
			continue
		}
		shown = append(shown, r)
	}
	var matched map[string][]string
	if len(compQ) > 0 {
		shown, matched = rankByComponents(shown, func(x store.Reference) (string, []string) { return x.ID, x.Components }, compQ)
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			References []store.Reference   `json:"references"`
			Matched    map[string][]string `json:"matched,omitempty"`
		}{shown, matched})
	}

	for _, r := range shown {
		line := fmt.Sprintf("%s  [%s]  %s", r.ID, r.State, r.Title)
		var tags []string
		if r.External {
			tags = append(tags, "external")
		}
		if len(r.Facets) > 0 {
			tags = append(tags, "facet "+strings.Join(r.Facets, ","))
		}
		if len(r.Components) > 0 {
			tags = append(tags, "comp "+strings.Join(r.Components, ","))
		}
		if len(r.Informs) > 0 {
			tags = append(tags, "informs "+strings.Join(r.Informs, ","))
		}
		if r.URL != "" {
			tags = append(tags, r.URL)
		}
		if a := r.Archive; a != nil && a.Status == "archived" {
			tags = append(tags, "archive "+a.URL+" "+a.CapturedAt.Format(time.DateOnly))
		}
		if m := matched[r.ID]; m != nil {
			tags = append(tags, "matched "+strings.Join(m, ","))
		}
		if len(tags) > 0 {
			line += "  · " + strings.Join(tags, " · ")
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 {
		if all {
			fmt.Fprintln(w, "No references registered. Use 'cerne reference new \"<title>\" --url <url>' to register one.")
		} else {
			fmt.Fprintln(w, "No current references. Use 'cerne reference list --all' to see the obsolete ones.")
		}
	}
	return nil
}

// readReferenceContent reads the source content from --file, or from a piped
// stdin — through the shared guarded reader (F-0157): an open silent pipe or
// an interactive terminal reads as empty instead of blocking.
func readReferenceContent(cmd *cobra.Command, file string) (string, error) {
	return readOptionalBody(cmd, file)
}

// splitInforms parses the comma-separated --informs flag into trimmed ids
// (empties dropped); app.CreateExternalReference validates them.
func splitInforms(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(csv, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func newReferenceLinkCmd(dir *string) *cobra.Command {
	var informs, unlink string
	cmd := &cobra.Command{
		Use:   "link <id>",
		Short: "Add or remove the entities a current reference informs",
		Long: "Adds (--informs) or removes (--unlink) F-/E-/T-/B-/D-/RB-/N-/I- targets of a current reference, without " +
			"duplicating the ones already there. The path source -> idea -> feature: register the source with " +
			"--informs I-…, then 'idea promote' carries the link to the new feature; or link later with " +
			"'cerne reference link R-… --informs F-…'. Prints the final list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(informs) == "" && strings.TrimSpace(unlink) == "" {
				return fmt.Errorf("provide at least --informs or --unlink")
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			r, absent, err := app.LinkReference(ctx.repoDir, ctx.projDir, args[0], splitInforms(informs), splitInforms(unlink))
			if err != nil {
				return err
			}
			if len(absent) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "not present: %s\n", strings.Join(absent, ", "))
			}
			list := "(none)"
			if len(r.Informs) > 0 {
				list = strings.Join(r.Informs, ", ")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s informs: %s\n", r.ID, list)
			return nil
		},
	}
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/E-/T-/B-/D-/RB-/N-/I- ids to add")
	cmd.Flags().StringVar(&unlink, "unlink", "", "comma-separated ids to remove")
	return verbs.MarkChat(cmd)
}

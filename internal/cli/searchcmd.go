package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/project"
)

func newSearchCmd() *cobra.Command {
	var dir string
	var limit int
	var asJSON, perDoc, worksOnly bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search over the whole history (superseded and rejected included)",
		Long:  "Full-text search over the whole history (superseded and rejected included).\nMatching works (W-) are listed after the results; --works lists only them, so it answers \"is someone already on this?\".",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, dir, args[0], limit, asJSON, perDoc, worksOnly)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum number of results (entities; documents with --per-doc)")
	cmd.Flags().BoolVar(&perDoc, "per-doc", false, "one line per document instead of per entity")
	cmd.Flags().BoolVar(&worksOnly, "works", false, "list only the works (W-) that match, up to 50: is someone already on this?")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

// searchHitJSON is the --json shape of a search hit. It mirrors the HTTP API's
// payload (internal/server's searchHit) so an agent gets the same JSON whether
// it queries via the CLI or the server.
type searchHitJSON struct {
	EntityID     string `json:"entity_id"`
	Kind         string `json:"kind"`
	Doc          string `json:"doc"`
	State        string `json:"state"`
	Title        string `json:"title"`
	Snippet      string `json:"snippet"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

// entityHitJSON is the --json shape of an entity line (F-0367): a search hit
// plus the entity's matching documents, always present ([] when none).
type entityHitJSON struct {
	searchHitJSON
	Docs []string `json:"docs"`
}

// maxWorkHits caps the works group (F-0408).
const maxWorkHits = 5

// maxWorksOnly caps the --works listing.
const maxWorksOnly = 50

// workHitJSON is the --json shape of a matching work.
type workHitJSON struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Actor string `json:"actor"`
	Title string `json:"title"`
}

func runSearch(cmd *cobra.Command, dir, query string, limit int, asJSON, perDoc, worksOnly bool) (err error) {
	// Offline by design: search reads the cached clone and the local index,
	// never the remote. 'cerne pull' is the sync point that refreshes both.
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	defer func() { err = unwritableCacheErr(ctx.repoDir, ctx.cfg.Backend, err) }()
	dbPath, err := project.IndexPath(ctx.cfg.ProjectID)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) || (err == nil && !index.HasWorks(dbPath)) {
		// (a missing index, or one built before works were indexed)
		// In --json mode stdout must stay pure JSON, so build the index silently.
		if !asJSON {
			fmt.Fprintln(out, "Building index...")
		}
		if err := index.Rebuild(ctx.repoDir, dbPath, priorSyncedAt(dbPath)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	synced := indexSyncedAt(dbPath, ctx.repoDir)
	if !asJSON {
		noteIndexAge(cmd.ErrOrStderr(), ctx.cfg.Backend, synced)
	}

	if worksOnly {
		works, total, err := index.SearchWorks(dbPath, query, maxWorksOnly)
		if err != nil {
			return err
		}
		if asJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(struct {
				Works         []workHitJSON `json:"works"`
				WorksTotal    int           `json:"works_total"`
				IndexSyncedAt *string       `json:"index_synced_at"`
			}{workHitsJSON(works), total, syncedAtJSON(synced)})
		}
		if len(works) == 0 {
			fmt.Fprintf(out, "No works match \"%s\".\n", query)
			return nil
		}
		printWorks(out, works, total, "")
		return nil
	}

	var results []index.EntityResult
	var broadened bool
	if perDoc {
		var docs []index.Result
		docs, broadened, err = index.SearchBroad(dbPath, query, limit)
		for _, r := range docs {
			results = append(results, index.EntityResult{Result: r})
		}
	} else {
		results, broadened, err = index.SearchEntities(dbPath, query, limit)
	}
	if err != nil {
		return err
	}

	// The works group is a bonus: a failure to read it never fails the search.
	works, worksTotal, _ := index.SearchWorks(dbPath, query, maxWorkHits)

	if asJSON {
		hits := make([]searchHitJSON, len(results))
		entities := make([]entityHitJSON, len(results))
		for i, r := range results {
			hits[i] = searchHitJSON{
				EntityID:     r.EntityID,
				Kind:         r.Kind,
				Doc:          r.Doc,
				State:        r.State,
				Title:        r.Title,
				Snippet:      r.Snippet,
				SupersededBy: r.SupersededBy,
			}
			entities[i] = entityHitJSON{hits[i], r.Docs}
		}
		var payload any = entities
		if perDoc {
			payload = hits
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Results       any           `json:"results"`
			Broadened     bool          `json:"broadened,omitempty"`
			IndexSyncedAt *string       `json:"index_synced_at"`
			Works         []workHitJSON `json:"works"`
			WorksTotal    int           `json:"works_total"`
		}{payload, broadened, syncedAtJSON(synced), workHitsJSON(works), worksTotal})
	}

	if len(results) == 0 {
		fmt.Fprintf(out, "Nothing found for \"%s\".\n", query)
		printWorks(out, works, worksTotal, query)
		return nil
	}
	if broadened && (perDoc || results[0].Partial) {
		fmt.Fprintln(out, "No document has all the terms; showing those that have any.")
	}
	for i, r := range results {
		if r.Partial && i > 0 && !results[i-1].Partial {
			fmt.Fprintln(out, "The lines below have only some of the terms.")
		}
		if perDoc {
			fmt.Fprintln(out, resultHeader(r.Result))
		} else {
			fmt.Fprintln(out, entityHeader(r))
		}
		if r.Snippet != "" {
			fmt.Fprintln(out, "  "+r.Snippet)
		}
	}
	printWorks(out, works, worksTotal, query)
	return nil
}

func workHitsJSON(works []index.WorkResult) []workHitJSON {
	hits := make([]workHitJSON, len(works))
	for i, w := range works {
		hits[i] = workHitJSON{w.ID, w.State, w.Actor, w.Title}
	}
	return hits
}

// printWorks lists the matching works after the entity results: "Is anyone on
// it already?" is a different question from "what do we know". When more match
// than are shown it says so; hintQuery, when set, adds how to list them all.
func printWorks(out io.Writer, works []index.WorkResult, total int, hintQuery string) {
	if len(works) == 0 {
		return
	}
	if hintQuery != "" {
		fmt.Fprintln(out, "\nWorks:")
	}
	for _, w := range works {
		fmt.Fprintf(out, "%s [%s] %s — %s\n", w.ID, w.State, w.Actor, w.Title)
	}
	if more := total - len(works); more > 0 {
		if hintQuery != "" {
			fmt.Fprintf(out, "… %d more works match — cerne search --works \"%s\"\n", more, hintQuery)
		} else {
			fmt.Fprintf(out, "… %d more works match — narrow the query\n", more)
		}
	}
}

// resultHeader renders one --per-doc hit header:
// "F-0007 [feature/spec] superseded → F-0009 — Bills (boletos)". Only
// feature rows show the lifecycle doc suffix; the arrow appears only on
// superseded entities.
func resultHeader(r index.Result) string {
	var b strings.Builder
	b.WriteString(r.EntityID + " [" + r.Kind)
	if r.Kind == "feature" && r.Doc != "" {
		b.WriteString("/" + r.Doc)
	}
	b.WriteString("] " + r.State)
	if r.SupersededBy != "" {
		b.WriteString(" → " + r.SupersededBy)
	}
	b.WriteString(" — " + r.Title)
	return b.String()
}

// entityHeader renders one entity line (F-0367):
// "F-0007 [superseded → F-0009] Bills (boletos) — spec, result". The arrow
// appears only on superseded entities; the matching documents only on
// features, whose docs are the lifecycle documents.
func entityHeader(r index.EntityResult) string {
	var b strings.Builder
	b.WriteString(r.EntityID + " [" + r.State)
	if r.SupersededBy != "" {
		b.WriteString(" → " + r.SupersededBy)
	}
	b.WriteString("] " + r.Title)
	if r.Kind == "feature" && len(r.Docs) > 0 {
		b.WriteString(" — " + strings.Join(r.Docs, ", "))
	}
	return b.String()
}

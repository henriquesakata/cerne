package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/project"
)

func newSearchCmd() *cobra.Command {
	var dir string
	var limit int
	var asJSON, perDoc bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search over the whole history (superseded and rejected included)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, dir, args[0], limit, asJSON, perDoc)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum number of results (entities; documents with --per-doc)")
	cmd.Flags().BoolVar(&perDoc, "per-doc", false, "one line per document instead of per entity")
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

func runSearch(cmd *cobra.Command, dir, query string, limit int, asJSON, perDoc bool) error {
	// Offline by design: search reads the cached clone and the local index,
	// never the remote. 'cerne pull' is the sync point that refreshes both.
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	dbPath, err := project.IndexPath(ctx.cfg.ProjectID)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		// In --json mode stdout must stay pure JSON, so build the index silently.
		if !asJSON {
			fmt.Fprintln(out, "Building index...")
		}
		if err := index.Rebuild(ctx.repoDir, dbPath, time.Time{}); err != nil {
			return err
		}
	} else if err != nil {
		return err
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
			Results   any  `json:"results"`
			Broadened bool `json:"broadened,omitempty"`
		}{payload, broadened})
	}

	if len(results) == 0 {
		fmt.Fprintf(out, "Nothing found for \"%s\".\n", query)
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
	return nil
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

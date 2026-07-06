package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/project"
)

func newSearchCmd() *cobra.Command {
	var dir string
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search over the whole history (superseded and rejected included)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, dir, args[0], limit, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum number of results")
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

func runSearch(cmd *cobra.Command, dir, query string, limit int, asJSON bool) error {
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
		if err := index.Rebuild(ctx.repoDir, dbPath); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	results, err := index.Search(dbPath, query, limit)
	if err != nil {
		return err
	}

	if asJSON {
		hits := make([]searchHitJSON, len(results))
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
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Results []searchHitJSON `json:"results"`
		}{hits})
	}

	if len(results) == 0 {
		fmt.Fprintf(out, "Nothing found for \"%s\".\n", query)
		return nil
	}
	for _, r := range results {
		fmt.Fprintln(out, resultHeader(r))
		if r.Snippet != "" {
			fmt.Fprintln(out, "  "+r.Snippet)
		}
	}
	return nil
}

// resultHeader renders one hit header:
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

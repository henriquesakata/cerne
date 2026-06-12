package cli

import (
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
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search over the whole history (superseded and rejected included)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, dir, args[0], limit)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum number of results")
	return cmd
}

func runSearch(cmd *cobra.Command, dir, query string, limit int) error {
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
		fmt.Fprintln(out, "Construindo índice...")
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
	if len(results) == 0 {
		fmt.Fprintf(out, "Nada encontrado para \"%s\".\n", query)
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

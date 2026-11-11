package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/ask"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
)

// askOpts groups the ask flags that shape retrieval and output, so runAsk keeps
// a short signature as the set grows.
type askOpts struct {
	history    bool
	noPreamble bool
	asJSON     bool
	maxSources int
	maxSnippet int
}

func newAskCmd() *cobra.Command {
	var dir string
	var opts askOpts
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Q&A over current state, diving into history (LLM layer)",
		Long: "Assembles the context (wiki + FTS index), current-first, each source tagged by " +
			"state. In delegation mode (default) it prints the assembled prompt for the host agent " +
			"to answer inline. Use --history to include superseded/obsolete entities (tagged). " +
			"To save tokens: --max-sources N (limits the number of sources), --max-snippet N (truncates each " +
			"snippet), --no-preamble (omits the answer instruction), --json (emits just the sources, structured).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsk(cmd, dir, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&opts.history, "history", false, "include superseded/rejected/obsolete entities (state-tagged)")
	cmd.Flags().IntVar(&opts.maxSources, "max-sources", 0, "limit of sources in the context (0 = all)")
	cmd.Flags().IntVar(&opts.maxSnippet, "max-snippet", 0, "limit of characters per snippet (0 = whole)")
	cmd.Flags().BoolVar(&opts.noPreamble, "no-preamble", false, "omit the answer instruction (saves tokens)")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "emit just the retrieved sources, in JSON")
	return cmd
}

func runAsk(cmd *cobra.Command, dir, query string, opts askOpts) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("the question cannot be empty")
	}
	// Offline by design, like search: reads the cached clone and local index.
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
		// In --json mode stdout must stay pure JSON: build the index silently.
		if !opts.asJSON {
			fmt.Fprintln(out, "Building index...")
		}
		if err := index.Rebuild(ctx.repoDir, dbPath); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	snippets, err := ask.Retrieve(ctx.repoDir, dbPath, query, opts.history, 10)
	if err != nil {
		return err
	}
	snippets = ask.Trim(snippets, opts.maxSources, opts.maxSnippet)

	// --json: emit the retrieved sources structured, with no LLM call — the
	// caller wants the context, not a synthesized answer.
	if opts.asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Sources []ask.Snippet `json:"sources"`
		}{snippets})
	}

	prompt := ask.BuildAskPrompt(query, snippets, !opts.noPreamble)

	// byo-key: call the Provider and print the answer. Delegation (default)
	// prints the assembled prompt for the host agent to answer inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) != llm.ModeDelegation {
		prov, err := llm.NewProvider(ctx.cfg.LLM)
		if err != nil {
			return err
		}
		answer, err := prov.Complete(cmd.Context(), prompt)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, answer)
		return nil
	}
	fmt.Fprint(out, prompt)
	return nil
}

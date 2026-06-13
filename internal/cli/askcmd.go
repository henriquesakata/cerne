package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/ask"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/project"
)

func newAskCmd() *cobra.Command {
	var dir string
	var history bool
	cmd := &cobra.Command{
		Use:   "ask <pergunta>",
		Short: "Q&A over current state, diving into history (LLM layer)",
		Long: "Monta o contexto (wiki + índice FTS), vigente-primeiro, cada fonte marcada por " +
			"estado. No modo delegação (default) imprime o prompt montado para o agente hospedeiro " +
			"responder inline. Use --history para incluir entidades substituídas/obsoletas (marcadas).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsk(cmd, dir, args[0], history)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&history, "history", false, "include superseded/rejected/obsolete entities (state-tagged)")
	return cmd
}

func runAsk(cmd *cobra.Command, dir, query string, history bool) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("a pergunta não pode ser vazia")
	}
	// Offline by design, like search: reads the cached clone and local index.
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	if mode := llm.ResolveMode(ctx.cfg.LLM.Mode); mode != llm.ModeDelegation {
		return fmt.Errorf("modo LLM %q ainda não suportado (apenas 'delegation')", mode)
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
	snippets, err := ask.Retrieve(ctx.repoDir, dbPath, query, history, 10)
	if err != nil {
		return err
	}
	fmt.Fprint(out, ask.BuildAskPrompt(query, snippets))
	return nil
}

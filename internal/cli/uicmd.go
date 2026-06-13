package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/server"
)

func newUICmd() *cobra.Command {
	var dir string
	var port int
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Serve the local web dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(cmd, dir, port)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&port, "port", 7717, "port for the local UI server")
	return cmd
}

func runUI(cmd *cobra.Command, dir string, port int) error {
	// resolveProject pulls first: the dashboard should show fresh state.
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "UI local em http://localhost:%d (Ctrl+C para sair)\n", port)
	return http.ListenAndServe(fmt.Sprintf("localhost:%d", port), server.New(ctx.repoDir, ctx.projDir, ctx.cfg.ProjectID))
}

package cli

import (
	"fmt"
	"net"
	"net/http"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/server"
)

func newUICmd() *cobra.Command {
	var dir string
	var port int
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Serve the local web dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(cmd, dir, port, noBrowser)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&port, "port", 7717, "port for the local UI server")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "do not open the browser automatically")
	return cmd
}

func runUI(cmd *cobra.Command, dir string, port int, noBrowser bool) error {
	// resolveProject pulls first: the dashboard should show fresh state.
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	// Bind before printing/opening so the URL is shown only once the socket is
	// listening (no race with the browser).
	addr := fmt.Sprintf("localhost:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s", addr)
	fmt.Fprintf(cmd.OutOrStdout(), "UI local em %s (Ctrl+C para sair)\n", url)
	if !noBrowser {
		go openBrowser(url)
	}
	return http.Serve(ln, server.New(ctx.repoDir, ctx.projDir, ctx.cfg.ProjectID))
}

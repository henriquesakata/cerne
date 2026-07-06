package cli

import (
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/server"
)

func newServeCmd() *cobra.Command {
	var root string
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve many projects over HTTP for a host (multi-project, S2S)",
		Long: "Starts a multi-project HTTP service: one process serves the docs of N " +
			"projects under /api/projects/{pid}/* (reuses the same endpoints as 'cerne ui'). " +
			"The host registers each project's backend via PUT /api/projects/{pid}. Every route " +
			"requires the S2S shared-secret in CERNE_SERVE_SECRET (without it, the service won't start). " +
			"Per-user/per-project authorization is the host's responsibility (e.g. the Studio BFF).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, root, port)
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "root dir for per-project caches (defaults to CERNE_CACHE_DIR or the user cache dir)")
	cmd.Flags().IntVar(&port, "port", 7700, "port for the multi-project HTTP service")
	return cmd
}

func runServe(cmd *cobra.Command, root string, port int) error {
	secret := os.Getenv("CERNE_SERVE_SECRET")
	if secret == "" {
		return fmt.Errorf("CERNE_SERVE_SECRET not set — set the S2S shared-secret before starting the service")
	}
	// Per-project caches live under root; the path helpers (CacheDir/IndexPath)
	// read CERNE_CACHE_DIR, so point it at --root when given.
	if root != "" {
		if err := os.Setenv("CERNE_CACHE_DIR", root); err != nil {
			return err
		}
	}
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "cerne serve (multi-project) at %s\n", addr)
	return http.Serve(ln, server.NewMultiProject(secret))
}

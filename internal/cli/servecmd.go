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
		Long: "Sobe um serviço HTTP multi-projeto: um processo serve os docs de N " +
			"projetos sob /api/projects/{pid}/* (reusa os mesmos endpoints do 'cerne ui'). " +
			"O host registra o backend de cada projeto via PUT /api/projects/{pid}. Toda rota " +
			"exige o shared-secret S2S em CERNE_SERVE_SECRET (sem ele, o serviço não sobe). " +
			"A autorização por usuário/projeto é responsabilidade do host (ex.: o BFF do Studio).",
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
		return fmt.Errorf("CERNE_SERVE_SECRET não definido — defina o shared-secret S2S antes de subir o serviço")
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
	fmt.Fprintf(cmd.OutOrStdout(), "cerne serve (multi-projeto) em %s\n", addr)
	return http.Serve(ln, server.NewMultiProject(secret))
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/server"
)

func newUICmd() *cobra.Command {
	var dir string
	var port int
	var noBrowser bool
	var pullInterval time.Duration
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Serve the local web dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(cmd, dir, port, noBrowser, pullInterval)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&port, "port", 7717, "port for the local UI server")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "do not open the browser automatically")
	cmd.Flags().DurationVar(&pullInterval, "pull-interval", 15*time.Second,
		"intervalo de sincronização com o backend remoto (0 desliga)")
	return cmd
}

func runUI(cmd *cobra.Command, dir string, port int, noBrowser bool, pullInterval time.Duration) error {
	// resolveProject pulls first: the dashboard should show fresh state.
	proj, err := resolveProject(dir)
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

	// A cancellable context ties the live-refresh goroutines (HEAD watcher +
	// periodic pull) to the process: Ctrl+C cancels it and shuts the server
	// down gracefully, so the goroutines never outlive the listener.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	srv := &http.Server{
		Handler: server.NewLive(sigCtx, proj.repoDir, proj.projDir, proj.cfg.ProjectID, pullInterval),
	}
	go func() {
		<-sigCtx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

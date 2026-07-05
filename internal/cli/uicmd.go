package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/server"
	"gitlab.com/rockt/cerne/internal/store"
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

// runUI binds the port to become the host that serves the dashboard for N
// projects. If the port is already in use by a running cerne ui, it instead
// joins that instance: it registers this project and stays attached, removing
// the project again on Ctrl+C.
func runUI(cmd *cobra.Command, dir string, port int, noBrowser bool, pullInterval time.Duration) error {
	addr := fmt.Sprintf("localhost:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			return joinRunningUI(cmd.OutOrStdout(), dir, port)
		}
		return err
	}

	// A cancellable context ties the per-project live-refresh goroutines to the
	// process; Ctrl+C cancels it and shuts the server down gracefully.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return hostUI(sigCtx, cmd.OutOrStdout(), ln, addr, dir, pullInterval, uiResolver, !noBrowser)
}

// hostUI is host mode: this process owns the port and serves every registered
// project until ctx is done. openTab controls the automatic browser tab.
func hostUI(ctx context.Context, out io.Writer, ln net.Listener, addr, dir string, pullInterval time.Duration, resolve server.Resolver, openTab bool) error {
	handler, err := server.NewUIMulti(ctx, resolve, pullInterval, dir)
	if err != nil {
		ln.Close()
		return err
	}
	url := fmt.Sprintf("http://%s", addr)
	fmt.Fprintf(out, "UI local em %s (Ctrl+C para sair)\n", url)
	if openTab {
		go openBrowser(url)
	}
	srv := &http.Server{Handler: handler}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// uiResolver maps a project directory to the backend locations and identity the
// multi-project server needs. It refreshes the cached clone (resolveProject
// pulls), so a freshly added project shows current state.
func uiResolver(dir string) (repoDir, projDir, projectID, name string, err error) {
	pc, err := resolveProject(dir)
	if err != nil {
		return "", "", "", "", err
	}
	proj, err := store.LoadProject(pc.repoDir)
	if err != nil {
		return "", "", "", "", err
	}
	return pc.repoDir, pc.projDir, pc.cfg.ProjectID, proj.Name, nil
}

// joinRunningUI registers this project with the cerne ui already serving the
// port, then blocks until Ctrl+C, removing the project on the way out. The 2nd
// process owns nothing — it just keeps the project in the panel for its
// lifetime (close the terminal → the project leaves the selector).
func joinRunningUI(out io.Writer, dir string, port int) error {
	base := fmt.Sprintf("http://localhost:%d", port)
	if !probeCerneUI(base) {
		return fmt.Errorf("a porta %d já está em uso por outro processo (não parece um cerne ui)", port)
	}
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	pid, name, err := registerProject(base, absDir)
	if err != nil {
		return fmt.Errorf("não foi possível adicionar o projeto ao cerne ui em %s: %w", base, err)
	}
	fmt.Fprintf(out, "projeto %q adicionado ao cerne ui em %s (Ctrl+C para remover)\n", name, base)

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-sigCtx.Done()

	if err := deregisterProject(base, pid); err != nil {
		fmt.Fprintf(out, "\naviso: não foi possível remover o projeto do painel: %v\n", err)
		return nil
	}
	fmt.Fprintf(out, "\nprojeto %q removido do painel\n", name)
	return nil
}

// errHostDied signals that the hosting cerne ui went away (its SSE stream
// dropped), so the supervisor should race for the port.
var errHostDied = errors.New("o cerne ui host caiu")

// monitorHost blocks attached to the host's SSE stream (/api/stream) and
// returns errHostDied when the connection drops — the OS closes the socket on
// crash, kill or graceful shutdown alike, so no polling is needed. Returns nil
// when ctx is done (user exit). The server-side keepalive keeps the stream
// distinguishable from a dead peer.
func monitorHost(ctx context.Context, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/stream", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return errHostDied
	}
	defer resp.Body.Close()
	buf := make([]byte, 256)
	for {
		if _, err := resp.Body.Read(buf); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errHostDied
		}
	}
}

// isAddrInUse reports whether err is a bind failure because the port is taken.
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	msg := strings.ToLower(err.Error())
	// Windows phrases it differently than the POSIX errno text.
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address")
}

// probeClient bounds the probe: a host that has bound the port but is still
// initializing (NewUIMulti pulls the backend) must not hang the caller.
var probeClient = &http.Client{Timeout: 2 * time.Second}

// probeCerneUI checks that the server on base is a cerne ui (and not some other
// app holding the port) by confirming GET /api/projects returns the expected
// shape.
func probeCerneUI(base string) bool {
	r, err := probeClient.Get(base + "/api/projects")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Projects []struct {
			PID string `json:"pid"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return false
	}
	return body.Projects != nil // a cerne ui always returns a (possibly empty) array
}

// registerProject adds dir to the running cerne ui (POST /api/projects),
// returning the minted project id and name.
func registerProject(base, dir string) (pid, name string, err error) {
	b, _ := json.Marshal(map[string]string{"dir": dir})
	r, err := http.Post(base+"/api/projects", "application/json", bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(r.Body).Decode(&e)
		if e.Error != "" {
			return "", "", errors.New(e.Error)
		}
		return "", "", fmt.Errorf("HTTP %d", r.StatusCode)
	}
	var info struct {
		PID  string `json:"pid"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return "", "", err
	}
	return info.PID, info.Name, nil
}

// deregisterProject removes pid from the running cerne ui (DELETE
// /api/projects/{pid}).
func deregisterProject(base, pid string) error {
	req, err := http.NewRequest(http.MethodDelete, base+"/api/projects/"+url.PathEscape(pid), nil)
	if err != nil {
		return err
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return nil
}

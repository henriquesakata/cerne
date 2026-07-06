package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
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
		"sync interval with the remote backend (0 disables it)")
	return cmd
}

// runUI serves the dashboard for N projects. The process either binds the
// port and hosts, or joins the cerne ui already on it; when the host dies,
// the survivors race to take the port over (F-0119).
func runUI(cmd *cobra.Command, dir string, port int, noBrowser bool, pullInterval time.Duration) error {
	// A cancellable context ties the per-project live-refresh goroutines to the
	// process; Ctrl+C cancels it and shuts the server down gracefully.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return superviseUI(sigCtx, cmd.OutOrStdout(), dir, port, noBrowser, pullInterval, uiResolver)
}

// superviseUI is the per-process takeover loop: try to bind and host; on
// EADDRINUSE join the running host and monitor it; when the host dies, race
// for the port again after a jittered backoff. Every survivor re-registers
// its own project with whichever process wins, so the registry rebuilds
// itself without any persistence — a project whose owning process died stays
// gone, same semantics as today.
func superviseUI(ctx context.Context, out io.Writer, dir string, port int, noBrowser bool, pullInterval time.Duration, resolve server.Resolver) error {
	// A concrete loopback address, never "localhost": the hostname is
	// dual-stack (127.0.0.1 and ::1), so bind and dial could land on
	// different families — and the port-bind mutex only works when every
	// process races for the same address.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	first := true
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			// Host mode ends only with the process (ctx done) or a fatal
			// init/serve error — a host never goes back to being a secondary.
			return hostUI(ctx, out, ln, addr, dir, pullInterval, resolve, first && !noBrowser)
		}
		if !isAddrInUse(err) {
			return err
		}
		res, err := joinUI(ctx, out, dir, port, first)
		if err != nil {
			return err
		}
		switch res {
		case joinExitUser:
			return nil
		case joinHostDied:
			backoff = 250 * time.Millisecond // fresh race: reset the backoff
		case joinRetry:
			backoff = min(backoff*2, 2*time.Second)
		}
		// Jitter spreads simultaneous racers so the port settles faster.
		if !sleepCtx(ctx, backoff+time.Duration(rand.IntN(250))*time.Millisecond) {
			return nil
		}
		first = false
	}
	return nil
}

// sleepCtx waits d unless ctx ends first; reports whether the wait completed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
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
	fmt.Fprintf(out, "Local UI at %s (Ctrl+C to exit)\n", url)
	if openTab {
		go openBrowser(url)
	}
	srv := &http.Server{Handler: handler}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		// The SSE streams end with ctx (multiHub.done), but force-close any
		// straggler so the port frees for the takeover race deterministically.
		_ = srv.Close()
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

// joinResult says how the attached (secondary) mode ended.
type joinResult int

const (
	joinExitUser joinResult = iota // ctx done — deregistered and left
	joinHostDied                   // monitor saw the host go away — race for the port
	joinRetry                      // host unreachable/unregistrable right now — back off, retry
)

// joinUI registers this project with the cerne ui already serving the port and
// stays attached, monitoring the host. The secondary owns nothing — it keeps
// the project in the panel for its lifetime. firstJoin keeps the original UX:
// a foreign process on the port or a failed registration is a hard error; on
// re-joins after a takeover those become joinRetry (the new host may still be
// initializing — or died mid-init, in which case the loop races for the bind
// again instead of waiting forever).
func joinUI(ctx context.Context, out io.Writer, dir string, port int, firstJoin bool) (joinResult, error) {
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !probeCerneUI(base) {
		if firstJoin {
			return 0, fmt.Errorf("port %d is already in use by another process (does not look like a cerne ui)", port)
		}
		return joinRetry, nil
	}
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	pid, name, err := registerProject(base, absDir)
	if err != nil {
		if firstJoin {
			return 0, fmt.Errorf("could not add the project to the cerne ui at %s: %w", base, err)
		}
		return joinRetry, nil
	}
	if firstJoin {
		fmt.Fprintf(out, "project %q added to the cerne ui at %s (Ctrl+C to remove)\n", name, base)
	} else {
		fmt.Fprintf(out, "reconnected: project %q registered with the cerne ui at %s\n", name, base)
	}

	if err := monitorHost(ctx, base); err != nil {
		if !errors.Is(err, errHostDied) {
			return 0, err
		}
		fmt.Fprintf(out, "the cerne ui at %s went down; racing for the port to take over\n", base)
		return joinHostDied, nil
	}

	// ctx done: user exit — remove the project from the panel on the way out.
	if err := deregisterProject(base, pid); err != nil {
		fmt.Fprintf(out, "\nwarning: could not remove the project from the panel: %v\n", err)
		return joinExitUser, nil
	}
	fmt.Fprintf(out, "\nproject %q removed from the panel\n", name)
	return joinExitUser, nil
}

// errHostDied signals that the hosting cerne ui went away (its SSE stream
// dropped), so the supervisor should race for the port.
var errHostDied = errors.New("the cerne ui host went down")

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

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
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/server"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
	"github.com/henriquesakata/cerne/internal/version"
)

// The chat's tool channel (F-0323) derives its verb catalog from this very
// tree, and the claude-cli agent its refusals; server and llm cannot import
// cli, so both are injected here.
func init() {
	server.CommandTree = Root
	llm.AgentDeniedCommands = agentDeniedCommands(Root())
}

// agentDeniedCommands is the claude-cli refusal list derived from the
// lifecycle marks (crit. 4: lifecycle refused in every mode). Only lifecycle:
// local verbs stay allowed in the subscription mode, whose write policy
// (F-0260/F-0267/F-0269) this feature does not change. It walks the whole
// tree, hidden commands included: verbs.From is the visible catalog, and the
// hidden pt-BR twin `substituir` runs like `supersede` (B-0039).
func agentDeniedCommands(c *cobra.Command) []string {
	var rules []string
	for _, sub := range c.Commands() {
		if verbs.IsLifecycle(sub) {
			rules = append(rules, "Bash("+sub.CommandPath()+":*)")
		}
		rules = append(rules, agentDeniedCommands(sub)...)
	}
	return rules
}

func newUICmd() *cobra.Command {
	var dir string
	var port int
	var noBrowser bool
	var pullInterval time.Duration
	var identity bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Serve the local web dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if identity {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(hostInfo{Version: version.Version, UI: server.UIIdentity()})
			}
			return runUI(cmd, dir, port, noBrowser, pullInterval)
		},
	}
	cmd.Flags().BoolVar(&identity, "identity", false, "print this binary's version and UI identity as JSON and exit")
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
//
// A host asked to restart (F-0478) leaves superviseUI with a restartRequest;
// runUI then turns into the binary on disk. A restart that fails keeps this
// version serving, on the same socket where the platform kept it.
func runUI(cmd *cobra.Command, dir string, port int, noBrowser bool, pullInterval time.Duration) error {
	uiCommand = commandPath()
	// The running binary is the file as it was when this process started: a
	// panel built later (a takeover after an upgrade, a failed restart that
	// keeps serving) must still see a newer file on disk as new.
	uiStart = server.StampCommand(uiCommand)
	for {
		err := serveUI(cmd, dir, port, noBrowser, pullInterval)
		var rr *restartRequest
		if !errors.As(err, &rr) {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "restarting the panel into %s\n", rr.path)
		restartErr := restartUI(rr) // returns only when the restart failed
		fmt.Fprintf(cmd.OutOrStdout(), "warning: could not restart into %s: %v; this version keeps serving\n", rr.path, restartErr)
		keepForRetry(rr) // the same socket serves again, when the platform kept it
		os.Setenv(restartedEnv, "1")
	}
}

// serveUI is one life of the panel process, from bind or join to exit.
func serveUI(cmd *cobra.Command, dir string, port int, noBrowser bool, pullInterval time.Duration) error {
	// A cancellable context ties the per-project live-refresh goroutines to the
	// process; Ctrl+C or SIGTERM (kill, an IDE stopping a preview) cancels it,
	// which shuts the server down gracefully or, when joined, removes this
	// project from the host's panel (F-0466).
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The first signal asks for the graceful exit; a start that is not
	// serving yet (a backend lock wait) never sees it, so the second one gets
	// the default action and ends the process (B-0067).
	context.AfterFunc(sigCtx, stop)
	defer initServerTelemetry(sigCtx, dir)()
	return superviseUI(sigCtx, cmd.OutOrStdout(), dir, port, noBrowser, pullInterval, uiResolve)
}

// uiResolve is uiResolver; a variable so the restart test's helper process
// serves fixture directories without a backend.
var uiResolve server.Resolver = uiResolver

// uiCommand is the command that started this process, as an absolute path;
// empty outside runUI, which leaves the panel without restart (F-0478).
// uiStart is its file as it was at process start.
var (
	uiCommand string
	uiStart   server.CommandStamp
)

// The restart hands the next process the panel's socket and tells it that it
// is a restart, so it opens no browser tab (F-0478 criterion 4).
const (
	listenFDEnv  = "CERNE_UI_LISTEN_FD"
	restartedEnv = "CERNE_UI_RESTARTED"
)

// restartRequest ends host mode for a restart into path. file is the socket
// kept open across the restart and fd its descriptor (nil and -1 where the
// platform cannot keep it).
type restartRequest struct {
	file *os.File
	fd   int
	path string
}

func (*restartRequest) Error() string { return "the panel was asked to restart" }

// commandPath resolves the command as it was invoked: a path is made
// absolute, a bare name is looked up in PATH. A Homebrew link stays a link,
// so after an upgrade it reaches the new Cellar — os.Executable may name the
// old one, which `brew cleanup` deletes.
func commandPath() string {
	arg := os.Args[0]
	if filepath.Base(arg) == arg {
		if p, err := exec.LookPath(arg); err == nil {
			arg = p
		}
	}
	if p, err := filepath.Abs(arg); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	p, _ := os.Executable()
	return p
}

// restartEnv is this process's environment without the restart variables,
// for the next process to receive fresh ones.
func restartEnv() []string {
	env := os.Environ()
	return slices.DeleteFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, listenFDEnv+"=") || strings.HasPrefix(kv, restartedEnv+"=")
	})
}

// inheritedListener takes over the socket a restarting panel handed down,
// and clears the variables so no subprocess inherits them. The handed-down
// descriptor is closed: net.FileListener keeps its own close-on-exec copy.
func inheritedListener() (net.Listener, error) {
	v := os.Getenv(listenFDEnv)
	if v == "" {
		return nil, nil
	}
	os.Unsetenv(listenFDEnv)
	fd, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("%s=%q: %w", listenFDEnv, v, err)
	}
	f := os.NewFile(uintptr(fd), "cerne-ui-listener")
	defer f.Close()
	return net.FileListener(f)
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
	if os.Getenv(restartedEnv) != "" {
		os.Unsetenv(restartedEnv)
		noBrowser = true // a restart reuses the tabs already open
	}
	if ln, err := inheritedListener(); err != nil {
		return err
	} else if ln != nil {
		return hostUI(ctx, out, ln, addr, dir, pullInterval, resolve, false)
	}
	first := true
	warned := map[string]bool{} // host identities already warned about, per process
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
		res, err := joinUI(ctx, out, dir, port, first, noBrowser, warned)
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
// A restart asked through the panel (F-0478) ends it with a restartRequest
// holding a copy of the socket, taken before the shutdown closes ln.
func hostUI(ctx context.Context, out io.Writer, ln net.Listener, addr, dir string, pullInterval time.Duration, resolve server.Resolver, openTab bool) error {
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	restartCh := make(chan struct{}, 1)
	var host server.UIHost
	if uiCommand != "" {
		host = server.UIHost{Command: uiCommand, Start: uiStart, Restart: func() {
			select {
			case restartCh <- struct{}{}:
			default:
			}
		}}
	}
	handler, err := server.NewUIMultiHost(srvCtx, resolve, pullInterval, dir, host)
	if err != nil {
		ln.Close()
		return err
	}
	url := fmt.Sprintf("http://%s", addr)
	fmt.Fprintf(out, "Local UI at %s, cerne %s (Ctrl+C to exit)\n", url, version.Short())
	if openTab {
		go openURL(url)
	}
	srv := &http.Server{Handler: handler}
	var rr *restartRequest
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-srvCtx.Done():
		case <-restartCh:
			f, fd, err := dupListener(ln)
			if err != nil {
				fmt.Fprintf(out, "warning: the port cannot be kept across the restart (%v); it is free for a moment\n", err)
			}
			rr = &restartRequest{file: f, fd: fd, path: uiCommand}
		}
		cancel() // ends the SSE streams (multiHub.done) and the watchers
		shutCtx, cancelShut := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShut()
		_ = srv.Shutdown(shutCtx)
		// Force-close any straggler so the port frees for the takeover race
		// deterministically.
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-stopped
	if rr != nil {
		return rr
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
func joinUI(ctx context.Context, out io.Writer, dir string, port int, firstJoin, noBrowser bool, warned map[string]bool) (joinResult, error) {
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	hostVer, ok := probeCerneUI(base)
	if !ok {
		if firstJoin {
			return 0, fmt.Errorf("port %d is already in use by another process (does not look like a cerne ui)", port)
		}
		return joinRetry, nil
	}
	mine := hostInfo{Version: version.Version, UI: server.UIIdentity()}
	act := joinAction(hostVer, mine)
	if act == actRestart { // criterion 7: the panel's binary on disk is this one
		if err := requestPanelRestart(base); err == nil {
			fmt.Fprintf(out, "the cerne ui on %s runs %s; restarting it into the installed %s\n", base, hostVer.Version, mine.Version)
			if v, ok := waitPanelIdentity(ctx, base, mine, 15*time.Second); ok {
				hostVer = v
			}
		}
		if act = joinAction(hostVer, mine); act == actRestart {
			act = actWarn // the restart did not land: say what to do by hand
		}
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
	// The panel serves the UI and server code of the binary that hosts it
	// (N-0022), so a host of another build, or one that reports no identity at
	// all (older than F-0373), shows another UI. Warn first — once per host
	// identity per process — then print and open the link.
	host := hostInfo{Version: "unknown", UI: "unknown"}
	if hostVer.Version != "" {
		host.Version = hostVer.Version
	}
	if hostVer.UI != "" {
		host.UI = hostVer.UI
	}
	myVersion, myUI := mine.Version, mine.UI
	// Bare base opens whichever project the panel already shows; the project's
	// own pid in the link needs a panel that reads it, which only one with a UI
	// identity does (F-0373).
	target := base + "/?project=" + url.QueryEscape(pid)
	oldPanel := hostVer.UI == ""
	if oldPanel {
		target = base
	}
	if key := host.Version + "|" + host.UI; act != actNone && !warned[key] {
		warned[key] = true
		switch act {
		case actInform: // criterion 8: the panel is current; this process is the odd one
			tail := ""
			if myVersion == "dev" {
				tail = "; to see this build's UI, run it with --port <another port>"
			}
			fmt.Fprintf(out, "the cerne ui on %s runs the installed cerne %s (ui %s); this cerne is %s (ui %s) and only keeps the project registered%s\n",
				base, host.Version, host.UI, myVersion, myUI, tail)
		default:
			tail := ""
			if oldPanel {
				tail = " (that panel ignores project links, so this opens its base address)"
			}
			fmt.Fprintf(out, "warning: the cerne ui on %s is version %s (ui %s), this cerne is %s (ui %s); stop that ui (it serves its own embedded UI) and run cerne ui again%s\n",
				base, host.Version, host.UI, myVersion, myUI, tail)
		}
	}
	if firstJoin {
		fmt.Fprintf(out, "project %q added to the cerne ui at %s (Ctrl+C to remove)\n", name, target)
		if !noBrowser {
			openURL(target)
		}
	} else {
		fmt.Fprintf(out, "reconnected: project %q registered with the cerne ui at %s\n", name, target)
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

// joinAct is what a joining cerne ui does about the panel it found (F-0478).
type joinAct int

const (
	actNone    joinAct = iota // the panel is this very build
	actRestart                // the panel's binary on disk is this one: restart the panel into it
	actInform                 // the panel runs its binary on disk; this process is another build
	actWarn                   // anything else, or a panel that does not report its binary on disk
)

// joinAction decides from the panel's running identity and the binary on its
// disk, never from version order: a local build (dev) has no order.
func joinAction(host, mine hostInfo) joinAct {
	if host.Version == mine.Version && host.UI == mine.UI {
		return actNone
	}
	if in := host.Installed; in != nil && in.Version != "" {
		switch {
		case in.Version == mine.Version && in.UI == mine.UI:
			return actRestart
		case in.Version == host.Version && in.UI == host.UI:
			return actInform
		}
	}
	return actWarn
}

// requestPanelRestart asks the panel on base to restart into its binary on
// disk (POST /api/restart, 202).
func requestPanelRestart(base string) error {
	r, err := probeClient.Post(base+"/api/restart", "application/json", nil)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusAccepted {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return nil
}

// waitPanelIdentity polls the panel until it reports want, for up to timeout:
// the restarted process answers once it has re-read its first project.
func waitPanelIdentity(ctx context.Context, base string, want hostInfo, timeout time.Duration) (hostInfo, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if v, ok := probeCerneUI(base); ok && v.Version == want.Version && v.UI == want.UI {
			return v, true
		}
		if !sleepCtx(ctx, 200*time.Millisecond) {
			break
		}
	}
	return hostInfo{}, false
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
// hostInfo is what a cerne ui reports about itself on the probe; both fields
// are empty on a panel older than F-0373. `cerne ui --identity` prints the
// same pair as JSON (F-0478).
type hostInfo struct {
	Version string `json:"version"`
	UI      string `json:"ui"`
	// Installed is the binary the panel's command resolves to on disk
	// (F-0478); nil on an older panel.
	Installed *hostInfo `json:"installed,omitempty"`
}

func probeCerneUI(base string) (info hostInfo, ok bool) {
	r, err := probeClient.Get(base + "/api/projects")
	if err != nil {
		return hostInfo{}, false
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return hostInfo{}, false
	}
	var body struct {
		Projects []struct {
			PID string `json:"pid"`
		} `json:"projects"`
		Version   string    `json:"version"` // absent on a panel built before F-0373
		UI        string    `json:"ui"`
		Installed *hostInfo `json:"installed"` // absent on a panel built before F-0478
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return hostInfo{}, false
	}
	// a cerne ui always returns a (possibly empty) array
	return hostInfo{Version: body.Version, UI: body.UI, Installed: body.Installed}, body.Projects != nil
}

// openURL opens a URL in the browser; a variable so tests never launch one.
var openURL = openBrowser

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

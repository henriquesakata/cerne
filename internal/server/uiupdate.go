package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/selfupdate"
	"github.com/henriquesakata/cerne/internal/version"
)

// The panel's Update button (F-0480) swaps the binary with cerne update's own
// code (F-0476), which lives in the CLI package this one cannot import: the
// CLI injects both functions, like CommandTree.
var (
	// PlanBinary says, without the network, whether exe can be replaced from
	// inside; when it cannot, why and the command the person runs instead.
	PlanBinary func(exe string) (swappable bool, why, fix string)
	// ReplaceBinary brings exe to the latest release the way it was installed,
	// writing progress to w.
	ReplaceBinary func(w io.Writer, exe string) BinaryOutcome
)

// BinaryOutcome is the end of one swap: State is updated, up to date or not
// done; Fix is the command to run when not done; NewExe the executable that
// changed, when one did.
type BinaryOutcome struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
	NewExe string `json:"new_exe,omitempty"`
	// RestartError says why the panel did not restart after a swap that
	// landed: the new binary is installed, the panel still runs the old one.
	RestartError string `json:"restart_error,omitempty"`
}

// updateCheckInterval is how often an open panel asks for the latest
// release, after the check at start (F-0480 criterion 1). Besides cerne
// update, this is the one place cerne asks the network for a version.
var updateCheckInterval = 24 * time.Hour

// updateState is GET /api/update. Current is the binary installed on disk
// (after a swap, the new one even before the restart), so the button goes
// away once there is nothing newer to install.
type updateState struct {
	Current   string         `json:"current"`
	Latest    string         `json:"latest,omitempty"`
	Newer     bool           `json:"newer"`
	Swappable bool           `json:"swappable"`
	Why       string         `json:"why,omitempty"`
	Fix       string         `json:"fix,omitempty"`
	Running   bool           `json:"running"`
	Error     string         `json:"error,omitempty"`
	Last      *BinaryOutcome `json:"last,omitempty"`
}

// updater checks for a newer release and runs the swap for the panel.
type updater struct {
	cmd, projDir string
	watch        *binaryWatch
	announce     func(liveEvent)

	mu      sync.Mutex
	latest  string
	err     string
	running bool
	last    *BinaryOutcome
	skipped bool // the last check found ui.update_check off
	// plan is PlanBinary's answer, read at each check and after a swap, not
	// on every GET: for a script install it writes a probe file in the folder.
	swappable bool
	why, fix  string
}

// installed is the version of the binary on disk: the running one until the
// watch sees another file.
func (u *updater) installed() string {
	if id := u.watch.installed(); id.Version != "" {
		return id.Version
	}
	return version.Version
}

func (u *updater) state() updateState {
	// The setting is read here too, so turning it off hides the button at
	// once; turned back on, the check it skipped runs now, not a day later.
	on := project.UpdateCheck(u.projDir)
	u.mu.Lock()
	if !on {
		u.latest, u.err = "", ""
	} else if u.skipped && !u.running {
		u.skipped = false
		go u.check()
	}
	st := updateState{Latest: u.latest, Error: u.err, Running: u.running, Last: u.last,
		Swappable: u.swappable, Why: u.why, Fix: u.fix}
	u.mu.Unlock()
	st.Current = u.installed()
	st.Newer = st.Latest != "" && selfupdate.Newer(st.Latest, st.Current)
	return st
}

func (u *updater) plan() {
	if PlanBinary == nil {
		return
	}
	ok, why, fix := PlanBinary(u.cmd)
	u.mu.Lock()
	u.swappable, u.why, u.fix = ok, why, fix
	u.mu.Unlock()
}

// check asks for the latest release, unless ui.update_check is off: the
// setting is read on every check, so turning it off takes effect without a
// restart, and a panel with it off never opens a connection for this.
func (u *updater) check() {
	u.mu.Lock()
	busy := u.running // a swap is under way: its end re-reads the plan
	u.mu.Unlock()
	if busy {
		return
	}
	if !project.UpdateCheck(u.projDir) {
		u.mu.Lock()
		had := u.latest != ""
		u.latest, u.err, u.skipped = "", "", true
		u.mu.Unlock()
		if had {
			u.announce(liveEvent{kind: "projects"})
		}
		return
	}
	base := os.Getenv("CERNE_BASE_URL")
	if base == "" {
		base = selfupdate.DefaultBaseURL
	}
	latest, err := selfupdate.Latest(base)
	u.plan()
	u.mu.Lock()
	if err != nil {
		u.latest, u.err = "", err.Error()
	} else {
		u.latest, u.err = latest, ""
	}
	u.mu.Unlock()
	u.announce(liveEvent{kind: "projects"})
}

func (u *updater) run(ctx context.Context) {
	u.check()
	t := time.NewTicker(updateCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.check()
		}
	}
}

var errUpdateRunning = errors.New("an update is already running")

// start runs the swap in the background: its lines go out as "update"
// events, and a swap that lands restarts the panel into the new binary
// (F-0478) with no other click. A failed swap keeps the old executable
// (cerne update's guarantee) and leaves the reason and the command in Last.
func (u *updater) start() (int, error) {
	if ReplaceBinary == nil {
		return http.StatusConflict, errors.New("this panel cannot update itself")
	}
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return http.StatusConflict, errUpdateRunning
	}
	u.running, u.last = true, nil
	u.mu.Unlock()
	u.announce(liveEvent{kind: "projects"})
	go func() {
		lines := &eventLines{announce: u.announce}
		// A script install prints nothing while it downloads and checks: the
		// panel says what it installs, then how it ended (criterion 3).
		st := u.state()
		fmt.Fprintf(lines, "installing %s over %s\n", st.Latest, st.Current)
		out := ReplaceBinary(lines, u.cmd)
		lines.Flush()
		if out.Detail != "" {
			fmt.Fprintln(lines, out.Detail)
		}
		if out.State == "updated" {
			u.watch.check()
			if _, err := u.watch.requestRestart(); err != nil {
				out.RestartError = err.Error()
			}
		}
		u.plan()
		u.mu.Lock()
		u.running, u.last = false, &out
		u.mu.Unlock()
		u.announce(liveEvent{kind: "projects"})
	}()
	return http.StatusAccepted, nil
}

// eventLines turns the swap's output into one "update" event per line: an
// SSE data field holds no line break, and a progress bar redraws itself with
// carriage returns, so both end a line.
type eventLines struct {
	announce func(liveEvent)
	buf      bytes.Buffer
}

func (e *eventLines) Write(p []byte) (int, error) {
	for _, c := range p {
		if c == '\n' || c == '\r' {
			e.Flush()
			continue
		}
		e.buf.WriteByte(c)
	}
	return len(p), nil
}

// Flush sends what is left, a last line with no newline.
func (e *eventLines) Flush() {
	if line := strings.TrimSpace(e.buf.String()); line != "" {
		e.announce(liveEvent{kind: "update", data: line})
	}
	e.buf.Reset()
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/henriquesakata/cerne/internal/version"
)

// UIHost is what the CLI tells the panel about its own process (F-0478): the
// command that started it, as an absolute path (a PATH link stays a link, so a
// Homebrew upgrade shows up as the link's new target), and how to restart into
// the binary that command now resolves to. The zero value turns both off.
type UIHost struct {
	Command string
	Restart func()
}

// Identity is a binary's version and embedded UI, as `cerne ui --identity`
// prints it. Error says why the binary on disk could not be read.
type Identity struct {
	Version string `json:"version"`
	UI      string `json:"ui"`
	Error   string `json:"error,omitempty"`
}

func runningIdentity() Identity { return Identity{Version: version.Version, UI: UIIdentity()} }

// binaryPollInterval is how often the panel looks at its command on disk:
// a stat, plus one run of the binary only when the file changed.
var binaryPollInterval = 30 * time.Second

// fileStamp tells two files apart without reading them: the real path (a
// Homebrew link points at a new Cellar), size and modification time (the
// install script and a local build replace the file in place).
type fileStamp struct {
	path string
	size int64
	mod  time.Time
}

func stampOf(cmd string) (fileStamp, error) {
	real, err := filepath.EvalSymlinks(cmd)
	if err != nil {
		return fileStamp{}, err
	}
	fi, err := os.Stat(real)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{real, fi.Size(), fi.ModTime()}, nil
}

// readIdentity runs `<cmd> ui --identity` in dir. It is also the restart's
// pre-check: a binary that cannot answer it is not restarted into.
func readIdentity(cmd, dir string) (Identity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, cmd, "ui", "--identity")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return Identity{}, fmt.Errorf("%s ui --identity: %w", cmd, err)
	}
	var id Identity
	if err := json.Unmarshal(out, &id); err != nil || id.Version == "" {
		return Identity{}, fmt.Errorf("%s ui --identity printed no identity: %q", cmd, out)
	}
	return id, nil
}

// binaryWatch follows the panel's command on disk. start is the file that
// started the process; seen the last file whose identity was read.
type binaryWatch struct {
	cmd, dir string
	restart  func()

	mu         sync.Mutex
	start      fileStamp
	seen       fileStamp
	disk       Identity
	restarting bool
}

func newBinaryWatch(host UIHost, dir string) *binaryWatch {
	st, _ := stampOf(host.Command) // unreadable now: every later stamp reads as a change
	return &binaryWatch{cmd: host.Command, dir: dir, restart: host.Restart, start: st, seen: st, disk: runningIdentity()}
}

// installed is the identity of the binary the command resolves to on disk.
func (b *binaryWatch) installed() Identity {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.disk
}

// check re-reads the command's file and reports whether what is installed
// changed. A binary that does not answer yet (mid-upgrade) is read again on
// the next tick.
func (b *binaryWatch) check() bool {
	st, err := stampOf(b.cmd)
	if err != nil {
		return false
	}
	b.mu.Lock()
	same, isStart := st == b.seen, st == b.start
	b.mu.Unlock()
	if same {
		return false
	}
	id := runningIdentity()
	if !isStart {
		if id, err = readIdentity(b.cmd, b.dir); err != nil {
			id = Identity{Error: err.Error()}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		b.seen = st
	}
	changed := id != b.disk
	b.disk = id
	return changed
}

func (b *binaryWatch) run(ctx context.Context, every time.Duration, onChange func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if b.check() {
				onChange()
			}
		}
	}
}

var (
	errSameBinary = errors.New("the binary on disk is the one running: nothing to restart into")
	errRestarting = errors.New("the panel is already restarting")
)

// requestRestart checks the command on disk right now (not on the last tick):
// another file than the running one, that answers --identity. Only then the
// host restarts; the caller has already answered the request by then.
func (b *binaryWatch) requestRestart() (int, error) {
	if b == nil || b.restart == nil {
		return http.StatusConflict, errors.New("this panel cannot restart itself")
	}
	st, err := stampOf(b.cmd)
	if err != nil {
		return http.StatusUnprocessableEntity, fmt.Errorf("the cerne on disk is gone: %w", err)
	}
	b.mu.Lock()
	isStart, busy := st == b.start, b.restarting
	b.mu.Unlock()
	if busy {
		return http.StatusConflict, errRestarting
	}
	if isStart {
		return http.StatusConflict, errSameBinary
	}
	id, err := readIdentity(b.cmd, b.dir)
	if err != nil {
		return http.StatusUnprocessableEntity, fmt.Errorf("the cerne on disk does not run: %w", err)
	}
	b.mu.Lock()
	b.seen, b.disk, b.restarting = st, id, true
	b.mu.Unlock()
	return http.StatusAccepted, nil
}

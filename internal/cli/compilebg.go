package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// The background-compile sidecars sit NEXT TO the backend clone (same rule as
// gitback.LockPath): repoDir is <cache>/<id>/repo, so they never land inside a
// worktree and are never swept into a commit.
func compileLockPath(repoDir string) string { return repoDir + ".compile.lock" }
func compileLogPath(repoDir string) string  { return repoDir + ".compile.log" }

// compileStaleAfter bounds ONE page's synthesis, not a whole run: the holder
// touches the lock after every page. A wiki page is a full LLM call and
// routinely takes minutes, so this window is far wider than gitback's — but a
// crashed holder still frees the wiki within it instead of wedging the project.
var compileStaleAfter = 30 * time.Minute

// maxCompileRounds bounds the holder's absorb loop (see compileSynchronous).
const maxCompileRounds = 5

// compileLock is the single-flight guard over the wiki synthesis. It is NOT
// gitback.WithLock: that one blocks and steals after 2 minutes, both wrong for
// an operation measured in minutes whose second caller must not wait at all.
type compileLock struct{ path string }

// tryCompileLock takes the compile lock without blocking. ok=false means
// another compile is already running: the caller must NOT queue behind it —
// the holder re-reads the dirty set before releasing, so it absorbs this
// round's changes too (compileSynchronous).
func tryCompileLock(repoDir string) (*compileLock, bool) {
	path := compileLockPath(repoDir)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintln(f, strconv.Itoa(os.Getpid()))
			_ = f.Close()
			return &compileLock{path: path}, true
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, false
		}
		if !stealStaleCompileLock(path) {
			return nil, false
		}
	}
}

// stealStaleCompileLock removes path when its holder last showed a sign of life
// more than compileStaleAfter ago. Returns true when the caller should retry the
// exclusive create — the lock was removed, or it vanished on its own.
func stealStaleCompileLock(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if time.Since(info.ModTime()) < compileStaleAfter {
		return false
	}
	return os.Remove(path) == nil
}

// touch refreshes the lock's mtime, proving the holder is alive through a run
// longer than compileStaleAfter. Called between pages.
func (l *compileLock) touch() {
	if l == nil {
		return
	}
	now := time.Now()
	_ = os.Chtimes(l.path, now, now)
}

func (l *compileLock) release() {
	if l != nil {
		_ = os.Remove(l.path)
	}
}

// startBackgroundCompile launches `cerne compile` as a detached child and
// returns as soon as it is spawned — the push must not wait on an LLM. The
// child is deliberately never Wait()ed: it outlives this process, reparented
// to init. Its streams go to a log file, never to the parent's stdout/stderr,
// so a harness reading the push's output is not held open by the child's pipe.
func startBackgroundCompile(ctx projCtx) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Under `go test` the executable is the test binary: it ignores "compile
	// --dir X" and would re-run the whole suite, forking without end. The
	// spawn is the one thing a test cannot exercise in-process, so it is
	// skipped there; everything downstream of it (the notice, the lock, the
	// deferred compile) is covered by running `cerne compile` explicitly.
	if strings.HasSuffix(strings.TrimSuffix(exe, ".exe"), ".test") {
		return nil
	}
	logFile, err := os.OpenFile(compileLogPath(ctx.repoDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	c := exec.Command(exe, "compile", "--dir", ctx.projDir)
	c.Dir = ctx.projDir
	c.Stdin = nil // /dev/null
	c.Stdout = logFile
	c.Stderr = logFile
	return c.Start()
}

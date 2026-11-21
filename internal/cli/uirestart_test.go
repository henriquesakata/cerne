package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/version"
)

// uiHelperMain runs this test binary as `cerne` for the restart test: the
// version is the one stored beside the executable (<exe>.version), and
// projects resolve to their own directory, with no backend.
func uiHelperMain() int {
	exe, _ := os.Executable()
	if v, err := os.ReadFile(exe + ".version"); err == nil {
		version.Version = strings.TrimSpace(string(v))
	}
	uiResolve = func(dir string) (string, string, string, string, error) {
		if os.Getenv("CERNE_TEST_UI_STUCK") == "1" {
			time.Sleep(time.Hour) // B-0067: a start that never serves (a backend lock wait)
		}
		if dir == "" {
			dir, _ = os.Getwd()
		}
		return fakeResolver(dir)
	}
	root := Root()
	root.SetArgs(os.Args[1:])
	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

// installUIHelper puts a copy of this test binary at path, reporting ver.
// The mtime is set so a replacement reads as another file even at equal size.
func installUIHelper(t *testing.T, path, ver string, mtime time.Time) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".version", []byte(ver), 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// TestSuperviseRestartedOpensNoTab (F-0478 criterion 4): the process a
// restart starts opens no browser tab, and clears the marker so nothing it
// spawns inherits it.
func TestSuperviseRestartedOpensNoTab(t *testing.T) {
	t.Setenv(restartedEnv, "1")
	opened := make(chan string, 1)
	prev := openURL
	openURL = func(u string) { opened <- u }
	defer func() { openURL = prev }()

	port := freePort(t)
	dirs := superviseDirs(t, "proj-a")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- superviseUI(ctx, io.Discard, dirs[0], port, false, 0, fakeResolver) }()
	waitPIDs(t, fmt.Sprintf("http://127.0.0.1:%d", port), 5*time.Second, "proj-a")
	if v, ok := os.LookupEnv(restartedEnv); ok {
		t.Fatalf("%s still set (%q)", restartedEnv, v)
	}
	cancel()
	waitExit(t, done, "host")
	select {
	case u := <-opened:
		t.Fatalf("a restarted panel opened %s", u)
	default:
	}
}

// TestCommandPath: a bare name is looked up in PATH, a path is made
// absolute — the link as invoked, not its target.
func TestCommandPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "cerne")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	prev := os.Args[0]
	defer func() { os.Args[0] = prev }()

	os.Args[0] = "cerne"
	if got := commandPath(); got != bin {
		t.Fatalf("bare name: %q, want %q", got, bin)
	}
	t.Chdir(dir)
	os.Args[0] = "./cerne"
	if got, _ := filepath.EvalSymlinks(commandPath()); got != mustEval(t, bin) {
		t.Fatalf("relative path: %q, want %q", got, bin)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestRestartEnv: the next process gets fresh restart variables, never this
// process's.
func TestRestartEnv(t *testing.T) {
	t.Setenv(listenFDEnv, "9")
	t.Setenv(restartedEnv, "1")
	t.Setenv("CERNE_KEEP_ME", "x")
	env := restartEnv()
	if slices.ContainsFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, listenFDEnv+"=") || strings.HasPrefix(kv, restartedEnv+"=")
	}) {
		t.Fatalf("restart variables leaked: %v", env)
	}
	if !slices.Contains(env, "CERNE_KEEP_ME=x") {
		t.Fatal("the rest of the environment was dropped")
	}
}

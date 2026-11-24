package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// E-0005 criterion 11: the map (the CONTEXT.md a pull writes), cerne tags, the
// group search (T-0014), the task package and the index a work start --ref
// prints (T-0017) call no model and no network. Each command runs as a fresh
// cerne process (this binary, CERNE_TEST_RUN_CLI) whose every HTTP(S) request
// goes through a proxy that fails the test, with an ssh that fails it too and
// no AI provider configured. The probe's limit: Go never proxies a loopback
// address nor a client with its own Transport, so those would go unseen.
func TestMapAndTagsNeverTouchTheNetwork(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--dir", proj)
	mustCerne(t, "note", "new", "Seed vocabulary", "--component", "gitea,kpack", "--dir", proj)
	mustCerne(t, "decision", "new", "Forge choice", "--component", "gitea", "--dir", proj)
	mustCerne(t, "feature", "new", "Forge probe", "--context", "why", "--component", "gitea", "--dir", proj)
	mustCerne(t, "note", "link", "N-0001", "--informs", "F-0001", "--dir", proj)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("network call through the proxy: %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	bin := t.TempDir()
	sshCalled := filepath.Join(bin, "called")
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\ntouch "+sshCalled+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A fresh HOME and config dir: no user setting may name a provider.
	env := append(os.Environ(), "CERNE_TEST_RUN_CLI=1", "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+t.TempDir(), "CERNE_CONFIG_DIR="+t.TempDir(),
		"NO_PROXY=", "no_proxy=", "ANTHROPIC_API_KEY=", "OPENAI_API_KEY=", "GEMINI_API_KEY=", "OPENROUTER_API_KEY=")
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY"} {
		env = append(env, k+"="+proxy.URL)
	}
	for _, args := range [][]string{
		{"pull"},
		{"tags"},
		{"tags", "--json"},
		{"note", "list", "--component", "gitea,kpack"},
		{"decision", "list", "--component", "gitea,kpack"},
		{"reference", "list", "--component", "gitea,kpack"},
		{"context", "F-0001"},
		{"context", "F-0001", "--json"},
		{"work", "start", "Probe the package", "--ref", "F-0001"},
	} {
		cmd := exec.Command(self, append(args, "--dir", proj)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cerne %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		if strings.Contains(strings.Join(args, " "), "F-0001") && !strings.Contains(string(out), "N-0001") {
			t.Fatalf("cerne %s did not list the note that informs F-0001:\n%s", strings.Join(args, " "), out)
		}
	}
	if _, err := os.Stat(sshCalled); err == nil {
		t.Error("a command called ssh")
	}
	if ctx := mustRead(t, filepath.Join(proj, ".docs", "CONTEXT.md")); !strings.Contains(ctx, "gitea (1)") {
		t.Fatalf("the pull did not write the map:\n%s", ctx)
	}
}

// F-0501 criteria 7 and 8: lint, pull and push make no HTTP(S) request through
// the environment proxy (the backend is a local bare repo, so git traffic is not
// covered); only `reference check` is seen going through it. The reference URL is plain
// http and not loopback, so the proxy sees it (N-0130). Phase 1 is the positive
// control: the proxy answers 404 and the check goes through it and records gone.
// Phase 2: the proxy fails the test on any request while lint, pull and push run.
func TestReferenceGoneLintNeverTouchesTheNetwork(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--dir", proj)
	mustCerne(t, "feature", "new", "Uses a source", "--context", "why", "--component", "gitea", "--dir", proj)
	mustCerne(t, "reference", "new", "A source", "--url", "http://source.example.test/page", "--informs", "F-0001", "--component", "gitea", "--dir", proj)

	var hits atomic.Int32
	var strict atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if strict.Load() {
			t.Errorf("network call through the proxy: %s %s", r.Method, r.URL)
		}
		http.NotFound(w, r)
	}))
	defer proxy.Close()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "CERNE_TEST_RUN_CLI=1", "HOME="+t.TempDir(), "CERNE_CONFIG_DIR="+t.TempDir(), "NO_PROXY=", "no_proxy=")
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY"} {
		env = append(env, k+"="+proxy.URL)
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(self, append(args, "--dir", proj)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run("reference", "check")
	if err != nil || !strings.Contains(out, "R-0001  gone (HTTP 404)  informs F-0001") {
		t.Fatalf("check: %v\n%s", err, out)
	}
	if hits.Load() == 0 {
		t.Fatal("the check never went through the proxy: the probe is blind")
	}

	strict.Store(true)
	out, err = run("lint")
	if err == nil || !strings.Contains(strings.ToLower(out), "reference-gone") || !strings.Contains(out, "F-0001") {
		t.Fatalf("lint should fail on reference-gone naming F-0001 (err %v):\n%s", err, out)
	}
	for _, args := range [][]string{{"pull"}, {"push"}} {
		if out, err := run(args...); err != nil {
			t.Fatalf("cerne %s: %v\n%s", args[0], err, out)
		}
	}
}

// F-0503 crit. 2 and crit. 7: with references.archive off, reference new
// (with and without --no-archive) and reference archive (dry run included)
// make no HTTP(S) request through the environment proxy; with it on, neither a
// reference new --no-archive nor an archive run naming a no-archive reference
// does. Positive control, last: on, the batch takes the one reference without
// the mark and its anonymous proof GET goes through the proxy (404, so not
// public: nothing reaches the archive service), while the no-archive URLs never
// show. The URLs are public IP literals from the documentation range, so no DNS
// lookup is made and Go proxies them (N-0130).
func TestReferenceArchiveNeverTouchesTheNetworkWhenOff(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--dir", proj)

	var strict atomic.Bool
	var mu sync.Mutex
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.Host+" "+r.URL.String())
		mu.Unlock()
		if strict.Load() {
			t.Errorf("network call through the proxy: %s %s", r.Method, r.URL)
		}
		http.NotFound(w, r)
	}))
	defer proxy.Close()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "CERNE_TEST_RUN_CLI=1", "HOME="+t.TempDir(), "CERNE_CONFIG_DIR="+t.TempDir(), "NO_PROXY=", "no_proxy=")
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY"} {
		env = append(env, k+"="+proxy.URL)
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(self, append(args, "--dir", proj)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	must := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("cerne %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}

	strict.Store(true)
	if out := must("reference", "new", "Public page", "--url", "http://203.0.113.10/a"); strings.Contains(out, "archive") {
		t.Errorf("reference new spoke of the archive with the setting off:\n%s", out)
	}
	must("reference", "new", "Kept page", "--url", "http://203.0.113.11/b", "--no-archive")
	for _, args := range [][]string{{"reference", "archive"}, {"reference", "archive", "--dry-run"}} {
		if out, err := run(args...); err == nil || !strings.Contains(out, "references.archive is off") {
			t.Errorf("cerne %s with the setting off: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	rc := filepath.Join(proj, ".cernerc")
	if err := os.WriteFile(rc, []byte(mustRead(t, rc)+"\n[references]\narchive = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must("reference", "new", "Kept too", "--url", "http://203.0.113.12/c", "--no-archive")
	if out := must("reference", "archive", "R-0002", "R-0003"); !strings.Contains(out, "R-0002  skipped (no-archive)") || !strings.Contains(out, "R-0003  skipped (no-archive)") {
		t.Errorf("named no-archive references:\n%s", out)
	}

	strict.Store(false)
	if out := must("reference", "archive"); !strings.Contains(out, "R-0001  not public (HTTP 404)") || strings.Contains(out, "R-0002") || strings.Contains(out, "R-0003") {
		t.Errorf("positive control:\n%s", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 || !strings.Contains(strings.Join(seen, "\n"), "203.0.113.10") {
		t.Fatalf("the proof GET never went through the proxy (the probe is blind): %v", seen)
	}
	for _, s := range seen {
		if strings.Contains(s, "203.0.113.11") || strings.Contains(s, "203.0.113.12") || strings.Contains(s, "archive.org") {
			t.Errorf("the proxy saw %q", s)
		}
	}
}

package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// F-0501 criteria 7 and 8: only `reference check` reaches the network; lint,
// pull and push read what the last check recorded. The reference URL is plain
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

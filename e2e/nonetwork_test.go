package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// E-0005 criterion 11: the map (the CONTEXT.md a pull writes), cerne tags and
// the group search call no model and no network. Each command runs as a fresh
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
	} {
		cmd := exec.Command(self, append(args, "--dir", proj)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cerne %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	if _, err := os.Stat(sshCalled); err == nil {
		t.Error("a command called ssh")
	}
	if ctx := mustRead(t, filepath.Join(proj, ".docs", "CONTEXT.md")); !strings.Contains(ctx, "gitea (1)") {
		t.Fatalf("the pull did not write the map:\n%s", ctx)
	}
}

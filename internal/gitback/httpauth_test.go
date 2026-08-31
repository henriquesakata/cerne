package gitback

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// TestCloneHTTPTokenWiring proves the end-to-end wiring: Clone against an
// http backend sends the env token as BasicAuth, and an auth rejection
// surfaces the CERNE_GIT_TOKEN hint.
func TestCloneHTTPTokenWiring(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	t.Setenv("CERNE_GIT_TOKEN", "tok123")
	t.Setenv("CERNE_GIT_USER", "")
	err := Clone(srv.URL+"/x.git", t.TempDir())
	if err == nil {
		t.Fatal("Clone err = nil, want auth failure")
	}
	if !errors.Is(err, ErrHTTPAuthRequired) {
		t.Fatalf("Clone err = %v, want ErrHTTPAuthRequired in chain", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("cerne:tok123"))
	if gotAuth != want {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, want)
	}

	// Without a token the request goes out anonymous; the rejection still
	// carries the hint so the fix is self-describing.
	gotAuth = "unset"
	t.Setenv("CERNE_GIT_TOKEN", "")
	err = Clone(srv.URL+"/x.git", t.TempDir())
	if !errors.Is(err, ErrHTTPAuthRequired) {
		t.Fatalf("anonymous Clone err = %v, want ErrHTTPAuthRequired in chain", err)
	}
	if gotAuth != "" {
		t.Fatalf("anonymous Authorization header = %q, want empty", gotAuth)
	}
}

func TestWrapAuthHint(t *testing.T) {
	authErr := fmt.Errorf("push: %w", transport.ErrAuthenticationRequired)
	otherErr := errors.New("connection refused")

	cases := []struct {
		name     string
		err      error
		url      string
		wantHint bool
	}{
		{"auth-required on https", authErr, "https://h/x.git", true},
		{"authorization-failed on https", fmt.Errorf("push: %w", transport.ErrAuthorizationFailed), "https://h/x.git", true},
		{"auth-required on ssh", authErr, "git@h:x.git", false},
		{"non-auth error on https", otherErr, "https://h/x.git", false},
		{"nil error", nil, "https://h/x.git", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapAuthHint(c.err, c.url)
			if c.wantHint {
				if !errors.Is(got, ErrHTTPAuthRequired) {
					t.Fatalf("wrapAuthHint = %v, want ErrHTTPAuthRequired in chain", got)
				}
				if !errors.Is(got, transport.ErrAuthenticationRequired) && !errors.Is(got, transport.ErrAuthorizationFailed) {
					t.Fatalf("wrapAuthHint = %v, want original cause kept in chain", got)
				}
				return
			}
			if !errors.Is(got, c.err) || (c.err != nil && errors.Is(got, ErrHTTPAuthRequired)) {
				t.Fatalf("wrapAuthHint(%v, %q) = %v, want error unchanged", c.err, c.url, got)
			}
			if c.err == nil && got != nil {
				t.Fatalf("wrapAuthHint(nil) = %v, want nil", got)
			}
		})
	}
}

func TestIsHTTPURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://git.rockt.com.br/rockt/nutriario-docs.git", true},
		{"http://localhost:3000/x.git", true},
		{"git@cerne.test:rockt/x.git", false},
		{"ssh://git@h/x", false},
		{`C:\local\path`, false},
		{"/tmp/x", false},
		{"./rel", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isHTTPURL(c.url); got != c.want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestAuthForDispatch(t *testing.T) {
	// SSH path: key-file fallback proves the ssh branch is reached.
	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519")
	writeKnownHostsLine(t, filepath.Join(dir, "known_hosts"), "cerne.test")
	t.Setenv("CERNE_SSH_DIR", dir)
	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) {
		return nil, errNoAgentForTest
	}
	t.Setenv("CERNE_GIT_TOKEN", "tok123")

	auth, err := authFor("git@cerne.test:rockt/x.git")
	if err != nil {
		t.Fatalf("authFor(ssh): %v", err)
	}
	if _, ok := auth.(*ssh.PublicKeys); !ok {
		t.Fatalf("authFor(ssh) = %T, want *ssh.PublicKeys", auth)
	}

	auth, err = authFor("https://h/x.git")
	if err != nil {
		t.Fatalf("authFor(https): %v", err)
	}
	if _, ok := auth.(*githttp.BasicAuth); !ok {
		t.Fatalf("authFor(https) = %T, want *githttp.BasicAuth", auth)
	}

	auth, err = authFor("/tmp/x")
	if err != nil {
		t.Fatalf("authFor(local): %v", err)
	}
	if auth != nil {
		t.Fatalf("authFor(local) = %v, want nil", auth)
	}
}

func TestHTTPAuthFor(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		token    string
		user     string
		wantAuth bool
		wantUser string
	}{
		{"https with token", "https://h/x.git", "tok123", "", true, "cerne"},
		{"https with token and custom user", "https://h/x.git", "tok123", "alice", true, "alice"},
		{"http with token", "http://h/x.git", "tok123", "", true, "cerne"},
		{"https without token", "https://h/x.git", "", "", false, ""},
		{"ssh url", "git@h:x.git", "tok123", "", false, ""},
		{"local path", "/tmp/x", "tok123", "", false, ""},
		{"empty url", "", "tok123", "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CERNE_GIT_TOKEN", c.token)
			t.Setenv("CERNE_GIT_USER", c.user)
			auth := httpAuthFor(c.url)
			if !c.wantAuth {
				if auth != nil {
					t.Fatalf("httpAuthFor(%q) = %v, want nil", c.url, auth)
				}
				return
			}
			basic, ok := auth.(*githttp.BasicAuth)
			if !ok {
				t.Fatalf("httpAuthFor(%q) = %T, want *githttp.BasicAuth", c.url, auth)
			}
			if basic.Username != c.wantUser {
				t.Errorf("Username = %q, want %q", basic.Username, c.wantUser)
			}
			if basic.Password != c.token {
				t.Errorf("Password = %q, want %q", basic.Password, c.token)
			}
		})
	}
}

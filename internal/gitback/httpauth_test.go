package gitback

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

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
		{"git@gitlab.com:rockt/x.git", false},
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
	t.Setenv("CERNE_SSH_DIR", dir)
	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) {
		return nil, errNoAgentForTest
	}
	t.Setenv("CERNE_GIT_TOKEN", "tok123")

	auth, err := authFor("git@gitlab.com:rockt/x.git")
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

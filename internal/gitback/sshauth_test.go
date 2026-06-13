package gitback

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

var errNoAgentForTest = errors.New("no agent (test)")

func TestIsSSHURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"git@gitlab.com:rockt/x.git", true},
		{"ssh://git@h/x", true},
		{`C:\local\path`, false},
		{"C:/x", false},
		{"/tmp/x", false},
		{"https://h/x", false},
		{"http://h/x", false},
		{"./rel", false},
	}
	for _, c := range cases {
		if got := isSSHURL(c.url); got != c.want {
			t.Errorf("isSSHURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

// writeKeyFile generates a passphrase-less ed25519 OpenSSH key into dir/name.
func writeKeyFile(t *testing.T, dir, name string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

func TestSSHAuthKeyFileFallback(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519")
	t.Setenv("CERNE_SSH_DIR", dir)
	// Force the agent path to fail so the test exercises the key-file fallback
	// regardless of whether a real agent is reachable on the host.
	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) {
		return nil, errNoAgentForTest
	}

	auth, err := sshAuthFor("git@gitlab.com:rockt/x.git")
	if err != nil {
		t.Fatalf("sshAuthFor: %v", err)
	}
	pk, ok := auth.(*ssh.PublicKeys)
	if !ok {
		t.Fatalf("auth = %T, want *ssh.PublicKeys", auth)
	}
	if pk.User != "git" {
		t.Fatalf("pk.User = %q, want %q", pk.User, "git")
	}

	// User is extracted from the scp-like url prefix before "@".
	auth2, err := sshAuthFor("deploy@h:p")
	if err != nil {
		t.Fatalf("sshAuthFor deploy: %v", err)
	}
	pk2, ok := auth2.(*ssh.PublicKeys)
	if !ok {
		t.Fatalf("auth2 = %T, want *ssh.PublicKeys", auth2)
	}
	if pk2.User != "deploy" {
		t.Fatalf("pk2.User = %q, want %q", pk2.User, "deploy")
	}
}

func TestSSHAuthNoKey(t *testing.T) {
	dir := t.TempDir() // empty: no key files
	t.Setenv("CERNE_SSH_DIR", dir)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) {
		return nil, errNoAgentForTest
	}

	_, err := sshAuthFor("git@gitlab.com:rockt/x.git")
	if err == nil {
		t.Fatal("sshAuthFor err = nil, want error mentioning the probed dir")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("sshAuthFor err = %q, want it to mention probed dir %q", err, dir)
	}
}

func TestSSHAuthNonSSHURL(t *testing.T) {
	for _, url := range []string{"/tmp/x", "https://h/x"} {
		auth, err := sshAuthFor(url)
		if err != nil {
			t.Errorf("sshAuthFor(%q) err = %v, want nil", url, err)
		}
		if auth != nil {
			t.Errorf("sshAuthFor(%q) auth = %v, want nil", url, auth)
		}
	}
}

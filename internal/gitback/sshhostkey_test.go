package gitback

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func TestSSHHost(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"git@gitlab.com:rockt/x.git", "gitlab.com"},
		{"ssh://git@h.example:2222/x", "h.example"},
		{"ssh://git@h.example/x", "h.example"},
		{"deploy@host:p", "host"},
		{"/tmp/x", ""},
		{"https://h/x", ""},
		{`C:\local\path`, ""},
	}
	for _, c := range cases {
		if got := sshHost(c.url); got != c.want {
			t.Errorf("sshHost(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// sshHostPort must keep the port: the known_hosts callback is addressed by
// "host:port", and an entry written as "[host]:2222" can only match if the real
// port survives. In scp-like remotes the segment after ":" is a PATH unless it
// is purely numeric, so both readings are pinned here.
func TestSSHHostPort(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"git@gitlab.com:rockt/x.git", "gitlab.com:22"},
		{"ssh://git@h.example:2222/x", "h.example:2222"},
		{"ssh://git@h.example/x", "h.example:22"},
		{"deploy@host:p", "host:22"},
		{"git@host:2222/x.git", "host:2222"},
		{"/tmp/x", ""},
		{"https://h/x", ""},
		{`C:\local\path`, ""},
	}
	for _, c := range cases {
		if got := sshHostPort(c.url); got != c.want {
			t.Errorf("sshHostPort(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// writeKnownHostsLine builds a known_hosts file at path with a single entry for
// host using a freshly generated ed25519 public key, and returns its algorithm.
func writeKnownHostsLine(t *testing.T, path, host string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	authorized := gossh.MarshalAuthorizedKey(sshPub) // "ssh-ed25519 AAAA...\n"
	line := host + " " + string(authorized)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
}

func TestHostKeyAlgorithmsFromKnownHosts(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	writeKnownHostsLine(t, khPath, "example.test")

	algos, err := hostKeyAlgorithms(khPath, "example.test:22")
	if err != nil {
		t.Fatalf("hostKeyAlgorithms: %v", err)
	}
	if !containsStr(algos, "ssh-ed25519") {
		t.Fatalf("algos = %v, want it to contain ssh-ed25519", algos)
	}

	// Unknown host yields no algorithms.
	unknown, err := hostKeyAlgorithms(khPath, "nope.test:22")
	if err != nil {
		t.Fatalf("hostKeyAlgorithms(unknown): %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("algos for unknown host = %v, want empty", unknown)
	}
}

func TestHostKeyAlgorithmsMissingFile(t *testing.T) {
	algos, err := hostKeyAlgorithms(filepath.Join(t.TempDir(), "does-not-exist"), "example.test:22")
	if err != nil {
		t.Fatalf("hostKeyAlgorithms(missing) err = %v, want nil", err)
	}
	if len(algos) != 0 {
		t.Fatalf("algos = %v, want empty for missing file", algos)
	}
}

func TestSSHAuthFor_SetsCallbackAndAlgos(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519")
	writeKnownHostsLine(t, filepath.Join(dir, "known_hosts"), "gitlab.com")
	t.Setenv("CERNE_SSH_DIR", dir)
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
	if pk.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback is nil, want non-nil from known_hosts")
	}

	cfg, err := pk.ClientConfig()
	if err != nil {
		t.Fatalf("ClientConfig: %v", err)
	}
	if cfg.HostKeyCallback == nil {
		t.Fatal("cfg.HostKeyCallback is nil, want non-nil")
	}
	if len(cfg.HostKeyAlgorithms) == 0 {
		t.Fatal("cfg.HostKeyAlgorithms is empty, want algorithms pinned from known_hosts")
	}
	if !containsStr(cfg.HostKeyAlgorithms, "ssh-ed25519") {
		t.Fatalf("cfg.HostKeyAlgorithms = %v, want it to contain ssh-ed25519", cfg.HostKeyAlgorithms)
	}
}

func containsStr(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

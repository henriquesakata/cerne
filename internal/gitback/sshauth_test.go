package gitback

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

// TestSSHAuthEmptyAgentFallsBackToKeyFile pins the macOS pathology: the launchd
// ssh-agent is reachable (agentDial succeeds) but holds no identities until the
// user runs `ssh-add`. go-git then offers no key and the server rejects the
// handshake with "[none publickey]". sshAuthFor must NOT use such an agent — it
// must fall back to the on-disk key file that plain ssh/git use.
func TestSSHAuthEmptyAgentFallsBackToKeyFile(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519")
	t.Setenv("CERNE_SSH_DIR", dir)

	t.Cleanup(func() { agentHasIdentities = defaultAgentHasIdentities })
	agentHasIdentities = func() bool { return false } // reachable but keyless

	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) {
		t.Fatal("agentDial must not be called when the agent holds no identities")
		return nil, nil
	}

	auth, err := sshAuthFor("git@gitlab.com:rockt/x.git")
	if err != nil {
		t.Fatalf("sshAuthFor: %v", err)
	}
	if _, ok := auth.(*ssh.PublicKeys); !ok {
		t.Fatalf("auth = %T, want *ssh.PublicKeys (key-file fallback)", auth)
	}
}

// TestSSHAuthAgentWithIdentitiesUsed is the complement: when the agent holds at
// least one identity, sshAuthFor uses it (does not fall back to key files). The
// probed dir is empty, so a wrongly-skipped agent would surface as a "no usable
// key" error instead of the agent auth.
func TestSSHAuthAgentWithIdentitiesUsed(t *testing.T) {
	t.Setenv("CERNE_SSH_DIR", t.TempDir()) // no key files

	t.Cleanup(func() { agentHasIdentities = defaultAgentHasIdentities })
	agentHasIdentities = func() bool { return true }

	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(user string) (transport.AuthMethod, error) {
		return &ssh.PublicKeysCallback{User: user}, nil
	}

	auth, err := sshAuthFor("git@gitlab.com:rockt/x.git")
	if err != nil {
		t.Fatalf("sshAuthFor: %v", err)
	}
	if _, ok := auth.(*ssh.PublicKeysCallback); !ok {
		t.Fatalf("auth = %T, want *ssh.PublicKeysCallback (agent used)", auth)
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

// A first key that cannot be read must not veto the ones after it: the probe
// order is id_ed25519, id_rsa, id_ecdsa, and aborting on the first failure means
// a stray unreadable file disables a perfectly good key.
func TestSSHAuthBadFirstKeyFallsThroughToNext(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), []byte("isto nao e uma chave\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	writeKeyFile(t, dir, "id_rsa")
	t.Setenv("CERNE_SSH_DIR", dir)
	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) { return nil, errNoAgentForTest }

	auth, err := sshAuthFor("git@gitlab.com:rockt/x.git")
	if err != nil {
		t.Fatalf("sshAuthFor = %v, want it to fall through to id_rsa", err)
	}
	if auth == nil {
		t.Fatal("auth is nil")
	}
}

// And the diagnosis must match the cause: a corrupt key is not a
// passphrase-protected one, and telling the user to run ssh-add sends them
// after the wrong problem.
func TestSSHAuthCorruptKeyIsNotReportedAsPassphrase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), []byte("isto nao e uma chave\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("CERNE_SSH_DIR", dir)
	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) { return nil, errNoAgentForTest }

	_, err := sshAuthFor("git@gitlab.com:rockt/x.git")
	if err == nil {
		t.Fatal("err is nil, want a failure")
	}
	if strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("err = %v, want it not to blame a passphrase for a corrupt key", err)
	}
}

// When nothing works, the agent's own failure is part of the story: reporting
// only "no usable SSH key" sends the user to the key files when the agent was
// the thing that broke.
func TestSSHAuthReportsAgentFailure(t *testing.T) {
	dir := t.TempDir() // no key files at all
	t.Setenv("CERNE_SSH_DIR", dir)
	t.Cleanup(func() { agentHasIdentities = defaultAgentHasIdentities; agentDial = realAgentDial })
	agentHasIdentities = func() bool { return true }
	agentDial = func(string) (transport.AuthMethod, error) {
		return nil, errors.New("agente recusou a conexao (teste)")
	}

	_, err := sshAuthFor("git@gitlab.com:rockt/x.git")
	if err == nil {
		t.Fatal("err is nil, want a failure")
	}
	if !strings.Contains(err.Error(), "agente recusou a conexao") {
		t.Fatalf("err = %v, want it to carry the agent's own failure", err)
	}
}

// An agent socket that accepts and then goes silent (a stale one inherited from
// a dead session) used to hang the CLI forever: the probe had no deadline
// anywhere. This is the only unbounded wait in the package.
func TestAgentHasIdentitiesDoesNotHangOnDeadAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix-socket probe is not the Windows path")
	}
	// Short base dir: a unix socket path is capped near 104 bytes, and
	// t.TempDir() embeds the test name.
	dir, err := os.MkdirTemp("/tmp", "cerne-agent")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	var held []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held = append(held, c) // accepted, never answered
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", sock)

	done := make(chan bool, 1)
	go func() { done <- agentHasIdentities() }()
	select {
	case <-done: // any answer is fine; not answering is the bug
	case <-time.After(20 * time.Second):
		t.Fatal("agentHasIdentities hung on an agent that never replies")
	}
}

// End-to-end proof that the port survives all the way to the pin. Without it,
// reverting sshHostPort to sshHost would leave every other test green: the unit
// test of the helper would still pass while the thing it exists for silently
// stopped happening.
func TestSSHAuthPinsWithTheRemotePort(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519")
	knownHostsWith(t, filepath.Join(dir, "known_hosts"), "[example.test]:2222")
	t.Setenv("CERNE_SSH_DIR", dir)
	t.Cleanup(func() { agentDial = realAgentDial })
	agentDial = func(string) (transport.AuthMethod, error) { return nil, errNoAgentForTest }

	auth, err := sshAuthFor("ssh://git@example.test:2222/rockt/x.git")
	if err != nil {
		t.Fatalf("sshAuthFor: %v", err)
	}
	cfg, err := auth.(*ssh.PublicKeys).ClientConfig()
	if err != nil {
		t.Fatalf("ClientConfig: %v", err)
	}
	if len(cfg.HostKeyAlgorithms) == 0 {
		t.Fatal("HostKeyAlgorithms is empty: the entry recorded for port 2222 was not found")
	}
}

// Each unusable key must get its OWN reason. Asserting only that a
// passphrase-protected key mentions ssh-add would pass against the old code
// too — it called every failure passphrase-protected. What discriminates is
// that the two causes produce DIFFERENT text.
func TestSSHAuthKeyFailureReasonsAreDistinct(t *testing.T) {
	reasonFor := func(t *testing.T, write func(path string)) string {
		t.Helper()
		dir := t.TempDir()
		write(filepath.Join(dir, "id_ed25519"))
		t.Setenv("CERNE_SSH_DIR", dir)
		t.Cleanup(func() { agentDial = realAgentDial })
		agentDial = func(string) (transport.AuthMethod, error) { return nil, errNoAgentForTest }
		_, err := sshAuthFor("git@gitlab.com:rockt/x.git")
		if err == nil {
			t.Fatal("err is nil, want a failure")
		}
		return err.Error()
	}

	corrupt := reasonFor(t, func(path string) {
		if err := os.WriteFile(path, []byte("isto nao e uma chave\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	locked := reasonFor(t, func(path string) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		block, err := gossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("segredo"))
		if err != nil {
			t.Fatalf("MarshalPrivateKeyWithPassphrase: %v", err)
		}
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	})

	if !strings.Contains(locked, "ssh-add") {
		t.Fatalf("locked key reason = %q, want the ssh-add hint", locked)
	}
	if strings.Contains(corrupt, "ssh-add") || strings.Contains(corrupt, "passphrase") {
		t.Fatalf("corrupt key reason = %q, want it not to blame a passphrase", corrupt)
	}
}

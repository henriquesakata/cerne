package gitback

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

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
		{"git@[::1]:rockt/x.git", "::1"},
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
		{"git@[::1]:rockt/x.git", "[::1]:22"},
		{"ssh://git@[::1]:2222/x", "[::1]:2222"},
		// A sintaxe scp-like não tem porta: em "git@host:2222/x.git" o
		// "2222/x.git" é CAMINHO. Só ssh:// carrega porta.
		{"git@host:2222/x.git", "host:22"},
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

	_, algos, err := knownHostsPin(khPath, "example.test:22")
	if err != nil {
		t.Fatalf("knownHostsPin: %v", err)
	}
	if !containsStr(algos, "ssh-ed25519") {
		t.Fatalf("algos = %v, want it to contain ssh-ed25519", algos)
	}

	// Unknown host yields no algorithms.
	_, unknown, err := knownHostsPin(khPath, "nope.test:22")
	if err != nil {
		t.Fatalf("knownHostsPin(unknown): %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("algos for unknown host = %v, want empty", unknown)
	}
}

func TestHostKeyAlgorithmsMissingFile(t *testing.T) {
	_, algos, err := knownHostsPin(filepath.Join(t.TempDir(), "does-not-exist"), "example.test:22")
	if err != nil {
		t.Fatalf("knownHostsPin(missing) err = %v, want nil", err)
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

// knownHostsWith writes a known_hosts at path whose single entry uses pattern
// verbatim as the host field (plaintext, hashed, "[host]:port" or wildcard) and
// returns the algorithm of the key recorded there.
func knownHostsWith(t *testing.T, path, pattern string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	if err := os.WriteFile(path, []byte(knownhosts.Line([]string{pattern}, key)+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return key.Type()
}

// The three shapes the hand-rolled matcher never handled. Each one is a real
// known_hosts configuration: hashed is the Debian/Ubuntu default, "[host]:port"
// is what ssh writes for a non-standard port, and a wildcard is ordinary.
func TestHostKeyAlgorithmsRealKnownHostsShapes(t *testing.T) {
	cases := []struct {
		name    string
		pattern func(host string) string
		addr    string
		host    string
	}{
		{"texto claro", func(h string) string { return h }, "example.test:22", "example.test"},
		{"hasheado", knownhosts.HashHostname, "example.test:22", "example.test"},
		{"porta nao padrao", func(h string) string { return "[" + h + "]:2222" }, "example.test:2222", "example.test"},
		{"wildcard", func(string) string { return "*.example.test" }, "git.example.test:22", "git.example.test"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kh := filepath.Join(t.TempDir(), "known_hosts")
			want := knownHostsWith(t, kh, c.pattern(c.host))
			before, err := os.ReadFile(kh)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			_, algos, err := knownHostsPin(kh, c.addr)
			if err != nil {
				t.Fatalf("knownHostsPin: %v", err)
			}
			if !containsStr(algos, want) {
				t.Fatalf("algos = %v, want it to contain %q", algos, want)
			}
			// The lookup must not rewrite the user's known_hosts.
			after, err := os.ReadFile(kh)
			if err != nil {
				t.Fatalf("read after: %v", err)
			}
			if string(before) != string(after) {
				t.Fatalf("known_hosts was rewritten by the lookup")
			}
		})
	}
}

// The other half of the invariant: an unknown host yields NOTHING, so "empty"
// can never be mistaken for success. Without this, a lookup that silently stops
// working looks exactly like a host nobody trusts yet.
func TestHostKeyAlgorithmsUnknownHostStaysEmpty(t *testing.T) {
	kh := filepath.Join(t.TempDir(), "known_hosts")
	knownHostsWith(t, kh, "example.test")
	_, algos, err := knownHostsPin(kh, "nope.test:22")
	if err != nil {
		t.Fatalf("knownHostsPin: %v", err)
	}
	if len(algos) != 0 {
		t.Fatalf("algos for unknown host = %v, want empty", algos)
	}
}

// An RSA host key is recorded as "ssh-rsa", which names SHA-1 signatures that
// OpenSSH >= 8.8 refuses by default. Pinning only that would swap the "key
// mismatch" this feature fixes for a fresh handshake failure against every
// current server, so the SHA-2 names for the same key go in too.
func TestKnownHostsPinExpandsRSAToSHA2(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pub, err := gossh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	kh := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(kh, []byte(knownhosts.Line([]string{"example.test"}, pub)+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, algos, err := knownHostsPin(kh, "example.test:22")
	if err != nil {
		t.Fatalf("knownHostsPin: %v", err)
	}
	for _, want := range []string{gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSA} {
		if !containsStr(algos, want) {
			t.Fatalf("algos = %v, want it to contain %q", algos, want)
		}
	}
	// Strongest first, and deterministic: ke.Want comes out of a map.
	if algos[0] != gossh.KeyAlgoRSASHA512 {
		t.Fatalf("algos[0] = %q, want the SHA-512 variant offered first", algos[0])
	}
}

// x/crypto rejects the WHOLE known_hosts on one malformed line, where ssh skips
// that line and carries on. Returning nil here would NOT soften that — go-git
// re-reads the same file through the same parser — so the contract is a clear,
// actionable error instead of a fallback that only pretends to be one.
func TestKnownHostsPinSurvivesACorruptLine(t *testing.T) {
	kh := filepath.Join(t.TempDir(), "known_hosts")
	good := knownHostsWith(t, kh, "example.test") // writes a valid entry
	body, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(kh, append([]byte("isto nao e uma entrada valida\n"), body...), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err = knownHostsPin(kh, "example.test:22")
	if err == nil {
		t.Fatalf("knownHostsPin on a dirty file = nil, want an error naming the file")
	}
	// The message has to be actionable: go-git re-reads the same file through
	// the same parser, so there is no silent recovery to offer — only a clear
	// account of what to fix.
	if !strings.Contains(err.Error(), kh) {
		t.Fatalf("err = %v, want it to name %q", err, kh)
	}
	_ = good
}

// A "@cert-authority" host records the CA by its PLAIN key type, never by a
// "-cert-v01@openssh.com" name — so pinning only the plain types would exclude
// exactly what a certificate-presenting server negotiates, breaking hosts that
// worked before any pin existed. Without certAlgoFor this test fails.
func TestKnownHostsPinOffersCertificateVariants(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	ca, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	kh := filepath.Join(t.TempDir(), "known_hosts")
	line := "@cert-authority example.test " + string(gossh.MarshalAuthorizedKey(ca))
	if err := os.WriteFile(kh, []byte(line), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, algos, err := knownHostsPin(kh, "example.test:22")
	if err != nil {
		t.Fatalf("knownHostsPin: %v", err)
	}
	if !containsStr(algos, gossh.CertAlgoED25519v01) {
		t.Fatalf("algos = %v, want it to contain %q for a @cert-authority host",
			algos, gossh.CertAlgoED25519v01)
	}
}

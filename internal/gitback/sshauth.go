package gitback

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sshKeyFiles are the private key filenames probed under the .ssh dir, in order.
var sshKeyFiles = []string{"id_ed25519", "id_rsa", "id_ecdsa"}

// scpLikeURL matches scp-like SSH remotes such as "git@host:path".
var scpLikeURL = regexp.MustCompile(`^[a-zA-Z0-9_.-]+@[^/]+:`)

// agentDial returns an SSH-agent-backed AuthMethod for user. It is a package
// var so tests can stub the agent path deterministically; production uses
// realAgentDial.
var agentDial = realAgentDial

func realAgentDial(user string) (transport.AuthMethod, error) {
	return ssh.NewSSHAgentAuth(user)
}

// agentHasIdentities reports whether the SSH agent is reachable AND holds at
// least one identity. It is a package var so tests can stub it; production uses
// defaultAgentHasIdentities.
//
// The check exists because ssh.NewSSHAgentAuth succeeds whenever the agent
// socket merely dials — even with zero keys loaded — so an empty agent would
// otherwise shadow the key-file fallback and make go-git fail the handshake with
// "[none publickey]". This is the macOS default: the launchd ssh-agent is always
// reachable (SSH_AUTH_SOCK always set) but stays keyless until the first
// `ssh-add`, where plain ssh/git still work by reading the key file directly.
var agentHasIdentities = defaultAgentHasIdentities

func defaultAgentHasIdentities() bool {
	// On Windows the agent is Pageant / the OpenSSH named pipe, not a unix
	// socket we can List() this way; agentDial errors cleanly if it is absent,
	// so preserve the historical "try the agent" behavior there.
	if runtime.GOOS == "windows" {
		return true
	}
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return true // cannot probe: let agentDial decide (unchanged behavior)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false // agent unreachable → fall back to key files
	}
	defer conn.Close()
	keys, err := agent.NewClient(conn).List()
	if err != nil {
		return false
	}
	return len(keys) > 0
}

// isSSHURL reports whether url is an SSH remote (ssh:// or scp-like git@host:).
// Local paths (including Windows drive letters like C:\x), http(s), and
// relative paths are not SSH.
func isSSHURL(u string) bool {
	if strings.HasPrefix(u, "ssh://") {
		return true
	}
	return scpLikeURL.MatchString(u)
}

// sshUser extracts the SSH user from url, defaulting to "git".
func sshUser(u string) string {
	if strings.HasPrefix(u, "ssh://") {
		if parsed, err := url.Parse(u); err == nil && parsed.User != nil {
			if name := parsed.User.Username(); name != "" {
				return name
			}
		}
		return "git"
	}
	if i := strings.Index(u, "@"); i > 0 {
		return u[:i]
	}
	return "git"
}

// sshHost extracts the bare host (no port) from an SSH remote:
//   - scp-like "user@host:path" → the segment between "@" and the first ":"
//   - "ssh://user@host[:port]/path" → url.Hostname()
//
// Returns "" for non-SSH urls.
func sshHost(u string) string {
	if !isSSHURL(u) {
		return ""
	}
	if strings.HasPrefix(u, "ssh://") {
		if parsed, err := url.Parse(u); err == nil {
			return parsed.Hostname()
		}
		return ""
	}
	at := strings.Index(u, "@")
	if at < 0 {
		return ""
	}
	rest := u[at+1:]
	if colon := strings.Index(rest, ":"); colon >= 0 {
		return rest[:colon]
	}
	return rest
}

// sshHostPort is sshHost plus the port, in the "host:port" form the known_hosts
// callback is addressed by. Defaults to :22. Returns "" for non-SSH urls.
//
// It exists because sshHost deliberately drops the port, and without it an
// entry recorded as "[host]:2222" can never be matched: the callback would be
// asked about the wrong address. In scp-like remotes ("user@host:something")
// the segment after the colon is a PATH, so it counts as a port only when it is
// purely numeric — "git@host:rockt/x.git" is host gitlab-style, not port
// "rockt/x.git".
func sshHostPort(u string) string {
	host := sshHost(u)
	if host == "" {
		return ""
	}
	port := "22"
	if strings.HasPrefix(u, "ssh://") {
		if parsed, err := url.Parse(u); err == nil && parsed.Port() != "" {
			port = parsed.Port()
		}
		return net.JoinHostPort(host, port)
	}
	if at := strings.Index(u, "@"); at >= 0 {
		rest := u[at+1:]
		if colon := strings.Index(rest, ":"); colon >= 0 {
			if tail := rest[colon+1:]; isNumericPort(tail) {
				port = tail
			} else if slash := strings.Index(tail, "/"); slash > 0 && isNumericPort(tail[:slash]) {
				// "host:2222/path" — the scp-like spelling of an explicit port.
				port = tail[:slash]
			}
		}
	}
	return net.JoinHostPort(host, port)
}

func isNumericPort(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// hostKeyAlgorithms returns the host key algorithms (e.g. "ssh-ed25519",
// "ssh-rsa") recorded in the known_hosts file at path for addr ("host:port").
// It is used to pin ssh.ClientConfig.HostKeyAlgorithms so the server presents a
// host key type we already trust — avoiding the knownhosts "key mismatch" error
// that occurs when go-git leaves the algorithm list unconstrained and the server
// offers a key type (e.g. RSA) different from the one recorded (e.g. ed25519).
//
// The lookup is delegated to x/crypto/ssh/knownhosts rather than parsing the
// file here. That library already understands every shape the file really comes
// in — hashed entries (the Debian/Ubuntu default), the "[host]:port" spelling
// ssh writes for a non-standard port, and wildcards — and a hand-rolled matcher
// silently handled NONE of them: it compared a hashed entry against a freshly
// salted hash of the host, which can never be equal, so the pin degraded to a
// no-op exactly where the bug it fixes is most likely.
//
// The mechanism is the library's own: asking the callback about a throwaway key
// yields a *knownhosts.KeyError whose Want lists the keys known for that host.
// An unknown host yields an empty Want, so "no algorithms" keeps meaning
// "nothing recorded" and never "the lookup quietly stopped working".
//
// A missing file is not an error (returns nil): we don't break
// first-connection-to-unknown-host; go-git's default callback still applies.
func hostKeyAlgorithms(path, addr string) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	probe, err := throwawayHostKey()
	if err != nil {
		return nil, err
	}
	var ke *knownhosts.KeyError
	if !errors.As(cb(addr, probeAddr{}, probe), &ke) {
		return nil, nil // no KeyError at all: nothing recorded to pin
	}
	var algos []string
	seen := map[string]bool{}
	for _, want := range ke.Want {
		if alg := want.Key.Type(); alg != "" && !seen[alg] {
			seen[alg] = true
			algos = append(algos, alg)
		}
	}
	return algos, nil
}

// throwawayHostKey is the key handed to the known_hosts callback purely to be
// rejected, so the resulting KeyError carries what the file DOES know. Generated
// once per process: it is never trusted, transmitted or stored.
var throwawayHostKey = sync.OnceValues(func() (gossh.PublicKey, error) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return gossh.NewPublicKey(pub)
})

// probeAddr stands in for the remote address the callback logs; the lookup never
// connects to anything.
type probeAddr struct{}

func (probeAddr) Network() string { return "tcp" }
func (probeAddr) String() string  { return "0.0.0.0:0" }

// sshDir returns the directory probed for key files: $CERNE_SSH_DIR when set,
// otherwise the user's ~/.ssh.
func sshDir() (string, error) {
	if dir := os.Getenv("CERNE_SSH_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh"), nil
}

// sshAuthFor returns the AuthMethod for url: nil for non-SSH urls (local paths,
// http(s)); for ssh (git@host:..., ssh://...) it tries, in order:
//  1. SSH agent (Pageant / Windows OpenSSH agent / SSH_AUTH_SOCK) — only if it
//     holds at least one identity (see agentHasIdentities)
//  2. key files under the user's .ssh dir: id_ed25519, id_rsa, id_ecdsa (first
//     that exists), empty passphrase
//
// Both auth paths get their host-key verification pinned to the algorithms
// recorded in known_hosts (see pinKnownHosts), avoiding the go-git/x-crypto
// "knownhosts: key mismatch" gotcha.
//
// Returns a descriptive pt-BR error if the url is SSH and no method works.
func sshAuthFor(u string) (transport.AuthMethod, error) {
	if !isSSHURL(u) {
		return nil, nil
	}
	user := sshUser(u)
	addr := sshHostPort(u)

	// 1. SSH agent — but only when it actually holds an identity. A reachable
	//    yet keyless agent (the macOS launchd default until `ssh-add`) must not
	//    shadow the key-file fallback: go-git would offer no key and the server
	//    would reject the handshake with "[none publickey]". On any dial error we
	//    also fall through to key files.
	if agentHasIdentities() {
		if auth, err := agentDial(user); err == nil {
			if err := pinKnownHosts(auth, addr); err != nil {
				return nil, err
			}
			return auth, nil
		}
	}

	// 2. Key files under the .ssh dir.
	dir, err := sshDir()
	if err != nil {
		return nil, fmt.Errorf("could not locate the .ssh directory: %w", err)
	}
	for _, name := range sshKeyFiles {
		path := filepath.Join(dir, name)
		if _, statErr := os.Stat(path); statErr != nil {
			continue
		}
		auth, keyErr := ssh.NewPublicKeysFromFile(user, path, "")
		if keyErr != nil {
			return nil, fmt.Errorf(
				"key %s is passphrase-protected — start ssh-agent and run 'ssh-add': %w",
				path, keyErr)
		}
		if err := pinKnownHosts(auth, addr); err != nil {
			return nil, err
		}
		return auth, nil
	}

	return nil, fmt.Errorf(
		"no usable SSH key in %s (looked for id_ed25519, id_rsa, id_ecdsa) — "+
			"configure a key or start ssh-agent", dir)
}

// pinKnownHosts configures host-key verification on auth using the user's
// known_hosts file. It sets HostKeyCallback (so go-git verifies against
// known_hosts) AND pins HostKeyAlgorithms to the key types recorded there, so
// the server presents a key type we already trust — the fix for the
// "knownhosts: key mismatch" handshake failure. Both *ssh.PublicKeys and
// *ssh.PublicKeysCallback embed HostKeyCallbackHelper, whose fields ClientConfig
// reads, so we set them directly.
//
// If known_hosts is absent, auth is left untouched (go-git default behavior;
// don't break first-connection-to-unknown-host).
func pinKnownHosts(auth transport.AuthMethod, addr string) error {
	if addr == "" {
		return nil
	}
	helper := hostKeyHelperOf(auth)
	if helper == nil {
		return nil
	}

	dir, err := sshDir()
	if err != nil {
		return fmt.Errorf("could not locate the .ssh directory: %w", err)
	}
	khPath := filepath.Join(dir, "known_hosts")
	if _, statErr := os.Stat(khPath); statErr != nil {
		return nil // no known_hosts: leave go-git defaults in place
	}

	cb, err := knownhosts.New(khPath)
	if err != nil {
		return fmt.Errorf("could not read known_hosts (%s): %w", khPath, err)
	}
	algos, err := hostKeyAlgorithms(khPath, addr)
	if err != nil {
		return fmt.Errorf("could not read known_hosts (%s): %w", khPath, err)
	}

	helper.HostKeyCallback = cb
	helper.HostKeyAlgorithms = algos
	return nil
}

// hostKeyHelperOf returns the embedded *HostKeyCallbackHelper of the go-git ssh
// auth methods that carry one, or nil for other auth types.
func hostKeyHelperOf(auth transport.AuthMethod) *ssh.HostKeyCallbackHelper {
	switch a := auth.(type) {
	case *ssh.PublicKeys:
		return &a.HostKeyCallbackHelper
	case *ssh.PublicKeysCallback:
		return &a.HostKeyCallbackHelper
	default:
		return nil
	}
}

package gitback

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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

// agentProbeTimeout bounds both the dial and the reply of the agent probe.
const agentProbeTimeout = 3 * time.Second

func defaultAgentHasIdentities() bool {
	// On Windows the agent is Pageant / the OpenSSH named pipe, not a unix
	// socket we can List() this way; agentDial errors cleanly if it is absent,
	// so preserve the historical "try the agent" behavior there — including its
	// lack of a deadline (see the note in the unix path below).
	if runtime.GOOS == "windows" {
		return true
	}
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return true // cannot probe: let agentDial decide (unchanged behavior)
	}
	// Bounded on both halves. A stale socket inherited from a dead session
	// accepts the connection and then never answers, and this probe runs on
	// every SSH operation. A local agent answers in microseconds, so the budget
	// is generous by orders of magnitude.
	//
	// Known limit, and the reason this is not "the last unbounded wait": the two
	// early returns above skip the probe entirely (Windows, and no
	// SSH_AUTH_SOCK), and the ssh.NewSSHAgentAuth that then runs carries no
	// deadline of its own. A wedged Pageant or named pipe can still hang there.
	// Bounding it needs a wrapper around go-git's constructor — a separate
	// change, not a silent claim of coverage here.
	conn, err := net.DialTimeout("unix", sock, agentProbeTimeout)
	if err != nil {
		return false // agent unreachable → fall back to key files
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(agentProbeTimeout)); err != nil {
		return false
	}
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
	// A bracketed IPv6 literal carries colons of its own: the first one is
	// INSIDE the brackets, so splitting on it yields "[" and a dead lookup.
	if strings.HasPrefix(rest, "[") {
		if end := strings.Index(rest, "]"); end > 0 {
			return rest[1:end]
		}
		return ""
	}
	if colon := strings.Index(rest, ":"); colon >= 0 {
		return rest[:colon]
	}
	return rest
}

// sshHostPort is sshHost plus the port, in the "host:port" form the known_hosts
// callback is addressed by. Returns "" for non-SSH urls.
//
// It exists because sshHost deliberately drops the port, and without it an entry
// recorded as "[host]:2222" can never be matched: the callback would be asked
// about the wrong address.
//
// Only ssh:// carries a port. The scp-like spelling has NONE — git reads
// everything after the colon as a path, so "git@host:2222/x.git" is the path
// "2222/x.git" on port 22, not port 2222. Guessing otherwise would address the
// callback differently from the tool this package exists to speak for.
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
	}
	return net.JoinHostPort(host, port)
}

// knownHostsPin returns both halves of host-key verification for addr
// ("host:port"): the callback go-git checks against, and the host key
// algorithms to pin so the server presents a key type we already trust — the
// fix for the "knownhosts: key mismatch" handshake failure.
//
// Both come from go-git's own known_hosts database. That matters twice over.
//
// First, this package OVERRIDES go-git's callback, so it must consult exactly
// the files go-git would have: SSH_KNOWN_HOSTS when set, otherwise the user's
// file plus /etc/ssh/ssh_known_hosts. NewKnownHostsDb applies those rules, so
// they cannot drift apart.
//
// Second, and this is the part earlier versions of this function got wrong
// three times over: deriving the algorithm list is NOT simple, and
// skeema/knownhosts — which go-git already depends on — has done it correctly
// all along. It expands an "ssh-rsa" entry to rsa-sha2-512/256 (the recorded
// name denotes SHA-1 signatures, refused by OpenSSH >= 8.8), and it converts a
// key to its "-cert-v01@openssh.com" name ONLY for entries that came from an
// @cert-authority line. Hand-rolling that expansion here could not distinguish
// a CA entry from a plain one, so it either broke certificate hosts or broke
// plain ones depending on the ordering — a coin flip between two regressions.
// The library knows which line the key came from; this package never did.
func knownHostsPin(addr string) (gossh.HostKeyCallback, []string, error) {
	// CERNE_SSH_DIR exists to isolate the SSH material cerne uses, so it has to
	// cover known_hosts too — isolating the keys while silently verifying hosts
	// against the real ~/.ssh/known_hosts is a half-measure an operator cannot
	// see. But it only REPLACES the user's default file: SSH_KNOWN_HOSTS still
	// wins (it is the specific, standard override, and the error text from the
	// library tells people to set it), and the system-wide file stays in the
	// list, because a corporate image that trusts hosts only through
	// /etc/ssh/ssh_known_hosts must keep working.
	var files []string
	if dir := os.Getenv("CERNE_SSH_DIR"); dir != "" && os.Getenv("SSH_KNOWN_HOSTS") == "" {
		files = append(files, filepath.Join(dir, "known_hosts"), systemKnownHosts)
	}
	db, err := ssh.NewKnownHostsDb(files...)
	if err != nil {
		// go-git fails the same way from its own fallback, so there is no
		// softer behavior to offer — only a message that names the cause.
		return nil, nil, fmt.Errorf("could not read the known_hosts files: %w", err)
	}
	return db.HostKeyCallback(), db.HostKeyAlgorithms(addr), nil
}

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
//  2. key files under the user's .ssh dir: id_ed25519, id_rsa, id_ecdsa, in
//     that order and with an empty passphrase — a file that fails to load is
//     reported and SKIPPED, never a veto on the ones after it
//
// Both auth paths get their host-key verification pinned to the algorithms
// recorded in known_hosts (see pinKnownHosts), avoiding the go-git/x-crypto
// "knownhosts: key mismatch" gotcha.
//
// When the url is SSH and nothing works, the error names everything that was
// tried — each key file with its own cause, plus the agent's failure when there
// was one. Errors here are English, like the rest of the CLI surface (F-0124);
// this comment used to promise pt-BR, which the code never emitted.
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
	// Both agent outcomes are recorded, because the DOMINANT failure is the probe
	// saying no — an unreachable or keyless agent — and that path never reaches
	// agentDial. Leaving agentErr nil there would make the final message blame
	// the key files alone, which is what this used to do.
	var agentErr error
	if agentHasIdentities() {
		auth, err := agentDial(user)
		if err == nil {
			if err := pinKnownHosts(auth, addr); err != nil {
				return nil, err
			}
			return auth, nil
		}
		agentErr = err
	} else {
		agentErr = errors.New("the SSH agent was unreachable, empty, or did not answer in time")
	}

	// 2. Key files under the .ssh dir.
	dir, err := sshDir()
	if err != nil {
		return nil, fmt.Errorf("could not locate the .ssh directory: %w", err)
	}
	var keyErrs []string
	for _, name := range sshKeyFiles {
		path := filepath.Join(dir, name)
		if _, statErr := os.Stat(path); statErr != nil {
			continue
		}
		auth, keyErr := ssh.NewPublicKeysFromFile(user, path, "")
		if keyErr != nil {
			// Do NOT stop here: the probe order is a preference, not a
			// commitment. One unreadable id_ed25519 used to veto a perfectly
			// good id_rsa sitting right next to it.
			keyErrs = append(keyErrs, name+" "+keyFileReason(path, keyErr))
			continue
		}
		if err := pinKnownHosts(auth, addr); err != nil {
			return nil, err
		}
		return auth, nil
	}

	return nil, noUsableKeyError(dir, keyErrs, agentErr)
}

// keyFileReason names what is actually wrong with a key file. Every failure
// used to be reported as "passphrase-protected", which sends whoever hits a
// corrupt file or a permission problem chasing `ssh-add` instead of the cause.
//
// "Is it encrypted?" is answered by the FILE, not by the error text: go-git
// reports an encrypted key as "bcrypt_pbkdf: empty password", not as
// *ssh.PassphraseMissingError, so classifying on the returned error would
// silently never match — trading one wrong diagnosis for another, which is the
// failure mode this whole change exists to remove.
func keyFileReason(path string, err error) string {
	switch {
	case isPassphraseProtected(path):
		return "is passphrase-protected — start ssh-agent and run 'ssh-add'"
	case errors.Is(err, os.ErrPermission):
		return "is not readable — check its permissions"
	default:
		return "is unusable: " + err.Error()
	}
}

// isPassphraseProtected asks x/crypto's own parser, the authority on the
// format, instead of pattern-matching somebody's error string.
func isPassphraseProtected(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var missing *gossh.PassphraseMissingError
	_, parseErr := gossh.ParseRawPrivateKey(data)
	return errors.As(parseErr, &missing)
}

// noUsableKeyError explains everything that was tried. The agent's own failure
// belongs here: reporting only the key files sends the user to the wrong half
// of the problem when the agent is what broke.
func noUsableKeyError(dir string, keyErrs []string, agentErr error) error {
	msg := fmt.Sprintf("no usable SSH key in %s (looked for %s)",
		dir, strings.Join(sshKeyFiles, ", "))
	if len(keyErrs) > 0 {
		msg += ": " + strings.Join(keyErrs, "; ")
	}
	if agentErr != nil {
		msg += fmt.Sprintf("; the SSH agent also failed: %v", agentErr)
	}
	return errors.New(msg + " — configure a key or start ssh-agent")
}

// pinKnownHosts configures host-key verification on auth using the user's
// known_hosts file. addr is "host:port" — the form the known_hosts callback is
// addressed by, and the only form in which an entry recorded for a non-standard
// port can match. It sets HostKeyCallback (so go-git verifies against
// known_hosts) AND pins HostKeyAlgorithms to the key types recorded there, so
// the server presents a key type we already trust — the fix for the
// "knownhosts: key mismatch" handshake failure. Both *ssh.PublicKeys and
// *ssh.PublicKeysCallback embed HostKeyCallbackHelper, whose fields ClientConfig
// reads, so we set them directly.
//
// A missing known_hosts is an error, not a soft skip: go-git's own fallback
// fails on it too, so reporting it here only moves the same failure earlier and
// gives it a name. This comment used to promise the opposite.
func pinKnownHosts(auth transport.AuthMethod, addr string) error {
	if addr == "" {
		// Only reachable if sshHostPort failed on a url isSSHURL accepted — our
		// bug, not the user's. Returning nil here would quietly skip the pin,
		// which is the very failure shape this feature removes.
		return fmt.Errorf("cannot pin host key algorithms: no host in the remote url")
	}
	helper := hostKeyHelperOf(auth)
	if helper == nil {
		// Every auth this package builds carries the helper, so nil means the
		// go-git types changed under us. Silently skipping the pin here is the
		// exact shape of the bug this feature removes — a protection that stops
		// working with nobody noticing — so it fails loudly instead.
		return fmt.Errorf("cannot pin host key algorithms: unexpected SSH auth type %T", auth)
	}

	cb, algos, err := knownHostsPin(addr)
	if err != nil {
		return err
	}
	helper.HostKeyCallback = cb
	helper.HostKeyAlgorithms = algos
	return nil
}

// systemKnownHosts is the file OpenSSH reads as GlobalKnownHostsFile, and the
// one go-git's own default list ends with.
const systemKnownHosts = "/etc/ssh/ssh_known_hosts"

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

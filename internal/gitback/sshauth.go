package gitback

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
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
//  1. SSH agent (Pageant / Windows OpenSSH agent / SSH_AUTH_SOCK) — if available
//  2. key files under the user's .ssh dir: id_ed25519, id_rsa, id_ecdsa (first
//     that exists), empty passphrase
//
// Returns a descriptive pt-BR error if the url is SSH and no method works.
func sshAuthFor(u string) (transport.AuthMethod, error) {
	if !isSSHURL(u) {
		return nil, nil
	}
	user := sshUser(u)

	// 1. SSH agent — best effort; on any error fall through to key files.
	if auth, err := agentDial(user); err == nil {
		return auth, nil
	}

	// 2. Key files under the .ssh dir.
	dir, err := sshDir()
	if err != nil {
		return nil, fmt.Errorf("não foi possível localizar o diretório .ssh: %w", err)
	}
	for _, name := range sshKeyFiles {
		path := filepath.Join(dir, name)
		if _, statErr := os.Stat(path); statErr != nil {
			continue
		}
		auth, keyErr := ssh.NewPublicKeysFromFile(user, path, "")
		if keyErr != nil {
			return nil, fmt.Errorf(
				"chave %s protegida por passphrase — inicie o ssh-agent e rode 'ssh-add': %w",
				path, keyErr)
		}
		return auth, nil
	}

	return nil, fmt.Errorf(
		"nenhuma chave SSH utilizável em %s (procurei id_ed25519, id_rsa, id_ecdsa) — "+
			"configure uma chave ou inicie o ssh-agent", dir)
}

package gitback

import (
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// authFor returns the AuthMethod for url, dispatching by scheme: SSH remotes
// keep the full agent/key-file/known_hosts path (sshAuthFor), http(s) remotes
// get token BasicAuth (httpAuthFor), local paths get nil.
func authFor(u string) (transport.AuthMethod, error) {
	if isSSHURL(u) {
		return sshAuthFor(u)
	}
	return httpAuthFor(u), nil
}

// isHTTPURL reports whether url is an http(s) remote.
func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// httpAuthFor returns BasicAuth for http(s) remotes when CERNE_GIT_TOKEN is
// set: the token as password, CERNE_GIT_USER (default "cerne") as username —
// Gitea and GitLab accept any non-empty username with a personal access token.
// Nil for non-http urls or when no token is set (public read stays anonymous).
// Pure function of env + url; the token never touches .cernerc or docs.lock.
func httpAuthFor(u string) transport.AuthMethod {
	if !isHTTPURL(u) {
		return nil
	}
	token := os.Getenv("CERNE_GIT_TOKEN")
	if token == "" {
		return nil
	}
	user := os.Getenv("CERNE_GIT_USER")
	if user == "" {
		user = "cerne"
	}
	return &githttp.BasicAuth{Username: user, Password: token}
}

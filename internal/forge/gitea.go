package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/version"
)

// Gitea has no CLI here, so it is HTTP with the token the user's git
// credential helper already holds for the host. The token goes only in the
// Authorization header: it is never printed, logged, stored, or put in an error.

// apiBase is the API root of host; a variable so tests point it at a server.
var apiBase = func(host string) string { return "https://" + host + "/api/v1" }

// httpDo sends the request; a variable so tests replace the transport.
var httpDo = func(req *http.Request) (*http.Response, error) { return http.DefaultClient.Do(req) }

// credentialFill asks `git credential fill` for the host's login: the secret is
// the password= line, which on a Gitea host is an access token or the account
// password. A variable so tests replace the helper.
var credentialFill = func(ctx context.Context, host string) (user, secret string) {
	cmd := exec.CommandContext(ctx, "git", "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\n\n")
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil {
		return "", "" // no helper answer is "no credentials", not a failure to report
	}
	return parseCredential(string(out))
}

// parseCredential reads the username= and password= lines of a git credential answer.
func parseCredential(out string) (user, secret string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if v, ok := strings.CutPrefix(line, "password="); ok {
			secret = v
		} else if v, ok := strings.CutPrefix(line, "username="); ok {
			user = v
		}
	}
	return user, secret
}

type gitea struct{ repo Repo }

// get reads path under the repository into v. A 404 is reported as errNotFound.
// The secret goes as "Authorization: token <secret>"; a 401 means it is the
// account password rather than an access token, so the one retry sends it as
// HTTP Basic with the helper's username (Gitea accepts both).
func (g gitea) get(ctx context.Context, v any, path string) error {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	user, secret := credentialFill(ctx, g.repo.Host)
	if secret == "" {
		return fmt.Errorf("no credentials for %s", g.repo.Host)
	}
	auths := []func(*http.Request){func(r *http.Request) { r.Header.Set("Authorization", "token "+secret) }}
	if user != "" {
		auths = append(auths, func(r *http.Request) { r.SetBasicAuth(user, secret) })
	}
	for _, auth := range auths {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase(g.repo.Host)+"/repos/"+g.repo.Path+path, nil)
		if err != nil {
			return errors.New("gitea: bad request")
		}
		auth(req)
		req.Header.Set("User-Agent", "cerne/"+version.Version)
		req.Header.Set("Accept", "application/json")
		resp, err := httpDo(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("gitea: %w", ctx.Err())
			}
			return fmt.Errorf("gitea: request to %s failed", g.repo.Host) // never the transport error text
		}
		status := resp.StatusCode
		if status == http.StatusOK {
			err = json.NewDecoder(resp.Body).Decode(v)
		}
		resp.Body.Close()
		switch {
		case status == http.StatusUnauthorized:
			continue
		case status == http.StatusNotFound:
			return errNotFound
		case status != http.StatusOK:
			return fmt.Errorf("gitea: HTTP %d", status)
		case err != nil:
			return errors.New("gitea: unreadable answer")
		}
		return nil
	}
	return errors.New("gitea: HTTP 401")
}

var errNotFound = errors.New("gitea: HTTP 404")

func (g gitea) PRForCommit(ctx context.Context, sha string) (string, int, error) {
	var pr struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	err := g.get(ctx, &pr, "/commits/"+sha+"/pull")
	if errors.Is(err, errNotFound) {
		return "", 0, noSingle(0)
	}
	if err != nil {
		return "", 0, err
	}
	if pr.HTMLURL == "" {
		return "", 0, noSingle(0)
	}
	return pr.HTMLURL, pr.Number, nil
}

func (g gitea) OnDefaultBranch(ctx context.Context, sha string) (bool, string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.get(ctx, &r, ""); err != nil {
		return false, "", err
	}
	// The commits the default branch lacks, up to the commit: none means it is inside.
	var c struct {
		TotalCommits int `json:"total_commits"`
	}
	if err := g.get(ctx, &c, "/compare/"+r.DefaultBranch+"..."+sha); err != nil {
		return false, "", err
	}
	return c.TotalCommits == 0, r.DefaultBranch, nil
}

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

// credentialToken asks `git credential fill` for the host's password, which on
// a Gitea host is the access token. A variable so tests replace the helper.
var credentialToken = func(ctx context.Context, host string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\n\n")
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil {
		return "", nil // no helper answer is "no credentials", not a failure to report
	}
	return parseCredential(string(out)), nil
}

// parseCredential reads the password= line of a git credential answer.
func parseCredential(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "password="); ok {
			return v
		}
	}
	return ""
}

type gitea struct{ repo Repo }

// get reads path under the repository into v. A 404 is reported as errNotFound.
func (g gitea) get(ctx context.Context, v any, path string) error {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	token, _ := credentialToken(ctx, g.repo.Host)
	if token == "" {
		return fmt.Errorf("no credentials for %s", g.repo.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase(g.repo.Host)+"/repos/"+g.repo.Path+path, nil)
	if err != nil {
		return fmt.Errorf("gitea: bad request")
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("User-Agent", "cerne/"+version.Version)
	req.Header.Set("Accept", "application/json")
	resp, err := httpDo(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("gitea: %w", ctx.Err())
		}
		return fmt.Errorf("gitea: request to %s failed", g.repo.Host) // never the transport error text
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gitea: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return errors.New("gitea: unreadable answer")
	}
	return nil
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

package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// callJSON runs a forge CLI under the call deadline and decodes its JSON stdout.
func callJSON(ctx context.Context, v any, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	out, err := runCLI(ctx, name, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("%s: unreadable answer: %w", name, err)
	}
	return nil
}

// hostArgs adds --hostname for a host other than the CLI's own default.
func hostArgs(host, home string) []string {
	if host == home {
		return nil
	}
	return []string{"--hostname", host}
}

// github talks to GitHub through gh.
type github struct{ repo Repo }

func (g github) api(ctx context.Context, v any, path string) error {
	args := append([]string{"api"}, hostArgs(g.repo.Host, "github.com")...)
	return callJSON(ctx, v, "gh", append(args, "repos/"+g.repo.Path+path)...)
}

func (g github) PRForCommit(ctx context.Context, sha string) (string, int, error) {
	var prs []struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := g.api(ctx, &prs, "/commits/"+sha+"/pulls"); err != nil {
		return "", 0, err
	}
	if len(prs) != 1 {
		return "", 0, noSingle(len(prs))
	}
	return prs[0].HTMLURL, prs[0].Number, nil
}

func (g github) OnDefaultBranch(ctx context.Context, sha string) (bool, string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.api(ctx, &r, ""); err != nil {
		return false, "", err
	}
	var c struct {
		Status string `json:"status"`
	}
	if err := g.api(ctx, &c, "/compare/"+r.DefaultBranch+"..."+sha); err != nil {
		return false, "", err
	}
	return inBase(c.Status), r.DefaultBranch, nil
}

// gitlab talks to GitLab through glab; the project path is url-encoded.
type gitlab struct{ repo Repo }

func (g gitlab) api(ctx context.Context, v any, path string) error {
	args := append([]string{"api"}, hostArgs(g.repo.Host, "gitlab.com")...)
	return callJSON(ctx, v, "glab", append(args, "projects/"+url.PathEscape(g.repo.Path)+path)...)
}

func (g gitlab) PRForCommit(ctx context.Context, sha string) (string, int, error) {
	var mrs []struct {
		IID    int    `json:"iid"`
		WebURL string `json:"web_url"`
	}
	if err := g.api(ctx, &mrs, "/repository/commits/"+sha+"/merge_requests"); err != nil {
		return "", 0, err
	}
	if len(mrs) != 1 {
		return "", 0, noSingle(len(mrs))
	}
	return mrs[0].WebURL, mrs[0].IID, nil
}

func (g gitlab) OnDefaultBranch(ctx context.Context, sha string) (bool, string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.api(ctx, &r, ""); err != nil {
		return false, "", err
	}
	// The merge base of the commit and the default branch is the commit itself
	// exactly when the commit is reachable from the branch.
	var mb struct {
		ID string `json:"id"`
	}
	q := "/repository/merge_base?refs[]=" + url.QueryEscape(sha) + "&refs[]=" + url.QueryEscape(r.DefaultBranch)
	if err := g.api(ctx, &mb, q); err != nil {
		return false, "", err
	}
	return mb.ID != "" && strings.HasPrefix(mb.ID, strings.ToLower(sha)), r.DefaultBranch, nil
}

package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/version"
)

// gitea mirrors cards as issues on one Gitea repository over its HTTP API
// (F-0452), with the login `git credential fill` holds for the host: no token
// in .cernerc. Requests are always https, the secret goes only in the
// Authorization header, and forge.HTTPDo never follows a redirect, so the
// secret reaches only the host the record names.
type gitea struct {
	name string
	repo forge.Repo
}

func newGitea(c Config) (IssueTracker, error) {
	u, err := url.Parse(c.URL)
	if err == nil {
		// The login is sent to the host the record names: never an IP literal
		// or localhost, which name a service on this machine or network
		// rather than a Gitea.
		if h := u.Hostname(); net.ParseIP(h) != nil || strings.EqualFold(h, "localhost") {
			return nil, fmt.Errorf("%w: gitea tracker host %q is an IP address or localhost; use the Gitea host name", ErrTrackerURL, h)
		}
	}
	if err == nil && u.Scheme == "https" && u.RawQuery == "" && u.Fragment == "" { // tracker.New already refused a user and password
		if m := ghPath.FindStringSubmatch(u.Path); m != nil && !dots(m[1]) && !dots(m[2]) {
			return gitea{name: c.Name, repo: forge.Repo{Host: u.Host, Path: m[1] + "/" + m[2]}}, nil
		}
	}
	return nil, fmt.Errorf("%w %q: want https://<host>/<owner>/<repo>", ErrTrackerURL, redactURL(c.URL))
}

// giteaIssue is the part of a Gitea issue the adapter reads.
type giteaIssue struct {
	Number  int64  `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"` // open | closed
}

// giteaCall is one public call: the login is read once.
type giteaCall struct {
	g            gitea
	user, secret string
}

func (g gitea) call(ctx context.Context) (*giteaCall, error) {
	ctx, cancel := context.WithTimeout(ctx, forge.CallTimeout)
	defer cancel()
	user, secret := forge.CredentialFill(ctx, g.repo.Host)
	if secret == "" {
		return nil, fmt.Errorf("gitea %s: no credentials from git credential fill; store an access token for the host", g.repo.Host)
	}
	return &giteaCall{g: g, user: user, secret: secret}, nil
}

// do sends one request to path under the repository's API and decodes a 2xx
// answer into out (nil: drop it). The secret goes as "Authorization: token";
// a 401 means it is the account password, so when the helper names a user
// the one retry sends it as HTTP Basic, as the delivery lookup does
// (internal/forge/gitea.go). Every request carries the cerne/<version>
// User-Agent a Cloudflare in front of the host demands. An error names only
// the host and the status: never the transport's text.
func (c *giteaCall) do(ctx context.Context, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, forge.CallTimeout)
	defer cancel()
	host := c.g.repo.Host
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return fmt.Errorf("gitea %s: %w", host, err)
		}
	}
	auths := []func(*http.Request){func(r *http.Request) { r.Header.Set("Authorization", "token "+c.secret) }}
	if c.user != "" {
		auths = append(auths, func(r *http.Request) { r.SetBasicAuth(c.user, c.secret) })
	}
	for _, auth := range auths {
		req, err := http.NewRequestWithContext(ctx, method, "https://"+host+"/api/v1/repos/"+c.g.repo.Path+path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("gitea %s: bad request", host)
		}
		auth(req)
		req.Header.Set("User-Agent", "cerne/"+version.Version)
		req.Header.Set("Accept", "application/json")
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := forge.HTTPDo(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("gitea %s: %w", host, ctx.Err())
			}
			return fmt.Errorf("gitea %s: request failed", host)
		}
		status := resp.StatusCode
		if status/100 == 2 && out != nil {
			err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
		}
		resp.Body.Close()
		switch {
		case status == http.StatusUnauthorized:
			continue
		case status/100 != 2: // a 3xx too: forge.HTTPDo did not follow it, and neither does this
			return fmt.Errorf("gitea %s: HTTP %d", host, status)
		case err != nil:
			return fmt.Errorf("gitea %s: unreadable answer", host)
		}
		return nil
	}
	return fmt.Errorf("gitea %s: HTTP 401", host)
}

func (g gitea) ref(iss giteaIssue) Ref {
	return Ref{Tracker: g.name, ID: strconv.FormatInt(iss.Number, 10), URL: iss.HTMLURL}
}

var errGiteaTodo = errors.New("gitea: not implemented yet")

func (g gitea) Create(context.Context, Item) (Ref, error) { return Ref{}, errGiteaTodo }

func (g gitea) Update(context.Context, Ref, Item) error { return errGiteaTodo }

// ListOpen lists the open issues.
func (g gitea) ListOpen(ctx context.Context, label string) ([]Remote, error) {
	c, err := g.call(ctx)
	if err != nil {
		return nil, err
	}
	q := "/issues?state=open&type=issues&limit=50"
	if label != "" {
		q += "&labels=" + url.QueryEscape(label)
	}
	var list []giteaIssue
	if err := c.do(ctx, http.MethodGet, q, nil, &list); err != nil {
		return nil, err
	}
	var out []Remote
	for _, iss := range list {
		out = append(out, Remote{Ref: g.ref(iss), Title: iss.Title, Body: iss.Body})
	}
	return out, nil
}

func (g gitea) CommitRef(Ref, string, bool) string { return "" }

func (g gitea) Owns(Ref) bool { return false }

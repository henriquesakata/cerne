package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
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
	Number  int64        `json:"number"`
	HTMLURL string       `json:"html_url"`
	Title   string       `json:"title"`
	Body    string       `json:"body"`
	State   string       `json:"state"` // open | closed
	Labels  []giteaLabel `json:"labels"`
}

type giteaLabel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// labelColor is the color of the cerne:* labels the adapter creates.
const labelColor = "#5319e7"

// giteaCall is one public call: the login is read once, and the label ids
// live only until the call returns.
type giteaCall struct {
	g            gitea
	user, secret string
	labels       map[string]int64 // name -> id; nil until read
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

// Create opens the issue with its state label, then moves it to it.State when
// that is a closed one.
func (g gitea) Create(ctx context.Context, it Item) (Ref, error) {
	c, err := g.call(ctx)
	if err != nil {
		return Ref{}, err
	}
	id, err := c.labelID(ctx, stateLabel(it.State))
	if err != nil {
		return Ref{}, err
	}
	var iss giteaIssue
	if err := c.do(ctx, http.MethodPost, "/issues", map[string]any{"title": it.Title, "body": it.Body(), "labels": []int64{id}}, &iss); err != nil {
		return Ref{}, err
	}
	return g.ref(iss), c.apply(ctx, iss, it) // a Ref with an error: the issue exists, its closed state did not land
}

// Update reads the issue and writes only what differs from it.
func (g gitea) Update(ctx context.Context, r Ref, it Item) error {
	if !digits(r.ID) {
		return fmt.Errorf("%w: issue number %q", errBadRef, r.ID)
	}
	c, err := g.call(ctx)
	if err != nil {
		return err
	}
	var iss giteaIssue
	if err := c.do(ctx, http.MethodGet, "/issues/"+r.ID, nil, &iss); err != nil {
		return err
	}
	return c.apply(ctx, iss, it)
}

// apply moves cur toward it: the state label, then open or closed. Gitea has
// no close reason, so a rejected or superseded card's issue is told apart
// from a done one by its cerne:<state> label alone.
func (c *giteaCall) apply(ctx context.Context, cur giteaIssue, it Item) error {
	issue := "/issues/" + strconv.FormatInt(cur.Number, 10)
	if err := c.setStateLabel(ctx, issue, cur.Labels, stateLabel(it.State)); err != nil {
		return err
	}
	edit := map[string]string{}
	if it.Title != "" && cur.Title != it.Title {
		edit["title"] = it.Title
	}
	if it.Title != "" && cur.Body != it.Body() {
		edit["body"] = it.Body()
	}
	shut, closed := closeReason(it.State) != "", cur.State == "closed"
	switch {
	case closed && it.NoReopen: // the merge closed it: only the labels move
	case closed && !shut:
		edit["state"] = "open"
	case !closed && shut:
		edit["state"] = "closed"
	}
	if len(edit) > 0 {
		if err := c.do(ctx, http.MethodPatch, issue, edit, nil); err != nil {
			return err
		}
	}
	// The superseded comment goes only with the call that closes the issue,
	// so a sync never repeats it.
	if edit["state"] == "closed" && it.State == domain.StateSuperseded && it.Successor != nil {
		return c.do(ctx, http.MethodPost, issue+"/comments", map[string]string{"body": "Superseded by " + it.Successor.URL}, nil)
	}
	return nil
}

// setStateLabel leaves want as the issue's only cerne:* label and keeps every
// other label: it adds want first, so the issue never lacks a state label.
func (c *giteaCall) setStateLabel(ctx context.Context, issue string, cur []giteaLabel, want string) error {
	if !slices.ContainsFunc(cur, func(l giteaLabel) bool { return l.Name == want }) {
		id, err := c.labelID(ctx, want)
		if err != nil {
			return err
		}
		if err := c.do(ctx, http.MethodPost, issue+"/labels", map[string][]int64{"labels": {id}}, nil); err != nil {
			return err
		}
	}
	for _, l := range cur {
		if l.Name != want && strings.HasPrefix(l.Name, "cerne:") {
			if err := c.do(ctx, http.MethodDelete, issue+"/labels/"+strconv.FormatInt(l.ID, 10), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// labelID is the id of the repository label named name, the form Gitea's API
// takes; a label the repository lacks is created. Every page is read: a label
// missed on a later page would be created again.
func (c *giteaCall) labelID(ctx context.Context, name string) (int64, error) {
	if c.labels == nil {
		labels := map[string]int64{}
		for page := 1; ; page++ {
			var list []giteaLabel
			if err := c.do(ctx, http.MethodGet, "/labels?limit=50&page="+strconv.Itoa(page), nil, &list); err != nil {
				return 0, err
			}
			if len(list) == 0 {
				break
			}
			for _, l := range list {
				if _, dup := labels[l.Name]; !dup {
					labels[l.Name] = l.ID
				}
			}
		}
		c.labels = labels
	}
	if id, ok := c.labels[name]; ok {
		return id, nil
	}
	var l giteaLabel
	if err := c.do(ctx, http.MethodPost, "/labels", map[string]string{"name": name, "color": labelColor}, &l); err != nil {
		return 0, err
	}
	c.labels[name] = l.ID
	return l.ID, nil
}

// ListOpen lists up to 100 open issues carrying label, never PRs. Gitea drops
// a label name the repository lacks from its filter and then lists every
// issue, so the label is checked here too. Pages are read until an empty one:
// the server may cap a page below the limit asked.
func (g gitea) ListOpen(ctx context.Context, label string) ([]Remote, error) {
	c, err := g.call(ctx)
	if err != nil {
		return nil, err
	}
	q := "/issues?state=open&type=issues&limit=50"
	if label != "" {
		q += "&labels=" + url.QueryEscape(label)
	}
	var out []Remote
	for page, seen := 1, 0; seen < 100; page++ {
		var list []giteaIssue
		if err := c.do(ctx, http.MethodGet, q+"&page="+strconv.Itoa(page), nil, &list); err != nil {
			return nil, err
		}
		if len(list) == 0 {
			break
		}
		list = list[:min(len(list), 100-seen)]
		seen += len(list)
		for _, iss := range list {
			if label == "" || slices.ContainsFunc(iss.Labels, func(l giteaLabel) bool { return l.Name == label }) {
				out = append(out, Remote{Ref: g.ref(iss), Title: iss.Title, Body: iss.Body})
			}
		}
	}
	return out, nil
}

// Gitea links an issue from a commit the way GitHub does (#N in the same
// repository, owner/repo#N on the same host) and its issue URLs have GitHub's
// shape, https://<host>/<owner>/<repo>/issues/<n>: CommitRef and Owns are
// the GitHub adapter's, on this repository.

func (g gitea) CommitRef(r Ref, commitURL string, closing bool) string {
	return github{repo: g.repo}.CommitRef(r, commitURL, closing)
}

func (g gitea) Owns(r Ref) bool { return github{repo: g.repo}.Owns(r) }

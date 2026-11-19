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
	"regexp"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/version"
)

// jira mirrors cards as issues of one Jira project (F-0453), on Jira Cloud or
// Data Center, over the REST API with the login `git credential fill` holds
// for the site: no token in .cernerc. Requests are always https, the secret
// goes only in the Authorization header, and forge.HTTPDo never follows a
// redirect, so the secret reaches only the site the record names.
type jira struct {
	name      string
	host      string // the site, with its port
	root      string // the context path: "" or, on Data Center, e.g. "/jira"
	project   string // the project key
	atlassian bool   // a *.atlassian.net site: Cloud, with no call to ask
}

// jiraPath is the [/<context>]/browse/<KEY> of a mirror.trackers url.
var jiraPath = regexp.MustCompile(`^((?:/[A-Za-z0-9._~-]+)*)/browse/([A-Z][A-Z0-9_]*)/?$`)

func newJira(c Config) (IssueTracker, error) {
	u, err := url.Parse(c.URL)
	if err == nil {
		// The login is sent to the host the record names: never an IP literal
		// or localhost, which name a service on this machine or network
		// rather than a Jira. Data Center is reached by its DNS name.
		if h := u.Hostname(); net.ParseIP(h) != nil || strings.EqualFold(h, "localhost") {
			return nil, fmt.Errorf("%w: jira tracker host %q is an IP address or localhost; use the Jira site name", ErrTrackerURL, h)
		}
	}
	if err == nil && u.Scheme == "https" && u.RawQuery == "" && u.Fragment == "" { // tracker.New already refused a user and password
		if m := jiraPath.FindStringSubmatch(u.Path); m != nil && !slices.ContainsFunc(strings.Split(m[1], "/")[1:], dots) {
			return jira{name: c.Name, host: u.Host, root: m[1], project: m[2],
				atlassian: strings.HasSuffix(strings.ToLower(u.Hostname()), ".atlassian.net")}, nil
		}
	}
	return nil, fmt.Errorf("%w %q: want https://<site>/browse/<PROJECT KEY>", ErrTrackerURL, redactURL(c.URL))
}

// jiraCall is one public call: the login is read and the deployment detected
// once, and live only until the call returns.
type jiraCall struct {
	j            jira
	user, secret string
	cloud        bool // Cloud: API v3, ADF bodies, HTTP Basic. Data Center: API v2, text bodies, Bearer
	basic        bool // send HTTP Basic, not the Bearer token
}

// call reads the site's login and tells Cloud from Data Center, the one place
// that does: a *.atlassian.net site is Cloud; any other asks serverInfo,
// where deploymentType "Cloud" is Cloud and any other value is Data Center
// ("Server" there too). A failure, a redirect or an unreadable answer is an
// error, never a guess.
func (j jira) call(ctx context.Context) (*jiraCall, error) {
	fctx, cancel := context.WithTimeout(ctx, forge.CallTimeout)
	user, secret := forge.CredentialFill(fctx, j.host)
	cancel()
	if secret == "" {
		return nil, fmt.Errorf("jira %s: no credentials from git credential fill; store the account e-mail and an API token (Cloud) or a personal access token (Data Center) for the host", j.host)
	}
	c := &jiraCall{j: j, user: user, secret: secret, cloud: j.atlassian}
	if !c.cloud {
		var info struct {
			DeploymentType string `json:"deploymentType"`
		}
		if err := c.do(ctx, http.MethodGet, "/rest/api/2/serverInfo", nil, &info); err != nil {
			return nil, err
		}
		if info.DeploymentType == "" {
			return nil, fmt.Errorf("jira %s: unreadable answer", j.host)
		}
		c.cloud = info.DeploymentType == "Cloud"
	}
	if c.cloud {
		if user == "" {
			return nil, fmt.Errorf("jira %s: git credential fill gave no user; Jira Cloud wants the account e-mail as the user and an API token as the password", j.host)
		}
		c.basic = true
	}
	return c, nil
}

// api is path under the REST API of the deployment.
func (c *jiraCall) api(path string) string {
	if c.cloud {
		return "/rest/api/3" + path
	}
	return "/rest/api/2" + path
}

// do sends one request to path under the site and decodes a 2xx answer into
// out (nil: drop it). Data Center gets the secret as a Bearer personal access
// token; a 401 when the helper names a user means it is a password, so the
// one retry, and every later request of the call, sends HTTP Basic. Every
// request carries the cerne/<version> User-Agent. An error names only the
// host and the status: never the transport's text.
func (c *jiraCall) do(ctx context.Context, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, forge.CallTimeout)
	defer cancel()
	host := c.j.host
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return fmt.Errorf("jira %s: %w", host, err)
		}
	}
	bearer := !c.basic
	for {
		req, err := http.NewRequestWithContext(ctx, method, "https://"+host+c.j.root+path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("jira %s: bad request", host)
		}
		if bearer {
			req.Header.Set("Authorization", "Bearer "+c.secret)
		} else {
			req.SetBasicAuth(c.user, c.secret)
		}
		req.Header.Set("User-Agent", "cerne/"+version.Version)
		req.Header.Set("Accept", "application/json")
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := forge.HTTPDo(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("jira %s: %w", host, ctx.Err())
			}
			return fmt.Errorf("jira %s: request failed", host)
		}
		status := resp.StatusCode
		if status/100 == 2 && out != nil {
			err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
		}
		resp.Body.Close()
		switch {
		case status == http.StatusUnauthorized && bearer && c.user != "":
			bearer = false
			continue
		case status/100 != 2: // a 3xx too: forge.HTTPDo did not follow it, and neither does this
			return fmt.Errorf("jira %s: HTTP %d", host, status)
		case err != nil:
			return fmt.Errorf("jira %s: unreadable answer", host)
		}
		c.basic = !bearer
		return nil
	}
}

// jiraIssue is the part of a Jira issue the adapter reads.
type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
	} `json:"fields"`
}

func (j jira) ref(key string) Ref {
	return Ref{Tracker: j.name, ID: key, URL: "https://" + j.host + j.root + "/browse/" + key}
}

// ownKey reports whether id is an issue key of this project: <KEY>-<n>.
func (j jira) ownKey(id string) bool {
	n, ok := strings.CutPrefix(id, j.project+"-")
	return ok && digits(n)
}

func (j jira) Create(context.Context, Item) (Ref, error) {
	return Ref{}, errors.New("jira: create not implemented")
}

func (j jira) Update(context.Context, Ref, Item) error {
	return errors.New("jira: update not implemented")
}

// ListOpen lists up to 100 issues of the project whose status is not in the
// Done category. Cloud removed /search for /search/jql.
// ponytail: one page; paginate when a project outgrows it.
func (j jira) ListOpen(ctx context.Context, _ string) ([]Remote, error) {
	c, err := j.call(ctx)
	if err != nil {
		return nil, err
	}
	search := "/search"
	if c.cloud {
		search = "/search/jql"
	}
	q := url.Values{"jql": {"project = " + j.project + " AND statusCategory != Done"}, "fields": {"summary"}, "maxResults": {"100"}}
	var res struct {
		Issues []jiraIssue `json:"issues"`
	}
	if err := c.do(ctx, http.MethodGet, c.api(search)+"?"+q.Encode(), nil, &res); err != nil {
		return nil, err
	}
	var out []Remote
	for _, iss := range res.Issues {
		if j.ownKey(iss.Key) {
			out = append(out, Remote{Ref: j.ref(iss.Key), Title: iss.Fields.Summary})
		}
	}
	return out, nil
}

func (j jira) CommitRef(Ref, string, bool) string { return "" }

func (j jira) Owns(Ref) bool { return false }

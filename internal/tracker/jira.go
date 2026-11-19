package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/henriquesakata/cerne/internal/domain"
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
		status, fields := resp.StatusCode, ""
		switch {
		case status/100 == 2 && out != nil:
			err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
		case status == http.StatusBadRequest:
			fields = jiraErrorFields(resp.Body)
		}
		resp.Body.Close()
		switch {
		case status == http.StatusUnauthorized && bearer && c.user != "":
			bearer = false
			continue
		case status/100 != 2: // a 3xx too: forge.HTTPDo did not follow it, and neither does this
			return fmt.Errorf("jira %s: HTTP %d%s", host, status, fields)
		case err != nil:
			return fmt.Errorf("jira %s: unreadable answer", host)
		}
		c.basic = !bearer
		return nil
	}
}

// jiraFieldName is a field id Jira names in a 400 ("summary",
// "customfield_10010"): anything else in the answer is not repeated.
var jiraFieldName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// jiraErrorFields is " (fields: a, b)" for the fields a 400 answer refuses,
// or "": a required field the adapter does not fill is named, Jira's own
// message is not.
func jiraErrorFields(r io.Reader) string {
	var e struct {
		Errors map[string]any `json:"errors"`
	}
	if json.NewDecoder(io.LimitReader(r, 64<<10)).Decode(&e) != nil {
		return ""
	}
	var names []string
	for _, k := range slices.Sorted(maps.Keys(e.Errors)) {
		if jiraFieldName.MatchString(k) {
			names = append(names, k)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " (fields: " + strings.Join(names, ", ") + ")"
}

// jiraIssue is the part of a Jira issue the adapter reads; Description is
// ADF on API v3 and text on v2.
type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Labels      []string        `json:"labels"`
		Status      struct {
			StatusCategory jiraCategoryRef `json:"statusCategory"`
		} `json:"status"`
	} `json:"fields"`
}

type jiraCategoryRef struct {
	Key string `json:"key"` // new | indeterminate | done
}

// jiraTransition is one transition the issue offers.
type jiraTransition struct {
	ID string `json:"id"`
	To struct {
		StatusCategory jiraCategoryRef `json:"statusCategory"`
	} `json:"to"`
}

// errJiraNoTransition is an issue the workflow cannot take to the card
// state's category from where it is: the core prints it as a warning, and
// the transition of the card stands.
var errJiraNoTransition = errors.New("no transition to the status category")

// jiraCategory is the status category every Jira workflow has for a card
// state: To Do (new) until work starts, In Progress (indeterminate) through
// review, Done (done) for every state that closes an issue.
func jiraCategory(s domain.FeatureState) string {
	switch {
	case closeReason(s) != "":
		return "done"
	case s == domain.StateInDevelopment || s == domain.StateImplemented:
		return "indeterminate"
	}
	return "new"
}

// jiraADF is a node of the Atlassian Document Format, the body of API v3.
type jiraADF struct {
	Type    string    `json:"type"`
	Version int       `json:"version,omitempty"`
	Text    string    `json:"text,omitempty"`
	Content []jiraADF `json:"content,omitempty"`
}

// doc is s in the deployment's body form: on Cloud an ADF doc with one
// paragraph per block separated by a blank line, a hard break per line break
// inside it; on Data Center s itself.
func (c *jiraCall) doc(s string) any {
	if !c.cloud {
		return s
	}
	doc := jiraADF{Type: "doc", Version: 1}
	for _, block := range strings.Split(s, "\n\n") {
		p := jiraADF{Type: "paragraph"}
		for i, line := range strings.Split(block, "\n") {
			if i > 0 {
				p.Content = append(p.Content, jiraADF{Type: "hardBreak"})
			}
			if line != "" { // ADF has no empty text node
				p.Content = append(p.Content, jiraADF{Type: "text", Text: line})
			}
		}
		doc.Content = append(doc.Content, p)
	}
	return doc
}

// text reads a body back: the inverse of doc for what doc writes, and plain
// text for the rest of an outside issue's ADF (marks and attributes drop).
// A missing or unreadable body is "".
func (c *jiraCall) text(raw json.RawMessage) string {
	if !c.cloud {
		var s string
		_ = json.Unmarshal(raw, &s)
		return strings.ReplaceAll(s, "\r\n", "\n")
	}
	var doc jiraADF
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return jiraText(doc)
}

// jiraText joins the doc's blocks with a blank line, nested blocks (list
// items) with a line break, and inline nodes with nothing.
func jiraText(n jiraADF) string {
	switch n.Type {
	case "text":
		return n.Text
	case "hardBreak":
		return "\n"
	}
	sep := ""
	if n.Type == "doc" {
		sep = "\n\n"
	} else if slices.ContainsFunc(n.Content, func(c jiraADF) bool { return len(c.Content) > 0 }) {
		sep = "\n"
	}
	parts := make([]string, len(n.Content))
	for i, c := range n.Content {
		parts[i] = jiraText(c)
	}
	return strings.Join(parts, sep)
}

func (j jira) ref(key string) Ref {
	return Ref{Tracker: j.name, ID: key, URL: "https://" + j.host + j.root + "/browse/" + key}
}

// ownKey reports whether id is an issue key of this project: <KEY>-<n>.
func (j jira) ownKey(id string) bool {
	n, ok := strings.CutPrefix(id, j.project+"-")
	return ok && digits(n)
}

// Create opens the issue as the project's Task, or its first standard type.
func (j jira) Create(ctx context.Context, it Item) (Ref, error) {
	c, err := j.call(ctx)
	if err != nil {
		return Ref{}, err
	}
	typ, err := c.issueType(ctx)
	if err != nil {
		return Ref{}, err
	}
	fields := map[string]any{"project": map[string]string{"key": j.project}, "issuetype": map[string]string{"id": typ},
		"summary": it.Title, "description": c.doc(it.Body())}
	var created struct {
		Key string `json:"key"`
	}
	if err := c.do(ctx, http.MethodPost, c.api("/issue"), map[string]any{"fields": fields}, &created); err != nil {
		return Ref{}, err
	}
	if !j.ownKey(created.Key) {
		return Ref{}, fmt.Errorf("jira %s: unreadable answer", j.host)
	}
	r := j.ref(created.Key)
	cur, err := c.issue(ctx, r.ID)
	if err != nil {
		return r, err
	}
	return r, c.apply(ctx, r.ID, cur, it) // a Ref with an error: the issue exists, its status did not land
}

// issueType is the id of the project's Task type or, when it has none, of its
// first standard type: not a subtask and, on Cloud, not above the base
// hierarchy level (an epic). Data Center sends no level.
func (c *jiraCall) issueType(ctx context.Context) (string, error) {
	var p struct {
		IssueTypes []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Subtask bool   `json:"subtask"`
			Level   *int   `json:"hierarchyLevel"`
		} `json:"issueTypes"`
	}
	if err := c.do(ctx, http.MethodGet, c.api("/project/"+c.j.project), nil, &p); err != nil {
		return "", err
	}
	first := ""
	for _, t := range p.IssueTypes {
		if t.ID == "" || t.Subtask || t.Level != nil && *t.Level != 0 {
			continue
		}
		if strings.EqualFold(t.Name, "Task") {
			return t.ID, nil
		}
		if first == "" {
			first = t.ID
		}
	}
	if first == "" {
		return "", fmt.Errorf("jira %s: project %s has no issue type but subtasks", c.j.host, c.j.project)
	}
	return first, nil
}

// Update reads the issue and moves it toward it.
func (j jira) Update(ctx context.Context, r Ref, it Item) error {
	if !j.ownKey(r.ID) {
		return fmt.Errorf("%w: issue key %q", errBadRef, r.ID)
	}
	c, err := j.call(ctx)
	if err != nil {
		return err
	}
	cur, err := c.issue(ctx, r.ID)
	if err != nil {
		return err
	}
	return c.apply(ctx, r.ID, cur, it)
}

func (c *jiraCall) issue(ctx context.Context, key string) (jiraIssue, error) {
	var iss jiraIssue
	err := c.do(ctx, http.MethodGet, c.api("/issue/"+key+"?fields=summary,description,status,labels"), nil, &iss)
	return iss, err
}

// apply moves cur, issue key, to the status category of it.State by the
// first transition the issue offers into it; an issue already there is not
// moved. With no such transition the issue keeps its status, is labeled
// cerne:<state>, and apply returns errJiraNoTransition. Any other cerne:*
// label goes in the same edit. The edit goes while the issue is out of the
// Done category: a workflow may lock editing in a closed status.
func (c *jiraCall) apply(ctx context.Context, key string, cur jiraIssue, it Item) error {
	path := c.api("/issue/" + key)
	want := jiraCategory(it.State)
	var move *jiraTransition
	if cur.Fields.Status.StatusCategory.Key != want {
		var ts struct {
			Transitions []jiraTransition `json:"transitions"`
		}
		if err := c.do(ctx, http.MethodGet, path+"/transitions?expand=transitions.fields", nil, &ts); err != nil {
			return err
		}
		if i := slices.IndexFunc(ts.Transitions, func(t jiraTransition) bool { return t.To.StatusCategory.Key == want }); i >= 0 {
			move = &ts.Transitions[i]
		}
	}
	stuck := move == nil && cur.Fields.Status.StatusCategory.Key != want
	label := ""
	if stuck {
		label = stateLabel(it.State)
	}
	edit := map[string]any{}
	if ops := jiraLabelOps(cur.Fields.Labels, label); len(ops) > 0 {
		edit["update"] = map[string]any{"labels": ops}
	}
	transition := func() error {
		return c.do(ctx, http.MethodPost, path+"/transitions", map[string]any{"transition": map[string]string{"id": move.ID}}, nil)
	}
	if move != nil && want != "done" {
		if err := transition(); err != nil {
			return err
		}
	}
	if len(edit) > 0 {
		if err := c.do(ctx, http.MethodPut, path, edit, nil); err != nil {
			return err
		}
	}
	if move != nil && want == "done" {
		if err := transition(); err != nil {
			return err
		}
	}
	if stuck {
		return fmt.Errorf("jira %s: %s: %w %s; labeled it %s", c.j.host, key, errJiraNoTransition, want, label)
	}
	return nil
}

// jiraLabelOps is the label edit that leaves want ("" = none) as the issue's
// only cerne:* label and keeps every other label.
func jiraLabelOps(cur []string, want string) []map[string]string {
	var ops []map[string]string
	if want != "" && !slices.Contains(cur, want) {
		ops = append(ops, map[string]string{"add": want})
	}
	for _, l := range cur {
		if l != want && strings.HasPrefix(l, "cerne:") {
			ops = append(ops, map[string]string{"remove": l})
		}
	}
	return ops
}

// jiraQuote is the JQL string escape: a backslash and a double quote.
var jiraQuote = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// ListOpen lists up to 100 issues of the project whose status is not in the
// Done category, carrying label when it is not "". Cloud removed /search for
// /search/jql. A label with a control character is refused: JQL has no
// escape for one. ponytail: one page; paginate when a project outgrows it.
func (j jira) ListOpen(ctx context.Context, label string) ([]Remote, error) {
	jql := "project = " + j.project + " AND statusCategory != Done"
	if strings.ContainsFunc(label, unicode.IsControl) {
		return nil, fmt.Errorf("jira %s: label %q has a control character", j.host, label)
	}
	if label != "" {
		jql += ` AND labels = "` + jiraQuote.Replace(label) + `"`
	}
	c, err := j.call(ctx)
	if err != nil {
		return nil, err
	}
	search := "/search"
	if c.cloud {
		search = "/search/jql"
	}
	q := url.Values{"jql": {jql}, "fields": {"summary,description"}, "maxResults": {"100"}}
	var res struct {
		Issues []jiraIssue `json:"issues"`
	}
	if err := c.do(ctx, http.MethodGet, c.api(search)+"?"+q.Encode(), nil, &res); err != nil {
		return nil, err
	}
	var out []Remote
	for _, iss := range res.Issues {
		if j.ownKey(iss.Key) {
			out = append(out, Remote{Ref: j.ref(iss.Key), Title: iss.Fields.Summary, Body: c.text(iss.Fields.Description)})
		}
	}
	return out, nil
}

func (j jira) CommitRef(Ref, string, bool) string { return "" }

func (j jira) Owns(Ref) bool { return false }

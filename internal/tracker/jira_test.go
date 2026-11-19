package tracker_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
	"github.com/henriquesakata/cerne/internal/version"
)

// init keeps every test of this package off the network and off the user's
// credential helper: a test that forgets to install the emulator fails
// instead of writing to a real Jira (F-0453).
func init() {
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("test guard: %s %s must not reach the network (F-0453)", req.Method, req.URL.Host)
	}
	forge.CredentialFill = func(context.Context, string) (string, string) { return "", "" }
}

const (
	jiraSecret = "s3cret-f0453"
	jiraEmail  = "bot@example.com"
	jiraCloud  = "https://acme.atlassian.net/browse/PROJ"
	jiraDC     = "https://jira.example/jira/browse/PROJ"
)

// jiraReq is one request the adapter sent, as the transport saw it.
type jiraReq struct {
	method, url string
	header      http.Header
	body        string
}

// installJira routes forge.HTTPDo to h in process, recording each request,
// and makes the credential helper answer user and jiraSecret.
func installJira(t *testing.T, user string, h http.Handler) *[]jiraReq {
	t.Helper()
	var reqs []jiraReq
	prevDo, prevFill := forge.HTTPDo, forge.CredentialFill
	t.Cleanup(func() { forge.HTTPDo, forge.CredentialFill = prevDo, prevFill })
	forge.CredentialFill = func(context.Context, string) (string, string) { return user, jiraSecret }
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		reqs = append(reqs, jiraReq{req.Method, req.URL.String(), req.Header.Clone(), string(body)})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Result(), nil
	}
	return &reqs
}

func newJiraTracker(t *testing.T, raw string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "jt", Kind: "jira", URL: raw})
	if err != nil {
		t.Fatalf("New(jira, %s): %v", raw, err)
	}
	return tr
}

// jiraStatus is one status of the emulated workflow and its category key.
type jiraStatus struct{ name, cat string }

// jiraMove is one workflow transition. from nil is a global transition (any
// status); res nil means the transition screen has no resolution field.
type jiraMove struct {
	id, name, to string
	from, res    []string
	required     bool
}

// jiraType is one issue type of the project; Level is Cloud's hierarchyLevel
// (nil on Data Center, which does not send it).
type jiraType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
	Level   *int   `json:"hierarchyLevel,omitempty"`
}

type jiraFakeIssue struct {
	key, summary, status, resolution, typ string
	desc                                  json.RawMessage // as the API holds it: ADF on Cloud, a string on Data Center
	labels                                []string
	comments                              []json.RawMessage
}

// jiraFake emulates, behind forge.HTTPDo, the slice of the Jira REST API the
// adapter calls, for project PROJ: Cloud speaks only v3 (ADF bodies, Basic
// e-mail:token, /search removed for /search/jql), Data Center only v2 (text
// bodies, Bearer token). Every request needs the cerne/ User-Agent.
type jiraFake struct {
	cloud      bool
	host, root string
	deployment string // serverInfo's deploymentType; "" = not served
	basicOnly  bool   // a Data Center that refuses the Bearer token
	user       string // the login the emulator accepts with jiraSecret, on Basic
	statuses   []jiraStatus
	moves      []jiraMove
	types      []jiraType
	required   map[string]string // create fields the project demands -> Jira's message
	issues     map[string]*jiraFakeIssue
	order      []string // keys, in creation order
	jql        []string // every JQL searched
	moved      []string // every transition id applied
	reqs       *[]jiraReq
}

var jiraResolutionIDs = map[string]string{"Done": "10000", "Won't Do": "10001", "Duplicate": "10002", "Fixed": "1"}

func jiraDefaults(f *jiraFake) *jiraFake {
	f.issues = map[string]*jiraFakeIssue{}
	f.statuses = []jiraStatus{{"To Do", "new"}, {"In Progress", "indeterminate"}, {"Done", "done"}}
	f.moves = []jiraMove{{id: "11", name: "To Do", to: "To Do"}, {id: "21", name: "In Progress", to: "In Progress"},
		{id: "31", name: "Done", to: "Done", res: []string{"Done", "Won't Do", "Duplicate"}}}
	zero := 0
	f.types = []jiraType{{ID: "10001", Name: "Task", Level: &zero}, {ID: "10002", Name: "Sub-task", Subtask: true}}
	return f
}

// newJiraCloudFake is Jira Cloud at acme.atlassian.net.
func newJiraCloudFake(t *testing.T) *jiraFake {
	f := jiraDefaults(&jiraFake{cloud: true, host: "acme.atlassian.net", user: jiraEmail})
	f.reqs = installJira(t, jiraEmail, f.handler())
	return f
}

// newJiraDCFake is Jira Data Center at jira.example under the /jira context.
func newJiraDCFake(t *testing.T) *jiraFake {
	f := jiraDefaults(&jiraFake{host: "jira.example", root: "/jira", deployment: "Server"})
	f.reqs = installJira(t, "", f.handler())
	return f
}

func (f *jiraFake) api() string {
	if f.cloud {
		return f.root + "/rest/api/3"
	}
	return f.root + "/rest/api/2"
}

func (f *jiraFake) authorized(r *http.Request) bool {
	u, p, basic := r.BasicAuth()
	if f.cloud || f.basicOnly {
		return basic && u == f.user && p == jiraSecret
	}
	return r.Header.Get("Authorization") == "Bearer "+jiraSecret
}

func (f *jiraFake) handler() http.Handler {
	mux := http.NewServeMux()
	if f.deployment != "" {
		mux.HandleFunc("GET "+f.root+"/rest/api/2/serverInfo", func(w http.ResponseWriter, _ *http.Request) {
			jiraReply(w, http.StatusOK, map[string]string{"deploymentType": f.deployment, "version": "9.12.0"})
		})
	}
	api := f.api()
	mux.HandleFunc("GET "+api+"/project/{key}", f.project)
	mux.HandleFunc("POST "+api+"/issue", f.create)
	mux.HandleFunc("GET "+api+"/issue/{key}", f.issue(func(w http.ResponseWriter, _ *http.Request, iss *jiraFakeIssue) {
		jiraReply(w, http.StatusOK, f.view(iss))
	}))
	mux.HandleFunc("PUT "+api+"/issue/{key}", f.issue(f.edit))
	mux.HandleFunc("GET "+api+"/issue/{key}/transitions", f.issue(f.transitions))
	mux.HandleFunc("POST "+api+"/issue/{key}/transitions", f.issue(f.transition))
	mux.HandleFunc("POST "+api+"/issue/{key}/comment", f.issue(func(w http.ResponseWriter, r *http.Request, iss *jiraFakeIssue) {
		var in struct {
			Body json.RawMessage `json:"body"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || f.badDesc(in.Body) {
			jiraReply(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{"body": "Comment body is not valid"}})
			return
		}
		iss.comments = append(iss.comments, in.Body)
		jiraReply(w, http.StatusCreated, map[string]string{"id": strconv.Itoa(len(iss.comments))})
	}))
	if f.cloud {
		mux.HandleFunc("GET "+api+"/search/jql", f.search)
		mux.HandleFunc("GET "+api+"/search", func(w http.ResponseWriter, _ *http.Request) {
			jiraReply(w, http.StatusGone, map[string]any{"errorMessages": []string{"The requested API has been removed."}})
		})
	} else {
		mux.HandleFunc("GET "+api+"/search", f.search)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Host != f.host:
			http.Error(w, "unknown host", http.StatusBadGateway)
		case r.Header.Get("User-Agent") != "cerne/"+version.Version:
			http.Error(w, "error code: 1010", http.StatusForbidden)
		case !f.authorized(r):
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			mux.ServeHTTP(w, r)
		}
	})
}

func jiraReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *jiraFake) cat(status string) string {
	for _, s := range f.statuses {
		if s.name == status {
			return s.cat
		}
	}
	return ""
}

// seed adds an issue the way a third party would: in the workflow's first
// status, with body in the flavor's form.
func (f *jiraFake) seed(summary, body string, labels ...string) tracker.Ref {
	iss := &jiraFakeIssue{summary: summary, status: f.statuses[0].name, typ: "Task", labels: labels}
	iss.desc = jiraTextJSON(body)
	if f.cloud {
		iss.desc = jiraDocJSON(body)
	}
	return f.add(iss)
}

func (f *jiraFake) add(iss *jiraFakeIssue) tracker.Ref {
	iss.key = "PROJ-" + strconv.Itoa(len(f.order)+1)
	f.issues[iss.key] = iss
	f.order = append(f.order, iss.key)
	return f.ref(iss.key)
}

func (f *jiraFake) ref(key string) tracker.Ref {
	return tracker.Ref{Tracker: "jt", ID: key, URL: "https://" + f.host + f.root + "/browse/" + key}
}

func (f *jiraFake) project(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("key") != "PROJ" {
		jiraReply(w, http.StatusNotFound, map[string]any{"errorMessages": []string{"No project could be found"}})
		return
	}
	jiraReply(w, http.StatusOK, map[string]any{"key": "PROJ", "issueTypes": f.types})
}

// badDesc reports a description that is not the flavor's form: an ADF doc on
// Cloud, a string on Data Center.
func (f *jiraFake) badDesc(raw json.RawMessage) bool {
	if !f.cloud {
		var s string
		return json.Unmarshal(raw, &s) != nil
	}
	var doc struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
	}
	return json.Unmarshal(raw, &doc) != nil || doc.Type != "doc" || doc.Version != 1
}

func (f *jiraFake) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jiraReply(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	var project, typ struct {
		Key string `json:"key"`
		ID  string `json:"id"`
	}
	var summary string
	_ = json.Unmarshal(in.Fields["project"], &project)
	_ = json.Unmarshal(in.Fields["issuetype"], &typ)
	_ = json.Unmarshal(in.Fields["summary"], &summary)
	errs := map[string]string{}
	k := slices.IndexFunc(f.types, func(t jiraType) bool { return t.ID == typ.ID && !t.Subtask })
	switch {
	case project.Key != "PROJ":
		errs["project"] = "valid project is required"
	case k < 0:
		errs["issuetype"] = "valid issue type is required"
	case summary == "":
		errs["summary"] = "You must specify a summary of the issue."
	case f.badDesc(in.Fields["description"]):
		errs["description"] = "Operation value must be an Atlassian Document"
	}
	for name, msg := range f.required {
		if in.Fields[name] == nil {
			errs[name] = msg
		}
	}
	if len(errs) > 0 {
		jiraReply(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{}, "errors": errs})
		return
	}
	ref := f.add(&jiraFakeIssue{summary: summary, desc: in.Fields["description"], status: f.statuses[0].name, typ: f.types[k].Name})
	jiraReply(w, http.StatusCreated, map[string]string{"id": "1000" + strconv.Itoa(len(f.order)), "key": ref.ID, "self": "https://" + f.host + f.api() + "/issue/" + ref.ID})
}

func (f *jiraFake) issue(h func(http.ResponseWriter, *http.Request, *jiraFakeIssue)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		iss, ok := f.issues[r.PathValue("key")]
		if !ok {
			jiraReply(w, http.StatusNotFound, map[string]any{"errorMessages": []string{"Issue does not exist"}})
			return
		}
		h(w, r, iss)
	}
}

func (f *jiraFake) view(iss *jiraFakeIssue) map[string]any {
	var res any
	if iss.resolution != "" {
		res = map[string]string{"name": iss.resolution}
	}
	return map[string]any{"key": iss.key, "fields": map[string]any{
		"summary": iss.summary, "description": iss.desc, "labels": append([]string{}, iss.labels...), "resolution": res,
		"status":    map[string]any{"name": iss.status, "statusCategory": map[string]string{"key": f.cat(iss.status)}},
		"issuetype": map[string]string{"name": iss.typ},
	}}
}

// edit is PUT /issue/{key}: summary and description in fields, label add and
// remove operations in update.
func (f *jiraFake) edit(w http.ResponseWriter, r *http.Request, iss *jiraFakeIssue) {
	var in struct {
		Fields map[string]json.RawMessage     `json:"fields"`
		Update map[string][]map[string]string `json:"update"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jiraReply(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	for name, raw := range in.Fields {
		switch {
		case name == "summary" && json.Unmarshal(raw, &iss.summary) == nil && iss.summary != "":
		case name == "description" && !f.badDesc(raw):
			iss.desc = raw
		default:
			jiraReply(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{name: "Field cannot be set"}})
			return
		}
	}
	for name, ops := range in.Update {
		if name != "labels" {
			jiraReply(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{name: "Field cannot be set"}})
			return
		}
		for _, op := range ops {
			if l, ok := op["add"]; ok && !slices.Contains(iss.labels, l) {
				iss.labels = append(iss.labels, l)
			}
			if l, ok := op["remove"]; ok {
				iss.labels = slices.DeleteFunc(iss.labels, func(x string) bool { return x == l })
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// available is the transitions the workflow offers from the issue's status.
func (f *jiraFake) available(iss *jiraFakeIssue) []jiraMove {
	var out []jiraMove
	for _, m := range f.moves {
		if m.to != iss.status && (m.from == nil || slices.Contains(m.from, iss.status)) {
			out = append(out, m)
		}
	}
	return out
}

func (f *jiraFake) transitions(w http.ResponseWriter, r *http.Request, iss *jiraFakeIssue) {
	expand := strings.Contains(r.URL.Query().Get("expand"), "transitions.fields")
	var out []any
	for _, m := range f.available(iss) {
		t := map[string]any{"id": m.id, "name": m.name, "to": map[string]any{"name": m.to, "statusCategory": map[string]string{"key": f.cat(m.to)}}}
		if expand {
			fields := map[string]any{}
			if m.res != nil {
				var allowed []map[string]string
				for _, n := range m.res {
					allowed = append(allowed, map[string]string{"id": jiraResolutionIDs[n], "name": n})
				}
				fields["resolution"] = map[string]any{"required": m.required, "name": "Resolution", "allowedValues": allowed}
			}
			t["fields"] = fields
		}
		out = append(out, t)
	}
	jiraReply(w, http.StatusOK, map[string]any{"transitions": out})
}

// transition is POST /issue/{key}/transitions: a resolution only where the
// screen has the field, one of its values, and present when it is required.
// Leaving the Done category clears the resolution, as Jira's workflows do.
func (f *jiraFake) transition(w http.ResponseWriter, r *http.Request, iss *jiraFakeIssue) {
	var in struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
		Fields map[string]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jiraReply(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	avail := f.available(iss)
	k := slices.IndexFunc(avail, func(m jiraMove) bool { return m.id == in.Transition.ID })
	if k < 0 {
		jiraReply(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"Transition id '" + in.Transition.ID + "' is not valid for this issue."}})
		return
	}
	m, res := avail[k], ""
	for name, v := range in.Fields {
		i := slices.IndexFunc(m.res, func(n string) bool { return jiraResolutionIDs[n] == v.ID || n == v.Name })
		if name != "resolution" || i < 0 {
			jiraReply(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{name: "Field cannot be set. It is not on the appropriate screen, or unknown."}})
			return
		}
		res = m.res[i]
	}
	if m.required && res == "" {
		jiraReply(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{"resolution": "Resolution is required."}})
		return
	}
	iss.status = m.to
	f.moved = append(f.moved, m.id)
	switch {
	case f.cat(m.to) != "done":
		iss.resolution = ""
	case res != "":
		iss.resolution = res
	}
	w.WriteHeader(http.StatusNoContent)
}

// jiraWrites is every request after the first n that is not a GET.
func jiraWrites(reqs []jiraReq, n int) []string {
	var out []string
	for _, req := range reqs[n:] {
		if req.method != http.MethodGet {
			out = append(out, req.method+" "+req.url)
		}
	}
	return out
}

// jiraDocJSON is body as one ADF paragraph, built by hand: the emulator's
// own encoding, independent of the adapter's.
func jiraDocJSON(body string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": body}}}}})
	return b
}

func jiraTextJSON(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// jiraJQL is the only JQL the adapter writes: the spec's, plus a quoted label.
var jiraJQL = regexp.MustCompile(`^project = ([A-Z][A-Z0-9_]*) AND statusCategory != Done(?: AND labels = "((?:[^"\\]|\\.)*)")?$`)

var jiraUnescape = regexp.MustCompile(`\\(.)`)

func (f *jiraFake) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	jql := q.Get("jql")
	f.jql = append(f.jql, jql)
	m := jiraJQL.FindStringSubmatch(jql)
	if m == nil || m[1] != "PROJ" {
		jiraReply(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"unsupported JQL"}})
		return
	}
	label := jiraUnescape.ReplaceAllString(m[2], "$1")
	limit, _ := strconv.Atoi(q.Get("maxResults"))
	var out []any
	for _, k := range f.order {
		iss := f.issues[k]
		if f.cat(iss.status) == "done" || (label != "" && !slices.Contains(iss.labels, label)) || len(out) == limit {
			continue
		}
		out = append(out, map[string]any{"key": k, "fields": map[string]any{"summary": iss.summary, "description": iss.desc}})
	}
	jiraReply(w, http.StatusOK, map[string]any{"issues": out})
}

// F-0453 crit. 1: a jira record is https://<site>[/<context>]/browse/<KEY>;
// the adapter reaches the site, under its context, for that project key.
func TestJiraURLParsesSiteAndKey(t *testing.T) {
	for _, c := range []struct{ raw, wantAPI, wantJQL string }{
		{"https://acme.atlassian.net/browse/PROJ", "https://acme.atlassian.net/rest/api/3/search/jql", "project = PROJ AND statusCategory != Done"},
		{"https://jira.example/jira/browse/OPS", "https://jira.example/jira/rest/api/2/search", "project = OPS AND statusCategory != Done"},
		{"https://jira.example:8443/browse/K2_X/", "https://jira.example:8443/rest/api/2/search", "project = K2_X AND statusCategory != Done"},
	} {
		t.Run(c.raw, func(t *testing.T) {
			u, _ := url.Parse(c.raw)
			var seen []string
			reqs := installJira(t, jiraEmail, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/serverInfo") {
					jiraReply(w, http.StatusOK, map[string]string{"deploymentType": "Server"})
					return
				}
				seen = append(seen, r.URL.Query().Get("jql"))
				jiraReply(w, http.StatusOK, map[string]any{"issues": []any{}})
			}))
			if _, err := newJiraTracker(t, c.raw).ListOpen(context.Background(), ""); err != nil {
				t.Fatalf("ListOpen: %v", err)
			}
			last := (*reqs)[len(*reqs)-1]
			if got, _, _ := strings.Cut(last.url, "?"); got != c.wantAPI {
				t.Fatalf("search went to %s, want %s", got, c.wantAPI)
			}
			if len(seen) != 1 || seen[0] != c.wantJQL {
				t.Fatalf("JQL %q, want %q", seen, c.wantJQL)
			}
			for _, req := range *reqs {
				if r, _ := url.Parse(req.url); r.Host != u.Host {
					t.Fatalf("request to %s, want only %s", r.Host, u.Host)
				}
			}
		})
	}
	for _, raw := range []string{
		"http://acme.atlassian.net/browse/PROJ", "https://acme.atlassian.net", "https://acme.atlassian.net/browse/",
		"https://acme.atlassian.net/browse/PROJ-12", "https://acme.atlassian.net/browse/proj", "https://acme.atlassian.net/PROJ",
		"https://acme.atlassian.net/browse/PROJ?x=1", "https://acme.atlassian.net/browse/PROJ#top", "https://acme.atlassian.net/../browse/PROJ",
		"https://acme.atlassian.net/browse/PROJ/extra", "https://bot%40example.com:tok@acme.atlassian.net/browse/PROJ",
	} {
		if _, err := tracker.New(tracker.Config{Name: "jt", Kind: "jira", URL: raw}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(jira, %q) = %v, want errors.Is ErrTrackerURL", raw, err)
		}
	}
}

// F-0453 crit. 1: the adapter sends the login only to the record's host, so
// that host is never an IP literal or localhost (the GitHub kind refuses them
// too).
func TestJiraRefusesIPAndLocalhostHosts(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1/browse/PROJ", "https://[::1]:8080/browse/PROJ", "https://10.0.0.5/jira/browse/PROJ", "https://localhost/browse/PROJ", "https://LocalHost:8080/browse/PROJ"} {
		_, err := tracker.New(tracker.Config{Name: "jt", Kind: "jira", URL: raw})
		u, _ := url.Parse(raw)
		if !errors.Is(err, tracker.ErrTrackerURL) || !strings.Contains(err.Error(), u.Hostname()) {
			t.Errorf("New(jira, %s) = %v, want ErrTrackerURL naming host %s", raw, err, u.Hostname())
		}
	}
}

// jiraLeak reports a request that carries the secret anywhere but its
// Authorization header.
func jiraLeak(req jiraReq) bool {
	rest := req.header.Clone()
	rest.Del("Authorization")
	basic := base64.StdEncoding.EncodeToString([]byte(jiraEmail + ":" + jiraSecret))
	s := fmt.Sprint(req.url, rest, req.body)
	return strings.Contains(s, jiraSecret) || strings.Contains(s, basic)
}

// F-0453 crit. 1: the login is whatever `git credential fill` holds for the
// site: on Cloud the user is the account e-mail and the password an API token
// (HTTP Basic); on Data Center the password is a personal access token
// (Bearer), with one Basic retry after a 401 when the helper names a user.
// The secret travels only in Authorization, never in a URL, and no
// configuration holds it.
func TestJiraCredentialFromHelper(t *testing.T) {
	ctx := context.Background()
	basic := func(user string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+jiraSecret))
	}
	check := func(t *testing.T, reqs []jiraReq, want ...string) {
		t.Helper()
		if len(reqs) != len(want) {
			t.Fatalf("%d requests, want %d", len(reqs), len(want))
		}
		for i, req := range reqs {
			if got := req.header.Get("Authorization"); got != want[i] {
				t.Errorf("request %d (%s) Authorization = %q, want %q", i, req.url, got, want[i])
			}
			if ua := req.header.Get("User-Agent"); ua != "cerne/"+version.Version {
				t.Errorf("request %d User-Agent = %q, want cerne/%s", i, ua, version.Version)
			}
			if jiraLeak(req) {
				t.Errorf("request %d carries the secret outside Authorization: %s", i, req.url)
			}
		}
	}

	t.Run("cloud: e-mail and API token as Basic", func(t *testing.T) {
		f := newJiraCloudFake(t)
		var asked []string
		forge.CredentialFill = func(_ context.Context, host string) (string, string) {
			asked = append(asked, host)
			return jiraEmail, jiraSecret
		}
		if _, err := newJiraTracker(t, jiraCloud).ListOpen(ctx, ""); err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		if !slices.Equal(asked, []string{"acme.atlassian.net"}) {
			t.Fatalf("credential helper asked for %q, want once for acme.atlassian.net", asked)
		}
		check(t, *f.reqs, basic(jiraEmail))
	})

	t.Run("cloud without a user is refused before any request", func(t *testing.T) {
		f := newJiraCloudFake(t)
		forge.CredentialFill = func(context.Context, string) (string, string) { return "", jiraSecret }
		_, err := newJiraTracker(t, jiraCloud).ListOpen(ctx, "")
		if err == nil || !strings.Contains(err.Error(), "e-mail") || strings.Contains(err.Error(), jiraSecret) {
			t.Fatalf("ListOpen with no user = %v, want an error asking for the account e-mail, without the secret", err)
		}
		check(t, *f.reqs)
	})

	t.Run("no credential is refused before any request", func(t *testing.T) {
		f := newJiraDCFake(t)
		forge.CredentialFill = func(context.Context, string) (string, string) { return "", "" }
		if _, err := newJiraTracker(t, jiraDC).ListOpen(ctx, ""); err == nil || !strings.Contains(err.Error(), "git credential fill") {
			t.Fatalf("ListOpen with no credential = %v, want an error naming git credential fill", err)
		}
		check(t, *f.reqs)
	})

	t.Run("data center: personal access token as Bearer", func(t *testing.T) {
		f := newJiraDCFake(t)
		if _, err := newJiraTracker(t, jiraDC).ListOpen(ctx, ""); err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		check(t, *f.reqs, "Bearer "+jiraSecret, "Bearer "+jiraSecret)
	})

	t.Run("data center: Basic after a 401, then Basic only", func(t *testing.T) {
		f := jiraDefaults(&jiraFake{host: "jira.example", root: "/jira", deployment: "Server", basicOnly: true, user: "admin"})
		f.reqs = installJira(t, "admin", f.handler())
		if _, err := newJiraTracker(t, jiraDC).ListOpen(ctx, ""); err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		check(t, *f.reqs, "Bearer "+jiraSecret, basic("admin"), basic("admin"))
	})
}

// F-0453 crit. 1: Cloud or Data Center is decided in one place. A
// *.atlassian.net host is Cloud with no call; any other host asks
// /rest/api/2/serverInfo, where "Cloud" is Cloud and any other
// deploymentType is Data Center. A failure, a redirect or an unreadable
// answer is an error: the adapter does not guess.
func TestJiraDetectsDeployment(t *testing.T) {
	ctx := context.Background()
	t.Run("atlassian.net is cloud with no call", func(t *testing.T) {
		f := newJiraCloudFake(t)
		if _, err := newJiraTracker(t, jiraCloud).ListOpen(ctx, ""); err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		if len(*f.reqs) != 1 || !strings.Contains((*f.reqs)[0].url, "/rest/api/3/search/jql?") {
			t.Fatalf("requests %v, want one v3 search", *f.reqs)
		}
	})
	t.Run("serverInfo Cloud speaks v3 with Basic", func(t *testing.T) {
		f := jiraDefaults(&jiraFake{cloud: true, host: "jira.example", deployment: "Cloud", user: jiraEmail})
		f.reqs = installJira(t, jiraEmail, f.handler())
		if _, err := newJiraTracker(t, "https://jira.example/browse/PROJ").ListOpen(ctx, ""); err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		last := (*f.reqs)[len(*f.reqs)-1]
		if !strings.HasPrefix(last.url, "https://jira.example/rest/api/3/search/jql?") {
			t.Fatalf("after serverInfo Cloud the search went to %s, want v3 /search/jql", last.url)
		}
		if auth := last.header.Get("Authorization"); !strings.HasPrefix(auth, "Basic ") {
			t.Fatalf("Cloud search Authorization = %q, want Basic", auth)
		}
	})
	t.Run("serverInfo Server speaks v2 with Bearer", func(t *testing.T) {
		f := newJiraDCFake(t)
		if _, err := newJiraTracker(t, jiraDC).ListOpen(ctx, ""); err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		want := []string{"https://jira.example/jira/rest/api/2/serverInfo", "https://jira.example/jira/rest/api/2/search"}
		var got []string
		for _, req := range *f.reqs {
			u, _, _ := strings.Cut(req.url, "?")
			got = append(got, u)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("requests %q, want %q", got, want)
		}
	})
	for _, c := range []struct {
		name string
		h    http.HandlerFunc
	}{
		{"failure", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusInternalServerError) }},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://sso.example/login", http.StatusFound)
		}},
		{"not JSON", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>login</html>") }},
		{"no deploymentType", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"version":"9.12.0"}`) }},
	} {
		t.Run(c.name+" is an error", func(t *testing.T) {
			reqs := installJira(t, "", c.h)
			if _, err := newJiraTracker(t, jiraDC).ListOpen(ctx, ""); err == nil {
				t.Fatal("ListOpen = nil error, want the detection to fail")
			}
			if len(*reqs) != 1 {
				t.Fatalf("%d requests, want only the serverInfo one", len(*reqs))
			}
		})
	}
}

// F-0453 crit. 1: a 3xx is an answer, not a hop: one request, an error, and
// the secret neither in the error nor sent to the redirect's host.
func TestJiraNoRedirectKeepsCredential(t *testing.T) {
	reqs := installJira(t, jiraEmail, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
	}))
	_, err := newJiraTracker(t, jiraCloud).ListOpen(context.Background(), "")
	if err == nil {
		t.Fatal("ListOpen after a 302 = nil error, want an error")
	}
	if strings.Contains(err.Error(), jiraSecret) {
		t.Fatalf("error %q carries the secret", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("%d requests after a 302, want exactly 1", len(*reqs))
	}
	if u, _ := url.Parse((*reqs)[0].url); u.Host != "acme.atlassian.net" {
		t.Fatalf("request went to %s, want acme.atlassian.net", u.Host)
	}
}

// F-0453 crit. 6: the issue is a Task; a project without one gets its first
// standard type, never a subtask, and on Cloud never a type above the base
// level (an epic).
func TestJiraIssueTypeFallback(t *testing.T) {
	ctx := context.Background()
	one, zero, minus := 1, 0, -1
	for _, c := range []struct {
		name  string
		cloud bool
		types []jiraType
		want  string // "" = refused, nothing created
	}{
		{"cloud with Task", true, []jiraType{{ID: "1", Name: "Story", Level: &zero}, {ID: "2", Name: "Task", Level: &zero}}, "Task"},
		{"cloud without Task", true, []jiraType{{ID: "1", Name: "Sub-task", Subtask: true, Level: &minus}, {ID: "2", Name: "Epic", Level: &one}, {ID: "3", Name: "Story", Level: &zero}, {ID: "4", Name: "Bug", Level: &zero}}, "Story"},
		{"data center without Task", false, []jiraType{{ID: "1", Name: "Sub-task", Subtask: true}, {ID: "2", Name: "Bug"}, {ID: "3", Name: "Improvement"}}, "Bug"},
		{"only subtasks", false, []jiraType{{ID: "1", Name: "Sub-task", Subtask: true}}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, raw := newJiraDCFake(t), jiraDC
			if c.cloud {
				f, raw = newJiraCloudFake(t), jiraCloud
			}
			f.types = c.types
			r, err := newJiraTracker(t, raw).Create(ctx, tracker.Item{CardID: "F-0001", State: "candidate", Title: "Public", Summary: "Summary"})
			if c.want == "" {
				if err == nil || len(f.order) != 0 {
					t.Fatalf("Create with only subtasks = %+v, %v (%d issues); want an error and nothing created", r, err, len(f.order))
				}
				return
			}
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if got := f.issues[r.ID].typ; got != c.want {
				t.Fatalf("issue type %q, want %q", got, c.want)
			}
		})
	}
}

// F-0453: API v3 carries the body as ADF, one paragraph per block separated
// by a blank line and a hard break per line break inside it; API v2 carries
// plain text. Read back, either is the same text, so an unchanged issue is
// not rewritten, and an outside issue's rich ADF reads as text.
func TestJiraBodyADFvsText(t *testing.T) {
	ctx := context.Background()
	it := tracker.Item{CardID: "F-0001", State: "candidate", Title: "Public", Summary: "First line\nsecond line"}
	t.Run("cloud writes ADF", func(t *testing.T) {
		f := newJiraCloudFake(t)
		tr := newJiraTracker(t, jiraCloud)
		r, err := tr.Create(ctx, it)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		var got, want any
		_ = json.Unmarshal(f.issues[r.ID].desc, &got)
		_ = json.Unmarshal([]byte(`{"type":"doc","version":1,"content":[
			{"type":"paragraph","content":[{"type":"text","text":"First line"},{"type":"hardBreak"},{"type":"text","text":"second line"}]},
			{"type":"paragraph","content":[{"type":"text","text":"cerne F-0001"}]}]}`), &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("description %s, want the ADF of %q", f.issues[r.ID].desc, it.Body())
		}
		list, err := tr.ListOpen(ctx, "")
		if err != nil || len(list) != 1 || list[0].Body != it.Body() {
			t.Fatalf("ListOpen = %+v, %v; want the body read back as %q", list, err, it.Body())
		}
	})
	t.Run("data center writes text", func(t *testing.T) {
		f := newJiraDCFake(t)
		r, err := newJiraTracker(t, jiraDC).Create(ctx, it)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if got := string(f.issues[r.ID].desc); got != string(jiraTextJSON(it.Body())) {
			t.Fatalf("description %s, want the text %q", got, it.Body())
		}
	})
	t.Run("cloud reads rich ADF as text", func(t *testing.T) {
		f := newJiraCloudFake(t)
		r := f.seed("Outside", "")
		f.issues[r.ID].desc = json.RawMessage(`{"type":"doc","version":1,"content":[
			{"type":"paragraph","content":[{"type":"text","text":"Intro "},{"type":"text","text":"bold","marks":[{"type":"strong"}]}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]},
				{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]},
			{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}]}]}`)
		list, err := newJiraTracker(t, jiraCloud).ListOpen(ctx, "")
		if want := "Intro bold\n\none\ntwo\n\na\nb"; err != nil || len(list) != 1 || list[0].Body != want {
			t.Fatalf("ListOpen = %+v, %v; want body %q", list, err, want)
		}
	})
	t.Run("data center reads CRLF as LF, null as empty", func(t *testing.T) {
		f := newJiraDCFake(t)
		f.seed("Outside", "Line\r\nnext")
		f.issues[f.seed("No body", "").ID].desc = json.RawMessage("null")
		list, err := newJiraTracker(t, jiraDC).ListOpen(ctx, "")
		if err != nil || len(list) != 2 || list[0].Body != "Line\nnext" || list[1].Body != "" {
			t.Fatalf("ListOpen = %+v, %v; want bodies %q and empty", list, err, "Line\nnext")
		}
	})
}

// F-0453 (risk: a required field): a 400 names the fields Jira refused, and
// only those whose name is a plain field id: never Jira's message, never a
// name that could forge a line.
func TestJiraErrorNamesOnlyTheField(t *testing.T) {
	f := newJiraCloudFake(t)
	f.required = map[string]string{"customfield_10010": "Team is required.", "x\nCloses: #1": "forged"}
	_, err := newJiraTracker(t, jiraCloud).Create(context.Background(), tracker.Item{CardID: "F-0001", State: "candidate", Title: "Public", Summary: "Summary"})
	if want := "jira acme.atlassian.net: HTTP 400 (fields: customfield_10010)"; err == nil || err.Error() != want {
		t.Fatalf("Create = %v, want %q", err, want)
	}
}

// F-0453 crit. 4: the sync lists by the spec's JQL, project = <KEY> AND
// statusCategory != Done, plus AND labels = "<label>" when the record has
// one, quoted with its quote and backslash escaped; on Cloud through
// /rest/api/3/search/jql, on Data Center through /rest/api/2/search. A label
// with a control character is refused before any request.
func TestJiraSyncJQL(t *testing.T) {
	ctx := context.Background()
	const base = "project = PROJ AND statusCategory != Done"
	for _, c := range []struct {
		name   string
		cloud  bool
		label  string
		jql    string
		search string
	}{
		{"cloud, no label", true, "", base, "https://acme.atlassian.net/rest/api/3/search/jql"},
		{"cloud, label", true, "public", base + ` AND labels = "public"`, "https://acme.atlassian.net/rest/api/3/search/jql"},
		{"data center, no label", false, "", base, "https://jira.example/jira/rest/api/2/search"},
		{"data center, label with quote and backslash", false, `a"b\c`, base + ` AND labels = "a\"b\\c"`, "https://jira.example/jira/rest/api/2/search"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, raw := newJiraDCFake(t), jiraDC
			if c.cloud {
				f, raw = newJiraCloudFake(t), jiraCloud
			}
			labeled := f.seed("Labeled", "Body", "public", `a"b\c`)
			plain := f.seed("Plain", "Body")
			f.issues[f.seed("Finished", "Body", "public", `a"b\c`).ID].status = "Done"
			list, err := newJiraTracker(t, raw).ListOpen(ctx, c.label)
			if err != nil {
				t.Fatalf("ListOpen(%q): %v", c.label, err)
			}
			if !slices.Equal(f.jql, []string{c.jql}) {
				t.Fatalf("JQL %q, want %q", f.jql, c.jql)
			}
			want := []string{labeled.ID, plain.ID}
			if c.label != "" {
				want = want[:1]
			}
			var got []string
			for _, rm := range list {
				got = append(got, rm.ID)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("listed %v, want %v", got, want)
			}
			last := (*f.reqs)[len(*f.reqs)-1]
			u, _ := url.Parse(last.url)
			if s := u.Scheme + "://" + u.Host + u.Path; s != c.search || u.Query().Get("maxResults") != "100" {
				t.Fatalf("searched %s, want %s with maxResults=100", last.url, c.search)
			}
		})
	}
	t.Run("a label with a control character is refused before any request", func(t *testing.T) {
		f := newJiraCloudFake(t)
		if _, err := newJiraTracker(t, jiraCloud).ListOpen(ctx, "public\nOR project = OTHER"); err == nil || len(*f.reqs) != 0 {
			t.Fatalf("ListOpen(control label) = %v after %d requests, want an error and none", err, len(*f.reqs))
		}
	})
}

// jiraFlavors runs fn on an emulated Cloud and an emulated Data Center.
func jiraFlavors(t *testing.T, fn func(t *testing.T, f *jiraFake, tr tracker.IssueTracker)) {
	t.Run("cloud", func(t *testing.T) { f := newJiraCloudFake(t); fn(t, f, newJiraTracker(t, jiraCloud)) })
	t.Run("datacenter", func(t *testing.T) { f := newJiraDCFake(t); fn(t, f, newJiraTracker(t, jiraDC)) })
}

// F-0453 crit. 2: Create leaves the issue in the status category of the
// card's state: To Do for candidate and approved, In Progress for
// in_development and implemented, Done for done, rejected and superseded.
func TestJiraCreateLeavesIssueInState(t *testing.T) {
	want := map[domain.FeatureState]string{
		domain.StateCandidate: "new", domain.StateApproved: "new", domain.StateInDevelopment: "indeterminate", domain.StateImplemented: "indeterminate",
		domain.StateDone: "done", domain.StateRejected: "done", domain.StateSuperseded: "done",
	}
	jiraFlavors(t, func(t *testing.T, f *jiraFake, tr tracker.IssueTracker) {
		for s, cat := range want {
			r, err := tr.Create(context.Background(), tracker.Item{CardID: "F-0001", State: s, Title: "Public", Summary: "Summary"})
			if err != nil {
				t.Fatalf("Create(%s): %v", s, err)
			}
			if got := f.cat(f.issues[r.ID].status); got != cat {
				t.Errorf("Create(%s) left %s in %s (%s), want category %s", s, r.ID, f.issues[r.ID].status, got, cat)
			}
		}
	})
}

// F-0453 crit. 2: the transition is the first the issue offers whose target
// status is in the card state's category, whatever the workflow names its
// statuses and transitions; an issue already in that category is not moved.
func TestJiraTransitionByCategory(t *testing.T) {
	ctx := context.Background()
	jiraFlavors(t, func(t *testing.T, f *jiraFake, tr tracker.IssueTracker) {
		f.statuses = []jiraStatus{{"Triagem", "new"}, {"Fila", "new"}, {"Fazendo", "indeterminate"}, {"Revisão", "indeterminate"}, {"Arquivado", "done"}, {"Entregue", "done"}}
		f.moves = []jiraMove{
			{id: "101", name: "Arquivar", to: "Arquivado", from: []string{"Triagem", "Fila"}},
			{id: "102", name: "Puxar", to: "Fazendo", from: []string{"Triagem", "Fila"}},
			{id: "103", name: "Pedir revisão", to: "Revisão", from: []string{"Triagem", "Fazendo"}},
			{id: "104", name: "Despachar", to: "Entregue", from: []string{"Fazendo", "Revisão"}},
			{id: "105", name: "Devolver", to: "Fila", from: []string{"Arquivado", "Entregue", "Fazendo", "Revisão"}},
		}
		r := f.seed("Outside", "Body")
		for _, step := range []struct {
			state  domain.FeatureState
			status string
		}{
			{domain.StateInDevelopment, "Fazendo"}, {domain.StateImplemented, "Fazendo"}, {domain.StateDone, "Entregue"},
			{domain.StateApproved, "Fila"}, {domain.StateCandidate, "Fila"},
		} {
			before := len(*f.reqs)
			if err := tr.Update(ctx, r, tracker.Item{CardID: "F-0001", State: step.state}); err != nil {
				t.Fatalf("Update(%s): %v", step.state, err)
			}
			if got := f.issues[r.ID].status; got != step.status {
				t.Fatalf("after Update(%s) the issue is in %s, want %s", step.state, got, step.status)
			}
			if step.state == domain.StateImplemented || step.state == domain.StateCandidate {
				if w := jiraWrites(*f.reqs, before); len(w) != 0 || strings.Contains(fmt.Sprint((*f.reqs)[before:]), "/transitions") {
					t.Fatalf("Update(%s) on an issue already in the category sent %v", step.state, (*f.reqs)[before:])
				}
			}
		}
		if want := []string{"102", "104", "105"}; !slices.Equal(f.moved, want) {
			t.Fatalf("transitions applied %v, want %v", f.moved, want)
		}
	})
}

// F-0453 crit. 2: when no transition leads to the category, the issue keeps
// its status, gets the cerne:<state> label in place of any earlier cerne:*
// one, and Update returns the no-transition error the core prints as a
// warning without failing the cerne transition. A later state the workflow
// reaches drops the label again.
func TestJiraNoMatchingTransitionWarns(t *testing.T) {
	ctx := context.Background()
	jiraFlavors(t, func(t *testing.T, f *jiraFake, tr tracker.IssueTracker) {
		f.moves = []jiraMove{
			{id: "11", name: "Start", to: "In Progress", from: []string{"To Do"}},
			{id: "21", name: "Finish", to: "Done", from: []string{"In Progress"}},
		}
		r := f.seed("Outside", "Body", "keep", "cerne:approved")
		err := tr.Update(ctx, r, tracker.Item{CardID: "F-0001", State: domain.StateDone})
		if err == nil || !strings.Contains(err.Error(), "no transition to the status category done") || !strings.Contains(err.Error(), r.ID) {
			t.Fatalf("Update(done) with no transition to Done = %v, want the no-transition error naming %s", err, r.ID)
		}
		iss := f.issues[r.ID]
		if iss.status != "To Do" || !slices.Equal(iss.labels, []string{"keep", "cerne:done"}) {
			t.Fatalf("issue in %s with labels %v, want To Do with [keep cerne:done]", iss.status, iss.labels)
		}
		if err := tr.Update(ctx, r, tracker.Item{CardID: "F-0001", State: domain.StateInDevelopment}); err != nil {
			t.Fatalf("Update(in_development): %v", err)
		}
		if iss.status != "In Progress" || !slices.Equal(iss.labels, []string{"keep"}) {
			t.Fatalf("issue in %s with labels %v, want In Progress with [keep]", iss.status, iss.labels)
		}
	})
}

// F-0453 crit. 3: a rejected or superseded card's issue goes to Done with
// the resolution Won't Do where the transition's screen offers it, and
// otherwise with Done (or, when the field is required and has neither, its
// first value) plus the cerne:<state> label that tells it from a delivered
// issue; a screen with no resolution field gets the label too. A done card
// resolves as Done, with no label. A second update writes nothing, and
// reopening drops the label.
func TestJiraRejectResolution(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name      string
		res       []string
		required  bool
		state     domain.FeatureState
		wantRes   string
		wantLabel []string
	}{
		{"rejected with Won't Do", []string{"Done", "Won't Do", "Duplicate"}, false, domain.StateRejected, "Won't Do", nil},
		{"superseded with Won't Do", []string{"Done", "Won't Do"}, false, domain.StateSuperseded, "Won't Do", nil},
		{"rejected without Won't Do", []string{"Done", "Duplicate"}, false, domain.StateRejected, "Done", []string{"cerne:rejected"}},
		{"superseded without Won't Do", []string{"Done", "Duplicate"}, false, domain.StateSuperseded, "Done", []string{"cerne:superseded"}},
		{"rejected, screen without resolution", nil, false, domain.StateRejected, "", []string{"cerne:rejected"}},
		{"rejected, required resolution with neither", []string{"Fixed", "Duplicate"}, true, domain.StateRejected, "Fixed", []string{"cerne:rejected"}},
		{"done resolves Done", []string{"Won't Do", "Done"}, false, domain.StateDone, "Done", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			jiraFlavors(t, func(t *testing.T, f *jiraFake, tr tracker.IssueTracker) {
				f.moves[2].res, f.moves[2].required = c.res, c.required
				it := tracker.Item{CardID: "F-0001", State: domain.StateApproved, Title: "Public", Summary: "Summary"}
				r, err := tr.Create(ctx, it)
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				it.State = c.state
				if err := tr.Update(ctx, r, it); err != nil {
					t.Fatalf("Update(%s): %v", c.state, err)
				}
				iss := f.issues[r.ID]
				if iss.status != "Done" || iss.resolution != c.wantRes || !slices.Equal(iss.labels, c.wantLabel) {
					t.Fatalf("issue %s, resolution %q, labels %v; want Done, %q, %v", iss.status, iss.resolution, iss.labels, c.wantRes, c.wantLabel)
				}
				before := len(*f.reqs)
				if err := tr.Update(ctx, r, it); err != nil {
					t.Fatalf("Update(%s) again: %v", c.state, err)
				}
				if w := jiraWrites(*f.reqs, before); len(w) != 0 {
					t.Fatalf("a second Update(%s) wrote %v", c.state, w)
				}
				it.State = domain.StateInDevelopment
				if err := tr.Update(ctx, r, it); err != nil {
					t.Fatalf("Update(in_development): %v", err)
				}
				if iss.status != "In Progress" || iss.resolution != "" || len(iss.labels) != 0 {
					t.Fatalf("reopened issue %s, resolution %q, labels %v; want In Progress, none, none", iss.status, iss.resolution, iss.labels)
				}
			})
		})
	}
}

// F-0453: Update reads the issue and writes only what differs: nothing for
// an unchanged card, the summary alone for a new title, the description
// alone for a new summary, and nothing of an outside issue's text.
func TestJiraUpdateWritesOnlyDiffs(t *testing.T) {
	ctx := context.Background()
	jiraFlavors(t, func(t *testing.T, f *jiraFake, tr tracker.IssueTracker) {
		it := tracker.Item{CardID: "F-0001", State: domain.StateApproved, Title: "Public", Summary: "Summary"}
		r, err := tr.Create(ctx, it)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		edits := func(it tracker.Item) []string {
			t.Helper()
			before := len(*f.reqs)
			if err := tr.Update(ctx, r, it); err != nil {
				t.Fatalf("Update: %v", err)
			}
			var keys []string
			for _, req := range (*f.reqs)[before:] {
				if req.method != http.MethodGet {
					var body struct {
						Fields map[string]json.RawMessage `json:"fields"`
						Update map[string]json.RawMessage `json:"update"`
					}
					_ = json.Unmarshal([]byte(req.body), &body)
					for k := range body.Fields {
						keys = append(keys, req.method+" fields."+k)
					}
					for k := range body.Update {
						keys = append(keys, req.method+" update."+k)
					}
				}
			}
			return keys
		}
		if got := edits(it); len(got) != 0 {
			t.Fatalf("an unchanged update wrote %v", got)
		}
		it.Title = "New title"
		if got := edits(it); !slices.Equal(got, []string{"PUT fields.summary"}) || f.issues[r.ID].summary != "New title" {
			t.Fatalf("a new title wrote %v, want only the summary", got)
		}
		it.Summary = "New summary"
		if got := edits(it); !slices.Equal(got, []string{"PUT fields.description"}) {
			t.Fatalf("a new summary wrote %v, want only the description", got)
		}
		list, err := tr.ListOpen(ctx, "")
		if err != nil || len(list) != 1 || list[0].Body != it.Body() {
			t.Fatalf("ListOpen = %+v, %v; want the body %q", list, err, it.Body())
		}
		outside := f.seed("Outside title", "Outside body")
		before := len(*f.reqs)
		if err := tr.Update(ctx, outside, tracker.Item{CardID: "F-0002", State: domain.StateCandidate}); err != nil {
			t.Fatalf("Update(outside): %v", err)
		}
		if w := jiraWrites(*f.reqs, before); len(w) != 0 || f.issues[outside.ID].summary != "Outside title" {
			t.Fatalf("an outside issue's update wrote %v", w)
		}
	})
}

// F-0453: the superseded card's issue gets "Superseded by <successor>" once,
// from the call that takes it to Done; a later sync does not repeat it.
func TestJiraSupersededCommentOnce(t *testing.T) {
	ctx := context.Background()
	jiraFlavors(t, func(t *testing.T, f *jiraFake, tr tracker.IssueTracker) {
		it := tracker.Item{CardID: "F-0001", State: domain.StateInDevelopment, Title: "Public", Summary: "Summary"}
		r, err := tr.Create(ctx, it)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		next := f.ref("PROJ-9")
		it.State, it.Successor = domain.StateSuperseded, &next
		for range 2 {
			if err := tr.Update(ctx, r, it); err != nil {
				t.Fatalf("Update(superseded): %v", err)
			}
		}
		want := jiraTextJSON("Superseded by " + next.URL)
		if f.cloud {
			want = jiraDocJSON("Superseded by " + next.URL)
		}
		iss := f.issues[r.ID]
		if len(iss.comments) != 1 || !jiraSameJSON(iss.comments[0], want) {
			t.Fatalf("comments %s, want once %s", iss.comments, want)
		}
	})
}

func jiraSameJSON(a, b json.RawMessage) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// F-0453 crit. 5: the trailer is "Refs: <KEY>-<n>", whatever forge hosts the
// code: Jira links a commit by the key in its message. Jira closes nothing
// on a merge, so closing (mirror.close_on = "merge") changes nothing. It is
// pure: no request.
func TestJiraCommitRef(t *testing.T) {
	reqs := installJira(t, jiraEmail, http.NotFoundHandler())
	tr := newJiraTracker(t, jiraCloud)
	r := tracker.Ref{Tracker: "jt", ID: "PROJ-123", URL: "https://acme.atlassian.net/browse/PROJ-123"}
	for _, commitURL := range []string{"https://github.com/o/r/commit/{sha}", "https://gitlab.com/o/r/-/commit/{sha}", ""} {
		for _, closing := range []bool{false, true} {
			if got := tr.CommitRef(r, commitURL, closing); got != "Refs: PROJ-123" {
				t.Errorf("CommitRef(%q, closing=%v) = %q, want %q", commitURL, closing, got, "Refs: PROJ-123")
			}
		}
	}
	for _, id := range []string{"OTHER-123", "PROJ-", "PROJ-12a", "123"} {
		if got := tr.CommitRef(tracker.Ref{Tracker: "jt", ID: id, URL: "https://acme.atlassian.net/browse/" + id}, "", false); got != "" {
			t.Errorf("CommitRef(id %q) = %q, want none", id, got)
		}
	}
	if len(*reqs) != 0 {
		t.Fatalf("CommitRef sent %d requests, want none", len(*reqs))
	}
}

// F-0453 crit. 5: Owns accepts only https://<site>[/<context>]/browse/<KEY>-<n>
// of the configured site and project, whose id is that key; it refuses
// another host, another project, another context and a malformed id.
func TestJiraOwns(t *testing.T) {
	reqs := installJira(t, jiraEmail, http.NotFoundHandler())
	cloud, dc := newJiraTracker(t, jiraCloud), newJiraTracker(t, jiraDC)
	ref := func(id, u string) tracker.Ref { return tracker.Ref{Tracker: "jt", ID: id, URL: u} }
	for _, c := range []struct {
		name string
		tr   tracker.IssueTracker
		r    tracker.Ref
		want bool
	}{
		{"cloud issue", cloud, ref("PROJ-12", "https://acme.atlassian.net/browse/PROJ-12"), true},
		{"host in other case", cloud, ref("PROJ-12", "https://ACME.atlassian.net/browse/PROJ-12"), true},
		{"data center issue under its context", dc, ref("PROJ-3", "https://jira.example/jira/browse/PROJ-3"), true},
		{"other host", cloud, ref("PROJ-12", "https://evil.atlassian.net/browse/PROJ-12"), false},
		{"other project", cloud, ref("OTHER-12", "https://acme.atlassian.net/browse/OTHER-12"), false},
		{"id and URL disagree", cloud, ref("PROJ-12", "https://acme.atlassian.net/browse/PROJ-13"), false},
		{"malformed id", cloud, ref("PROJ-x", "https://acme.atlassian.net/browse/PROJ-x"), false},
		{"bare number", cloud, ref("12", "https://acme.atlassian.net/browse/12"), false},
		{"missing context", dc, ref("PROJ-3", "https://jira.example/browse/PROJ-3"), false},
		{"not browse", cloud, ref("PROJ-12", "https://acme.atlassian.net/issues/PROJ-12"), false},
		{"http", cloud, ref("PROJ-12", "http://acme.atlassian.net/browse/PROJ-12"), false},
	} {
		if got := c.tr.Owns(c.r); got != c.want {
			t.Errorf("%s: Owns(%+v) = %v, want %v", c.name, c.r, got, c.want)
		}
	}
	if len(*reqs) != 0 {
		t.Fatalf("Owns sent %d requests, want none", len(*reqs))
	}
}

// F-0453 crit. 7: the Jira adapter passes the contract suite the fake
// passes, against an emulated Cloud (v3, ADF, Basic) and an emulated Data
// Center (v2 under a context path, text, Bearer, serverInfo). Jira closes
// nothing on a merge: NoReopen is not checked.
func TestJiraContract(t *testing.T) {
	for _, flavor := range []struct {
		name string
		fake func(*testing.T) *jiraFake
		url  string
	}{{"cloud", newJiraCloudFake, jiraCloud}, {"datacenter", newJiraDCFake, jiraDC}} {
		t.Run(flavor.name, func(t *testing.T) {
			var f *jiraFake
			trackertest.RunContract(t, trackertest.Harness{
				New: func(t *testing.T) tracker.IssueTracker {
					f = flavor.fake(t)
					return newJiraTracker(t, flavor.url)
				},
				Open: func(_ *testing.T, title, body, label string) tracker.Ref {
					if label == "" {
						return f.seed(title, body)
					}
					return f.seed(title, body, label)
				},
			})
		})
	}
}

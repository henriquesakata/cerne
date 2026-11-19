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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
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
	issues     map[string]*jiraFakeIssue
	order      []string // keys, in creation order
	jql        []string // every JQL searched
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

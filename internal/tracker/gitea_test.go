package tracker_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
// credential helper: a test that forgets to install a fake transport fails
// instead of writing to a real Gitea (F-0452).
func init() {
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("test guard: %s %s must not reach the network (F-0452)", req.Method, req.URL.Host)
	}
	forge.CredentialFill = func(context.Context, string) (string, string) { return "", "" }
}

const giteaFixture = "fixture-f0452"

// giteaReq is one request the adapter sent, as the transport saw it.
type giteaReq struct {
	method, url string
	header      http.Header
	body        string
}

// installGitea routes forge.HTTPDo to h in process, recording each request,
// and makes the credential helper answer user and giteaFixture.
func installGitea(t *testing.T, user string, h http.Handler) *[]giteaReq {
	t.Helper()
	var reqs []giteaReq
	prevDo, prevFill := forge.HTTPDo, forge.CredentialFill
	t.Cleanup(func() { forge.HTTPDo, forge.CredentialFill = prevDo, prevFill })
	forge.CredentialFill = func(context.Context, string) (string, string) { return user, giteaFixture }
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		reqs = append(reqs, giteaReq{req.Method, req.URL.String(), req.Header.Clone(), string(body)})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Result(), nil
	}
	return &reqs
}

// newGiteaTracker builds the adapter named "gt" for the repository at repoURL.
func newGiteaTracker(t *testing.T, repoURL string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: repoURL})
	if err != nil {
		t.Fatalf("New(gitea, %s): %v", repoURL, err)
	}
	return tr
}

// F-0452 crit. 1: a gitea record is accepted over https only, with a URL
// that names owner/repo.
func TestGiteaRequiresHTTPS(t *testing.T) {
	for _, raw := range []string{
		"http://git.example/o/r", "git.example/o/r",
		"https://git.example", "https://git.example/o", "https://git.example/o/r/extra", "https://git.example/o/r?x=1", "https://git.example/../r",
	} {
		if _, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: raw}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(gitea, %q) = %v, want errors.Is ErrTrackerURL", raw, err)
		}
	}
	newGiteaTracker(t, "https://git.example/o/r")
	newGiteaTracker(t, "https://git.example:3000/o/r/")
}

// F-0452 crit. 1: the login goes to the record's host, so that host is never
// an IP literal or localhost, which name a service on this machine or network
// rather than a Gitea (the GitHub kind refuses them too).
func TestGiteaRefusesIPAndLocalhostHosts(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:3000/o/r", "https://[::1]/o/r", "https://10.0.0.5/o/r", "https://localhost/o/r", "https://LocalHost:3300/o/r"} {
		_, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: raw})
		u, _ := url.Parse(raw)
		if !errors.Is(err, tracker.ErrTrackerURL) || !strings.Contains(err.Error(), u.Hostname()) {
			t.Errorf("New(gitea, %s) = %v, want ErrTrackerURL naming host %s", raw, err, u.Hostname())
		}
	}
}

// F-0452 crit. 1: a 3xx is an answer, not a hop: one request, an error, and
// the secret neither in the error nor sent to the redirect's host.
func TestGiteaNoRedirectKeepsCredential(t *testing.T) {
	reqs := installGitea(t, "root", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
	}))
	_, err := newGiteaTracker(t, "https://git.example/o/r").ListOpen(context.Background(), "")
	if err == nil {
		t.Fatal("ListOpen after a 302 = nil error, want an error")
	}
	if strings.Contains(err.Error(), giteaFixture) {
		t.Fatalf("error %q carries the secret", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("%d requests after a 302, want exactly 1: %v", len(*reqs), *reqs)
	}
	if u, _ := url.Parse((*reqs)[0].url); u.Host != "git.example" {
		t.Fatalf("request went to %s, want git.example", u.Host)
	}
}

// F-0452 crit. 5: every request carries cerne/<version>, the User-Agent a
// Cloudflare in front of the host demands (it answers Go's default with 403
// "error code: 1010"). The secret travels only in Authorization: as a token,
// or as HTTP Basic after a 401 when the credential helper names a user.
func TestGiteaUserAgent(t *testing.T) {
	cloudflare := func(auth func(*http.Request) bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Header.Get("User-Agent") != "cerne/"+version.Version:
				http.Error(w, "error code: 1010", http.StatusForbidden)
			case !auth(r):
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			default:
				fmt.Fprint(w, "[]")
			}
		})
	}
	for _, c := range []struct {
		name, user string
		auth       func(*http.Request) bool
		want       []string // the Authorization of each request, in order
	}{
		{"token", "", func(r *http.Request) bool { return r.Header.Get("Authorization") == "token "+giteaFixture }, []string{"token " + giteaFixture}},
		{"basic after 401", "root", func(r *http.Request) bool { u, p, ok := r.BasicAuth(); return ok && u == "root" && p == giteaFixture },
			[]string{"token " + giteaFixture, "Basic cm9vdDpmaXh0dXJlLWYwNDUy"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			reqs := installGitea(t, c.user, cloudflare(c.auth))
			if _, err := newGiteaTracker(t, "https://git.example/o/r").ListOpen(context.Background(), "cerne:x"); err != nil {
				t.Fatalf("ListOpen: %v", err)
			}
			if len(*reqs) != len(c.want) {
				t.Fatalf("%d requests, want %d", len(*reqs), len(c.want))
			}
			for i, req := range *reqs {
				if got := req.header.Get("Authorization"); got != c.want[i] {
					t.Errorf("request %d Authorization = %q, want %q", i, got, c.want[i])
				}
				if ua := req.header.Get("User-Agent"); ua != "cerne/"+version.Version {
					t.Errorf("request %d User-Agent = %q, want cerne/%s", i, ua, version.Version)
				}
				rest := req.header.Clone()
				rest.Del("Authorization")
				if leak := fmt.Sprint(req.url, rest, req.body); strings.Contains(leak, giteaFixture) || strings.Contains(leak, "cm9vdDpmaXh0dXJlLWYwNDUy") {
					t.Errorf("request %d carries the secret outside Authorization: %s", i, leak)
				}
			}
		})
	}
}

// gtLabel is a repository label, as the Gitea API shows it.
type gtLabel struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// gtIssue is one issue (or PR) the emulator holds; Labels are label ids.
type gtIssue struct {
	Title, Body, State string
	Labels             []int64
	PR                 bool
	Comments           []string
}

// fakeGitea emulates, behind forge.HTTPDo, the slice of the Gitea API v1 the
// adapter calls, for repository o/r on git.example. Labels travel by id, as
// in Gitea. Like the Cloudflare in front of a real host it answers a request
// without the cerne/ User-Agent with 403 "error code: 1010", and one without
// the token with 401.
type fakeGitea struct {
	issues map[int64]*gtIssue
	labels []gtLabel
	reqs   *[]giteaReq
	// noPaging makes the label list ignore ?page=, as a proxy that drops it.
	noPaging bool
}

const gtBase = "/api/v1/repos/o/r"

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func newFakeGitea(t *testing.T) *fakeGitea {
	t.Helper()
	g := &fakeGitea{issues: map[int64]*gtIssue{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+gtBase+"/labels", func(w http.ResponseWriter, r *http.Request) {
		if g.noPaging {
			reply(w, http.StatusOK, g.labels[:min(50, len(g.labels))])
			return
		}
		reply(w, http.StatusOK, page(r, g.labels))
	})
	mux.HandleFunc("POST "+gtBase+"/labels", g.createLabel)
	mux.HandleFunc("GET "+gtBase+"/issues", g.listIssues)
	mux.HandleFunc("POST "+gtBase+"/issues", g.createIssue)
	mux.HandleFunc("GET "+gtBase+"/issues/{n}", g.issue(func(w http.ResponseWriter, _ *http.Request, n int64) { reply(w, http.StatusOK, g.view(n)) }))
	mux.HandleFunc("PATCH "+gtBase+"/issues/{n}", g.issue(g.editIssue))
	mux.HandleFunc("POST "+gtBase+"/issues/{n}/labels", g.issue(g.addLabels))
	mux.HandleFunc("DELETE "+gtBase+"/issues/{n}/labels/{id}", g.issue(g.removeLabel))
	mux.HandleFunc("POST "+gtBase+"/issues/{n}/comments", g.issue(g.comment))
	g.reqs = installGitea(t, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Host != "git.example":
			http.Error(w, "unknown host", http.StatusBadGateway)
		case r.Header.Get("User-Agent") != "cerne/"+version.Version:
			http.Error(w, "error code: 1010", http.StatusForbidden)
		case r.Header.Get("Authorization") != "token "+giteaFixture:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			mux.ServeHTTP(w, r)
		}
	}))
	return g
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return false
	}
	return true
}

// page cuts list to r's ?page= and ?limit=, with Gitea's defaults: 30 per
// page, at most 50.
func page[T any](r *http.Request, list []T) []T {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 30
	}
	limit = min(limit, 50)
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	lo := min((max(p, 1)-1)*limit, len(list))
	return list[lo:min(lo+limit, len(list))]
}

func (g *fakeGitea) label(id int64) (gtLabel, bool) {
	i := slices.IndexFunc(g.labels, func(l gtLabel) bool { return l.ID == id })
	if i < 0 {
		return gtLabel{}, false
	}
	return g.labels[i], true
}

func (g *fakeGitea) labelNamed(name string) (gtLabel, bool) {
	i := slices.IndexFunc(g.labels, func(l gtLabel) bool { return l.Name == name })
	if i < 0 {
		return gtLabel{}, false
	}
	return g.labels[i], true
}

func (g *fakeGitea) addLabel(name, color string) int64 {
	id := int64(len(g.labels) + 1)
	g.labels = append(g.labels, gtLabel{ID: id, Name: name, Color: color})
	return id
}

// seed adds an issue with labels by name, creating the ones the repository lacks.
func (g *fakeGitea) seed(iss *gtIssue, labels ...string) tracker.Ref {
	for _, name := range labels {
		l, ok := g.labelNamed(name)
		if !ok {
			l.ID = g.addLabel(name, "#ededed")
		}
		iss.Labels = append(iss.Labels, l.ID)
	}
	if iss.State == "" {
		iss.State = "open"
	}
	n := int64(len(g.issues) + 1)
	g.issues[n] = iss
	return g.ref(n)
}

func (g *fakeGitea) url(n int64) string {
	return "https://git.example/o/r/issues/" + strconv.FormatInt(n, 10)
}

func (g *fakeGitea) ref(n int64) tracker.Ref {
	return tracker.Ref{Tracker: "gt", ID: strconv.FormatInt(n, 10), URL: g.url(n)}
}

func (g *fakeGitea) view(n int64) map[string]any {
	iss := g.issues[n]
	labels := []gtLabel{}
	for _, id := range iss.Labels {
		l, _ := g.label(id)
		labels = append(labels, l)
	}
	v := map[string]any{"id": 1000 + n, "number": n, "html_url": g.url(n), "title": iss.Title, "body": iss.Body,
		"state": iss.State, "labels": labels, "pull_request": nil}
	if iss.PR {
		v["html_url"] = "https://git.example/o/r/pulls/" + strconv.FormatInt(n, 10)
		v["pull_request"] = map[string]any{"merged": false}
	}
	return v
}

// names is the sorted label names of issue n.
func (g *fakeGitea) names(n int64) []string {
	var out []string
	for _, id := range g.issues[n].Labels {
		l, _ := g.label(id)
		out = append(out, l.Name)
	}
	return slices.Sorted(slices.Values(out))
}

// count is how many requests had method on the repository path p.
func (g *fakeGitea) count(method, p string) int {
	n := 0
	for _, r := range *g.reqs {
		if u, _ := url.Parse(r.url); r.method == method && u.Path == gtBase+p {
			n++
		}
	}
	return n
}

func (g *fakeGitea) createLabel(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Color string }
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Name == "" || !hexColor.MatchString(in.Color) {
		http.Error(w, "invalid label", http.StatusUnprocessableEntity)
		return
	}
	l, _ := g.label(g.addLabel(in.Name, in.Color))
	reply(w, http.StatusCreated, l)
}

func (g *fakeGitea) listIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// Like Gitea, a label name the repository lacks drops out of the filter,
	// and an empty filter keeps every issue.
	var want []int64
	for name := range strings.SplitSeq(q.Get("labels"), ",") {
		if l, ok := g.labelNamed(name); ok {
			want = append(want, l.ID)
		}
	}
	list := []map[string]any{}
	for _, n := range slices.Sorted(maps.Keys(g.issues)) {
		iss := g.issues[n]
		if (q.Get("state") == "" || q.Get("state") == iss.State) && (q.Get("type") != "issues" || !iss.PR) &&
			!slices.ContainsFunc(want, func(id int64) bool { return !slices.Contains(iss.Labels, id) }) {
			list = append(list, g.view(n))
		}
	}
	reply(w, http.StatusOK, page(r, list))
}

func (g *fakeGitea) createIssue(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title, Body string
		Labels      []int64
	}
	if !decodeBody(w, r, &in) {
		return
	}
	for _, id := range in.Labels {
		if _, ok := g.label(id); !ok {
			http.Error(w, "label does not exist", http.StatusUnprocessableEntity)
			return
		}
	}
	n, _ := strconv.ParseInt(g.seed(&gtIssue{Title: in.Title, Body: in.Body, Labels: in.Labels}).ID, 10, 64)
	reply(w, http.StatusCreated, g.view(n))
}

// issue resolves the {n} of the path for fn, or answers 404.
func (g *fakeGitea) issue(fn func(http.ResponseWriter, *http.Request, int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.ParseInt(r.PathValue("n"), 10, 64)
		if g.issues[n] == nil || err != nil {
			http.Error(w, "issue not found", http.StatusNotFound)
			return
		}
		fn(w, r, n)
	}
}

func (g *fakeGitea) editIssue(w http.ResponseWriter, r *http.Request, n int64) {
	var in struct{ Title, Body, State *string }
	if !decodeBody(w, r, &in) {
		return
	}
	iss := g.issues[n]
	if in.State != nil && *in.State != "open" && *in.State != "closed" {
		http.Error(w, "bad state", http.StatusUnprocessableEntity)
		return
	}
	for dst, src := range map[*string]*string{&iss.Title: in.Title, &iss.Body: in.Body, &iss.State: in.State} {
		if src != nil {
			*dst = *src
		}
	}
	reply(w, http.StatusCreated, g.view(n))
}

func (g *fakeGitea) addLabels(w http.ResponseWriter, r *http.Request, n int64) {
	var in struct{ Labels []int64 }
	if !decodeBody(w, r, &in) {
		return
	}
	iss := g.issues[n]
	for _, id := range in.Labels {
		if _, ok := g.label(id); !ok {
			http.Error(w, "label does not exist", http.StatusUnprocessableEntity)
			return
		}
		if !slices.Contains(iss.Labels, id) {
			iss.Labels = append(iss.Labels, id)
		}
	}
	reply(w, http.StatusOK, g.view(n)["labels"])
}

func (g *fakeGitea) removeLabel(w http.ResponseWriter, r *http.Request, n int64) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	iss := g.issues[n]
	if !slices.Contains(iss.Labels, id) {
		http.Error(w, "label not on issue", http.StatusUnprocessableEntity)
		return
	}
	iss.Labels = slices.DeleteFunc(iss.Labels, func(l int64) bool { return l == id })
	w.WriteHeader(http.StatusNoContent)
}

func (g *fakeGitea) comment(w http.ResponseWriter, r *http.Request, n int64) {
	var in struct{ Body string }
	if !decodeBody(w, r, &in) {
		return
	}
	g.issues[n].Comments = append(g.issues[n].Comments, in.Body)
	reply(w, http.StatusCreated, map[string]any{"id": len(g.issues[n].Comments), "body": in.Body})
}

// F-0452 crit. 3: Gitea takes labels by id. Closing a rejected card's issue
// creates cerne:rejected when the repository lacks it, with a #RRGGBB color,
// labels the issue with the id Gitea answered, and creates it once; a label
// the repository has, even past the first page of labels, is reused. Third
// party labels stay.
func TestGiteaCreatesMissingLabel(t *testing.T) {
	ctx := context.Background()
	t.Run("missing", func(t *testing.T) {
		g := newFakeGitea(t)
		r := g.seed(&gtIssue{Title: "T", Body: "B"}, "bug", "cerne:approved")
		tr := newGiteaTracker(t, "https://git.example/o/r")
		for range 2 {
			if err := tr.Update(ctx, r, tracker.Item{CardID: "F-0001", State: domain.StateRejected}); err != nil {
				t.Fatalf("Update(rejected): %v", err)
			}
		}
		if n := g.count(http.MethodPost, "/labels"); n != 1 {
			t.Fatalf("%d label creations over two updates, want 1", n)
		}
		l, ok := g.labelNamed("cerne:rejected")
		if !ok || !hexColor.MatchString(l.Color) {
			t.Fatalf("label cerne:rejected = %+v (present %v), want created with a #RRGGBB color", l, ok)
		}
		if got := g.names(1); !slices.Equal(got, []string{"bug", "cerne:rejected"}) || g.issues[1].State != "closed" {
			t.Fatalf("issue labels %v, state %s; want [bug cerne:rejected], closed", got, g.issues[1].State)
		}
	})
	t.Run("present past the first page", func(t *testing.T) {
		g := newFakeGitea(t)
		for i := range 60 {
			g.addLabel(fmt.Sprintf("area-%02d", i), "#ededed")
		}
		g.addLabel("cerne:superseded", "#ededed")
		r := g.seed(&gtIssue{Title: "T", Body: "B"}, "cerne:in_development")
		if err := newGiteaTracker(t, "https://git.example/o/r").Update(ctx, r, tracker.Item{CardID: "F-0001", State: domain.StateSuperseded}); err != nil {
			t.Fatalf("Update(superseded): %v", err)
		}
		if n := g.count(http.MethodPost, "/labels"); n != 0 {
			t.Fatalf("%d label creations with cerne:superseded on page 2, want 0", n)
		}
		if got := g.names(1); !slices.Equal(got, []string{"cerne:superseded"}) || g.issues[1].State != "closed" {
			t.Fatalf("issue labels %v, state %s; want [cerne:superseded], closed", got, g.issues[1].State)
		}
	})
}

// A short page is the last one: 60 labels are two page requests, not three.
func TestGiteaLabelPagesStopAtShortPage(t *testing.T) {
	g := newFakeGitea(t)
	for i := range 60 {
		g.addLabel(fmt.Sprintf("area-%02d", i), "#ededed")
	}
	r := g.seed(&gtIssue{Title: "T", Body: "B"}, "cerne:approved")
	if err := newGiteaTracker(t, "https://git.example/o/r").Update(context.Background(), r, tracker.Item{CardID: "F-0001", State: domain.StateRejected}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n := g.count(http.MethodGet, "/labels"); n != 2 {
		t.Fatalf("%d label-page requests for 60 labels, want 2", n)
	}
}

// A server that never ends the label list (a proxy dropping ?page=) is cut at
// a cap, with an error that carries no server text.
func TestGiteaLabelPagesHaveACap(t *testing.T) {
	g := newFakeGitea(t)
	g.noPaging = true
	for i := range 50 {
		g.addLabel(fmt.Sprintf("area-%02d", i), "#ededed")
	}
	r := g.seed(&gtIssue{Title: "T", Body: "B"}, "cerne:approved")
	err := newGiteaTracker(t, "https://git.example/o/r").Update(context.Background(), r, tracker.Item{CardID: "F-0001", State: domain.StateRejected})
	if err == nil || !strings.Contains(err.Error(), "git.example") || !strings.Contains(err.Error(), "20") {
		t.Fatalf("Update = %v, want an error naming git.example and the cap 20", err)
	}
	if n := g.count(http.MethodGet, "/labels"); n > 20 {
		t.Fatalf("%d label-page requests, want at most 20", n)
	}
}

// F-0452 crit. 2 and 3: the Gitea adapter passes the contract suite the fake
// passes, against the emulator; then what the suite leaves to each kind.
func TestGiteaContract(t *testing.T) {
	ctx := context.Background()
	var g *fakeGitea
	trackertest.RunContract(t, trackertest.Harness{
		New: func(t *testing.T) tracker.IssueTracker {
			g = newFakeGitea(t)
			return newGiteaTracker(t, "https://git.example/o/r")
		},
		Open: func(_ *testing.T, title, body, label string) tracker.Ref {
			var labels []string
			if label != "" {
				labels = []string{label}
			}
			return g.seed(&gtIssue{Title: title, Body: body}, labels...)
		},
		ClosesOnMerge: true,
	})

	// Gitea has no close reason: rejected and superseded close with their
	// cerne:* label, the successor comment goes once, with the closing call,
	// and an update with nothing new writes nothing.
	t.Run("superseded closes with its label and comments once", func(t *testing.T) {
		g = newFakeGitea(t)
		tr := newGiteaTracker(t, "https://git.example/o/r")
		it := tracker.Item{CardID: "F-0001", State: domain.StateApproved, Title: "Public", Summary: "Summary"}
		r, err := tr.Create(ctx, it)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		it.State, it.Successor = domain.StateSuperseded, &tracker.Ref{Tracker: "gt", ID: "9", URL: g.url(9)}
		for range 2 {
			if err := tr.Update(ctx, r, it); err != nil {
				t.Fatalf("Update(superseded): %v", err)
			}
		}
		iss := g.issues[1]
		if got := g.names(1); !slices.Equal(got, []string{"cerne:superseded"}) || iss.State != "closed" {
			t.Fatalf("labels %v, state %s; want [cerne:superseded], closed", got, iss.State)
		}
		if want := []string{"Superseded by " + g.url(9)}; !slices.Equal(iss.Comments, want) {
			t.Fatalf("comments %q, want %q", iss.Comments, want)
		}
		before := len(*g.reqs)
		if err := tr.Update(ctx, r, it); err != nil {
			t.Fatalf("Update(unchanged): %v", err)
		}
		for _, req := range (*g.reqs)[before:] {
			if req.method != http.MethodGet {
				t.Fatalf("an unchanged update sent %s %s", req.method, req.url)
			}
		}
	})

	t.Run("lists at most 100 open issues across pages, never PRs", func(t *testing.T) {
		g = newFakeGitea(t)
		g.seed(&gtIssue{Title: "PR", PR: true})
		g.seed(&gtIssue{Title: "Closed", State: "closed"})
		for i := range 120 {
			g.seed(&gtIssue{Title: fmt.Sprint("Open ", i)})
		}
		list, err := newGiteaTracker(t, "https://git.example/o/r").ListOpen(ctx, "")
		if err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		if len(list) != 100 || list[0].Title != "Open 0" || list[99].Title != "Open 99" {
			t.Fatalf("ListOpen listed %d issues, first %+v; want Open 0 to Open 99", len(list), list[0])
		}
	})

	// Gitea drops a label name the repository lacks from its filter and then
	// lists every issue: the import label must not pull in the whole tracker.
	t.Run("a label the repository lacks lists nothing", func(t *testing.T) {
		g = newFakeGitea(t)
		g.seed(&gtIssue{Title: "Outside"}, "bug")
		list, err := newGiteaTracker(t, "https://git.example/o/r").ListOpen(ctx, "public")
		if err != nil || len(list) != 0 {
			t.Fatalf("ListOpen(public) = %+v, %v; want none", list, err)
		}
	})
}

// F-0452 crit. 4: the whole trailer, the shortest way Gitea links it from a
// commit in the commit_url repository: #N in the same repository,
// owner/repo#N on the same host, the issue URL anywhere else; Refs, or Closes
// when closing. It is pure: no request.
func TestGiteaCommitRefForms(t *testing.T) {
	for _, c := range []struct {
		name, trackerURL, commitURL string
		closing                     bool
		want                        string
	}{
		{"same repository", "https://git.example/o/r", "https://git.example/o/r/commit/{sha}", false, "Refs: #12"},
		{"same repository, closing", "https://git.example/o/r", "https://git.example/o/r/commit/{sha}", true, "Closes: #12"},
		{"same repository, host with port", "https://git.example:3000/o/r", "https://git.example:3000/o/r/commit/{sha}", true, "Closes: #12"},
		{"other repository, same host", "https://git.example/o/issues", "https://git.example/o/r/commit/{sha}", false, "Refs: o/issues#12"},
		{"other repository, same host, closing", "https://git.example/o/issues", "https://git.example/o/r/commit/{sha}", true, "Closes: o/issues#12"},
		{"other forge", "https://git.example/o/r", "https://github.com/o/r/commit/{sha}", false, "Refs: https://git.example/o/r/issues/12"},
		{"other forge, closing", "https://git.example/o/r", "https://gitlab.com/o/r/-/commit/{sha}", true, "Closes: https://git.example/o/r/issues/12"},
		{"same path, other port", "https://git.example:3000/o/r", "https://git.example/o/r/commit/{sha}", false, "Refs: https://git.example:3000/o/r/issues/12"},
		{"no commit_url", "https://git.example/o/r", "", false, "Refs: https://git.example/o/r/issues/12"},
	} {
		t.Run(c.name, func(t *testing.T) {
			reqs := installGitea(t, "", http.NotFoundHandler())
			r := tracker.Ref{Tracker: "gt", ID: "12", URL: c.trackerURL + "/issues/12"}
			if got := newGiteaTracker(t, c.trackerURL).CommitRef(r, c.commitURL, c.closing); got != c.want {
				t.Fatalf("CommitRef = %q, want %q", got, c.want)
			}
			if len(*reqs) != 0 {
				t.Fatalf("CommitRef sent %d requests, want none", len(*reqs))
			}
		})
	}
}

// F-0452 crit. 2, live: the contract suite against a real, throwaway Gitea
// that the plan's script starts in a container; skipped without
// CERNE_LIVE_GITEA. The kind refuses localhost, so the tracker host is a name
// only the test certificate carries, dialed on the loopback: TLS still checks
// that name, against that one CA, and no redirect is followed. The test ends
// by logging the closing trailer of an issue it opens, for the script's commit.
func TestGiteaContractLive(t *testing.T) {
	raw := os.Getenv("CERNE_LIVE_GITEA")
	if raw == "" {
		t.Skip("CERNE_LIVE_GITEA is unset: no live Gitea to run against")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Port() == "" {
		t.Fatalf("CERNE_LIVE_GITEA %q: want https://<name>:<port>/<owner>/<repo>", raw)
	}
	pool := x509.NewCertPool()
	if pem, err := os.ReadFile(os.Getenv("CERNE_LIVE_GITEA_CA")); err != nil || !pool.AppendCertsFromPEM(pem) {
		t.Fatalf("CERNE_LIVE_GITEA_CA: no certificate read (%v)", err)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", u.Port()))
			},
		},
	}
	token := os.Getenv("CERNE_LIVE_GITEA_TOKEN")
	prevDo, prevFill := forge.HTTPDo, forge.CredentialFill
	t.Cleanup(func() { forge.HTTPDo, forge.CredentialFill = prevDo, prevFill })
	forge.HTTPDo = client.Do
	forge.CredentialFill = func(context.Context, string) (string, string) { return "", token }

	// api makes the calls the contract's outside issues need, beside the adapter.
	api := func(t *testing.T, method, path string, in, out any) {
		t.Helper()
		body, _ := json.Marshal(in)
		req, err := http.NewRequest(method, "https://"+u.Host+"/api/v1/repos"+u.Path+path, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: request failed", method, path)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			t.Fatalf("%s %s: HTTP %d", method, path, resp.StatusCode)
		}
		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
		}
	}
	tr := newGiteaTracker(t, raw)
	trackertest.RunContract(t, trackertest.Harness{
		New: func(*testing.T) tracker.IssueTracker { return tr },
		Open: func(t *testing.T, title, body, label string) tracker.Ref {
			in := map[string]any{"title": title, "body": body}
			if label != "" {
				var labels []gtLabel
				api(t, http.MethodGet, "/labels?limit=50", nil, &labels)
				i := slices.IndexFunc(labels, func(l gtLabel) bool { return l.Name == label })
				l := gtLabel{}
				if i < 0 {
					api(t, http.MethodPost, "/labels", map[string]string{"name": label, "color": "#ededed"}, &l)
				} else {
					l = labels[i]
				}
				in["labels"] = []int64{l.ID}
			}
			var iss struct {
				Number  int64  `json:"number"`
				HTMLURL string `json:"html_url"`
			}
			api(t, http.MethodPost, "/issues", in, &iss)
			return tracker.Ref{Tracker: "gt", ID: strconv.FormatInt(iss.Number, 10), URL: iss.HTMLURL}
		},
		ClosesOnMerge: true,
	})

	r, err := tr.Create(context.Background(), tracker.Item{CardID: "F-0452", State: domain.StateInDevelopment,
		Title: "Closed by a commit trailer", Summary: "Live check of the closing trailer"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Logf("closing trailer: %s", tr.CommitRef(r, "https://"+u.Host+u.Path+"/commit/{sha}", true))
}

// F-0452: the login goes to the record's host, so numeric and local spellings
// are refused (the core's shared check); a normal DNS host is accepted.
func TestGiteaRefusesNumericAndLocalHostForms(t *testing.T) {
	for _, raw := range []string{"https://127.1/o/r", "https://localhost./o/r", "https://[fe80::1%25en0]/o/r", "https://a.localhost/o/r", "https://2130706433/o/r"} {
		if _, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: raw}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(gitea, %s) = %v, want ErrTrackerURL", raw, err)
		}
	}
	if _, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: "https://git.example.com/o/r"}); err != nil {
		t.Errorf("New(gitea, DNS host) = %v, want nil", err)
	}
}

// F-0452: Owns is GitHub's, so it inherits the strict rule (empty query and
// fragment, escaped path compared). Passes on first run: inherited behavior.
func TestGiteaOwnsRefusesQueryFragmentAndEscapedSeparators(t *testing.T) {
	tr, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: "https://git.example.com/o/r"})
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Owns(tracker.Ref{ID: "7", URL: "https://git.example.com/o/r/issues/7"}) {
		t.Error("Owns refused its own issue")
	}
	for _, u := range []string{"https://git.example.com/o/r/issues/7?x=1", "https://git.example.com/o/r/issues/7#c", "https://git.example.com/o/r/issues/7?", "https://git.example.com/o%2Fr/issues/7", "https://git.example.com/o/r/issues%2F7"} {
		if tr.Owns(tracker.Ref{ID: "7", URL: u}) {
			t.Errorf("Owns accepted %s", u)
		}
	}
}

package tracker_test

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// glIssue is one issue the glab emulator holds.
type glIssue struct {
	IID                int
	Title, Description string
	State              string // "opened" | "closed"
	Labels             []string
	Notes              []string
}

// fakeGL emulates, behind forge.RunCLI, the slice of the GitLab REST API the
// adapter calls, for one project on one host. It parses argv the way glab
// does: -X/--method, --hostname, -f/--raw-field key=value, and fields with no
// method make a POST. It refuses -F/--field and --input, which read files, an
// unescaped project path, and a call with no --hostname: glab would then call
// the host of the current directory's repository, not the tracker's.
type fakeGL struct {
	host, project string // "gitlab.com", "g/sub/p"
	issues        map[int]*glIssue
	next          int
	calls         [][]string // argv of every call, "glab" first
	reqs          []glReq
}

type glReq struct {
	method, path string
	query        url.Values
	fields       map[string]string
}

func newFakeGL(t *testing.T, host, project string) *fakeGL {
	t.Helper()
	g := &fakeGL{host: host, project: project, issues: map[int]*glIssue{}}
	old := forge.RunCLI
	t.Cleanup(func() { forge.RunCLI = old })
	forge.RunCLI = g.run
	return g
}

func (g *fakeGL) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	g.calls = append(g.calls, append([]string{name}, args...))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name != "glab" || len(args) == 0 || args[0] != "api" {
		return nil, fmt.Errorf("fake glab: not a glab api call: %s %q", name, args)
	}
	method, host, endpoint := "", "", ""
	fields := map[string]string{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			if endpoint != "" {
				return nil, fmt.Errorf("fake glab: two endpoints %q and %q", endpoint, a)
			}
			endpoint = a
			continue
		}
		flag, val, inline := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "--") {
			flag, inline = a, false
		}
		if !slices.Contains([]string{"-X", "--method", "--hostname", "-f", "--raw-field"}, flag) {
			return nil, fmt.Errorf("fake glab: unsupported flag %q (-F, --field and --input read files)", a)
		}
		if !inline {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("fake glab: %s needs a value", a)
			}
			i++
			val = args[i]
		}
		switch flag {
		case "-X", "--method":
			method = strings.ToUpper(val)
		case "--hostname":
			host = val
		default:
			k, v, _ := strings.Cut(val, "=")
			if _, dup := fields[k]; dup {
				return nil, fmt.Errorf("fake glab: field %q twice", k)
			}
			fields[k] = v
		}
	}
	if host == "" {
		return nil, errors.New("fake glab: no --hostname: glab would call the host of the current directory's repository")
	}
	if host != g.host {
		return nil, fmt.Errorf("glab: 404 Not Found on host %s (the project lives on %s)", host, g.host)
	}
	if method == "" {
		method = "GET"
		if len(fields) > 0 {
			method = "POST"
		}
	}
	path, rawQuery, _ := strings.Cut(endpoint, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("fake glab: query %q: %v", rawQuery, err)
	}
	g.reqs = append(g.reqs, glReq{method: method, path: path, query: q, fields: fields})

	rest, ok := strings.CutPrefix(path, "projects/"+url.PathEscape(g.project)+"/issues")
	if !ok {
		return nil, fmt.Errorf("glab: 404 Project Not Found: %s", path)
	}
	switch {
	case rest == "" && method == "GET":
		return g.list(q)
	case rest == "" && method == "POST":
		return g.create(fields)
	}
	num, sub, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	n, err := strconv.Atoi(num)
	iss := g.issues[n]
	if err != nil || iss == nil {
		return nil, fmt.Errorf("glab: 404 Not Found: %s", path)
	}
	switch {
	case sub == "" && method == "GET":
		return g.answer(iss)
	case sub == "" && method == "PUT":
		return g.put(iss, fields)
	case sub == "notes" && method == "POST":
		if fields["body"] == "" {
			return nil, errors.New("glab: 400 Bad Request: body is missing")
		}
		iss.Notes = append(iss.Notes, fields["body"])
		return json.Marshal(map[string]any{"id": len(iss.Notes), "body": fields["body"]})
	}
	return nil, fmt.Errorf("glab: 404 Not Found: %s %s", method, path)
}

// list answers GET .../issues: state defaults to all, labels is an AND list,
// and a page holds 20 issues unless per_page says otherwise.
func (g *fakeGL) list(q url.Values) ([]byte, error) {
	state := cmp.Or(q.Get("state"), "all")
	var want []string
	if l := q.Get("labels"); l != "" {
		want = strings.Split(l, ",")
	}
	page := 20
	if n, err := strconv.Atoi(q.Get("per_page")); err == nil {
		page = n
	}
	out := []map[string]any{}
	for _, n := range slices.Sorted(maps.Keys(g.issues)) {
		iss := g.issues[n]
		if (state == "all" || iss.State == state) && hasAll(iss.Labels, want) && len(out) < page {
			out = append(out, g.view(iss))
		}
	}
	return json.Marshal(out)
}

// unknownField refuses a field the endpoint does not take, so a misnamed one
// (body for description) fails instead of being dropped.
func unknownField(fields map[string]string, known ...string) error {
	for k := range fields {
		if !slices.Contains(known, k) {
			return fmt.Errorf("glab: 400 Bad Request: unknown field %q", k)
		}
	}
	return nil
}

func splitLabels(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func (g *fakeGL) create(fields map[string]string) ([]byte, error) {
	if err := unknownField(fields, "title", "description", "labels"); err != nil {
		return nil, err
	}
	if fields["title"] == "" {
		return nil, errors.New("glab: 400 Bad Request: title is missing")
	}
	iss := &glIssue{Title: fields["title"], Description: fields["description"], Labels: splitLabels(fields["labels"])}
	g.add(iss)
	return g.answer(iss)
}

func (g *fakeGL) put(iss *glIssue, fields map[string]string) ([]byte, error) {
	if err := unknownField(fields, "title", "description", "labels", "add_labels", "remove_labels", "state_event"); err != nil {
		return nil, err
	}
	if ev, ok := fields["state_event"]; ok {
		switch ev {
		case "close":
			iss.State = "closed"
		case "reopen":
			iss.State = "opened"
		default:
			return nil, fmt.Errorf("glab: 400 Bad Request: state_event does not have a valid value %q", ev)
		}
	}
	if v, ok := fields["title"]; ok {
		iss.Title = v
	}
	if v, ok := fields["description"]; ok {
		iss.Description = v
	}
	if v, ok := fields["labels"]; ok {
		iss.Labels = splitLabels(v)
	}
	for _, l := range splitLabels(fields["remove_labels"]) {
		iss.Labels = slices.DeleteFunc(iss.Labels, func(x string) bool { return x == l })
	}
	for _, l := range splitLabels(fields["add_labels"]) {
		if !slices.Contains(iss.Labels, l) {
			iss.Labels = append(iss.Labels, l)
		}
	}
	return g.answer(iss)
}

func (g *fakeGL) add(iss *glIssue) int {
	g.next++
	iss.IID = g.next
	if iss.State == "" {
		iss.State = "opened"
	}
	g.issues[g.next] = iss
	return g.next
}

func (g *fakeGL) webURL(n int) string {
	return fmt.Sprintf("https://%s/%s/-/issues/%d", g.host, g.project, n)
}

// view is the issue as the REST API answers it: a global id unlike the iid,
// labels as names, an empty description as null.
func (g *fakeGL) view(iss *glIssue) map[string]any {
	m := map[string]any{
		"id": 9000 + iss.IID, "iid": iss.IID, "project_id": 42, "web_url": g.webURL(iss.IID),
		"title": iss.Title, "description": nil, "state": iss.State, "labels": append([]string{}, iss.Labels...),
	}
	if iss.Description != "" {
		m["description"] = iss.Description
	}
	return m
}

func (g *fakeGL) answer(iss *glIssue) ([]byte, error) { return json.Marshal(g.view(iss)) }

// ref is the Ref the adapter named "gl" must hand back for issue n.
func (g *fakeGL) ref(n int) tracker.Ref {
	return tracker.Ref{Tracker: "gl", ID: strconv.Itoa(n), URL: g.webURL(n)}
}

// newGitLab builds the adapter named "gl" for the project at projectURL.
func newGitLab(t *testing.T, projectURL string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "gl", Kind: "gitlab", URL: projectURL})
	if err != nil {
		t.Fatalf("New(gitlab, %s): %v", projectURL, err)
	}
	return tr
}

// F-0450 crit. 1: kind "gitlab" is accepted, with the host and the project
// path (subgroups included) read from the record's url, and the unknown-kind
// text lists it. The list is built from KindNames, so a sibling kind (gitea)
// does not break this test.
func TestTrackerKindGitLabAccepted(t *testing.T) {
	for _, u := range []string{
		"https://gitlab.com/g/p",
		"https://gitlab.com/g/sub/p/",
		"https://gitlab.example/g/sub/sub2/p",
	} {
		if _, err := tracker.New(tracker.Config{Name: "gl", Kind: "gitlab", URL: u}); err != nil {
			t.Errorf("New(gitlab, %q) = %v, want an adapter", u, err)
		}
	}
	for _, u := range []string{
		"http://gitlab.com/g/p",
		"https://gitlab.com",
		"https://gitlab.com/",
		"https://gitlab.com/g",
		"https://gitlab.com/g/p/-/issues", // a page of the project, not the project
		"https://gitlab.com/g/../p",
		"https://gitlab.com/g/p?private_token=x",
		"https://gitlab.com/g/p#f",
		"https://127.0.0.1/g/p",
		"https://localhost/g/p",
	} {
		if _, err := tracker.New(tracker.Config{Name: "gl", Kind: "gitlab", URL: u}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(gitlab, %q) = %v, want errors.Is ErrTrackerURL", u, err)
		}
	}
	kinds := tracker.KindNames()
	if !slices.Contains(kinds, "github") || !slices.Contains(kinds, "gitlab") {
		t.Fatalf("KindNames() = %v, want github and gitlab", kinds)
	}
	_, err := tracker.New(tracker.Config{Name: "j", Kind: "jira", URL: "https://jira.example/PROJ"})
	want := `unknown tracker kind "jira" (supported: ` + strings.Join(kinds, ", ") + ")"
	if !errors.Is(err, tracker.ErrUnknownKind) || err.Error() != want {
		t.Fatalf("New(jira) = %v, want errors.Is ErrUnknownKind with the text %q", err, want)
	}
	t.Logf("unknown kind: %v", err)
}

// F-0450 crit. 1: every call names the record's host — gitlab.com too, since
// glab with no --hostname calls the host of the current directory's
// repository — and the project path, subgroups included, goes url-encoded.
// ListOpen asks for the open issues, 100 per page, filtered by the label.
func TestGitLabHostPathAndQuery(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct{ url, host, project string }{
		{"https://gitlab.com/g/sub/p", "gitlab.com", "g/sub/p"},
		{"https://gitlab.example/acme/issues/", "gitlab.example", "acme/issues"},
	} {
		t.Run(c.host, func(t *testing.T) {
			g := newFakeGL(t, c.host, c.project)
			tr := newGitLab(t, c.url)
			r, err := tr.Create(ctx, publicItem("F-0001", domain.StateCandidate))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if want := g.ref(1); r != want {
				t.Fatalf("Create returned %+v, want %+v", r, want)
			}
			argv := g.calls[0]
			if i := slices.Index(argv, "--hostname"); i < 0 || i+1 >= len(argv) || argv[i+1] != c.host {
				t.Fatalf("argv %q does not name host %s with --hostname", argv, c.host)
			}
			if want := "projects/" + url.PathEscape(c.project) + "/issues"; !slices.Contains(argv, want) {
				t.Fatalf("argv %q lacks the endpoint %s", argv, want)
			}
			if _, err := tr.ListOpen(ctx, "cerne:x y"); err != nil {
				t.Fatalf("ListOpen: %v", err)
			}
			last := g.reqs[len(g.reqs)-1]
			if last.method != "GET" || last.query.Get("state") != "opened" || last.query.Get("per_page") != "100" || last.query.Get("labels") != "cerne:x y" {
				t.Fatalf("request = %s %s?%s, want GET with state=opened, per_page=100 and labels=cerne:x y", last.method, last.path, last.query.Encode())
			}
		})
	}
}

// F-0450 crit. 2: the GitLab adapter passes the contract suite the fake
// passes, on a project in a subgroup.
func TestGitLabContract(t *testing.T) {
	var g *fakeGL
	trackertest.RunContract(t, trackertest.Harness{
		New: func(t *testing.T) tracker.IssueTracker {
			g = newFakeGL(t, "gitlab.com", "g/sub/p")
			return newGitLab(t, "https://gitlab.com/g/sub/p")
		},
		Open: func(_ *testing.T, title, body, label string) tracker.Ref {
			return g.ref(g.add(&glIssue{Title: title, Description: body, Labels: splitLabels(label)}))
		},
		ClosesOnMerge: true,
	})
}

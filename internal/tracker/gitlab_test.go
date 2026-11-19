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

// webURL is the issue page as gitlab.com 19.x answers it: /-/work_items/N.
// Older GitLab answers /-/issues/N, which TestGitLabOwns covers.
func (g *fakeGL) webURL(n int) string {
	return fmt.Sprintf("https://%s/%s/-/work_items/%d", g.host, g.project, n)
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

// F-0450 crit. 2 (Owns): an issue of the project is found under both pages
// GitLab answers for it — /-/issues/N (older GitLab) and /-/work_items/N
// (gitlab.com 19.x) — and nothing else is.
func TestGitLabOwns(t *testing.T) {
	newFakeGL(t, "gitlab.com", "g/sub/p")
	tr := newGitLab(t, "https://gitlab.com/g/sub/p")
	for _, c := range []struct {
		id, url string
		want    bool
	}{
		{"12", "https://gitlab.com/g/sub/p/-/issues/12", true},
		{"12", "https://gitlab.com/g/sub/p/-/work_items/12", true},
		{"12", "https://GitLab.com/G/Sub/P/-/issues/12/", true},
		{"12", "https://gitlab.com/g/sub/p/-/merge_requests/12", false},
		{"12", "https://gitlab.com/g/sub/p/-/issues/13", false},
		{"12", "https://gitlab.com/g/other/-/issues/12", false},
		{"12", "https://gitlab.example/g/sub/p/-/issues/12", false},
		{"12", "https://gitlab.com/g/sub/p/issues/12", false},
	} {
		if got := tr.Owns(tracker.Ref{Tracker: "gl", ID: c.id, URL: c.url}); got != c.want {
			t.Errorf("Owns(%s, %s) = %v, want %v", c.id, c.url, got, c.want)
		}
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

// F-0450 crit. 3: GitLab has no close reason, so the cerne:<state> label
// carries it — rejected and superseded close with cerne:rejected and
// cerne:superseded. Through Create and through Update, a state change swaps
// the cerne:* label and keeps the others; the superseded comment goes only
// with the call that closes the issue.
func TestGitLabCloseReasonLabel(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		state domain.FeatureState
		open  bool
		label string
	}{
		{domain.StateCandidate, true, "cerne:candidate"},
		{domain.StateApproved, true, "cerne:approved"},
		{domain.StateInDevelopment, true, "cerne:in_development"},
		{domain.StateImplemented, true, "cerne:ready"},
		{domain.StateDone, false, "cerne:done"},
		{domain.StateRejected, false, "cerne:rejected"},
		{domain.StateSuperseded, false, "cerne:superseded"},
	} {
		t.Run(string(c.state), func(t *testing.T) {
			check := func(t *testing.T, iss *glIssue, wantLabels []string) {
				t.Helper()
				if want := map[bool]string{true: "opened", false: "closed"}[c.open]; iss.State != want {
					t.Fatalf("issue state = %s, want %s", iss.State, want)
				}
				if got := sortedLabels(iss.Labels); !slices.Equal(got, sortedLabels(wantLabels)) {
					t.Fatalf("labels = %q, want %q", got, wantLabels)
				}
			}
			t.Run("create", func(t *testing.T) {
				g := newFakeGL(t, "gitlab.com", "g/p")
				r, err := newGitLab(t, "https://gitlab.com/g/p").Create(ctx, publicItem("F-0007", c.state))
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				n, _ := strconv.Atoi(r.ID)
				check(t, g.issues[n], []string{c.label})
			})
			t.Run("update", func(t *testing.T) {
				g := newFakeGL(t, "gitlab.com", "g/p")
				n := g.add(&glIssue{Title: "Old", Labels: []string{"bug", "cerne:stale"}})
				if err := newGitLab(t, "https://gitlab.com/g/p").Update(ctx, g.ref(n), publicItem("F-0007", c.state)); err != nil {
					t.Fatalf("Update: %v", err)
				}
				check(t, g.issues[n], []string{"bug", c.label})
			})
		})
	}

	t.Run("a closed issue changes its reason by label only", func(t *testing.T) {
		g := newFakeGL(t, "gitlab.com", "g/p")
		n := g.add(&glIssue{Title: "T", State: "closed", Labels: []string{"bug", "cerne:done"}})
		if err := newGitLab(t, "https://gitlab.com/g/p").Update(ctx, g.ref(n), publicItem("F-0007", domain.StateRejected)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if iss := g.issues[n]; iss.State != "closed" || !slices.Equal(sortedLabels(iss.Labels), []string{"bug", "cerne:rejected"}) {
			t.Fatalf("issue = %+v, want closed with [bug cerne:rejected]", iss)
		}
		if last := g.reqs[len(g.reqs)-1]; last.method == "PUT" && last.fields["state_event"] != "" {
			t.Fatalf("PUT sent state_event=%s to an issue already closed", last.fields["state_event"])
		}
	})

	succ := &tracker.Ref{Tracker: "gl", ID: "99", URL: "https://gitlab.com/g/p/-/issues/99"}
	t.Run("superseded comments the mirrored successor once", func(t *testing.T) {
		g := newFakeGL(t, "gitlab.com", "g/p")
		n := g.add(&glIssue{Title: "T"})
		tr := newGitLab(t, "https://gitlab.com/g/p")
		it := publicItem("F-0007", domain.StateSuperseded)
		it.Successor = succ
		for range 2 {
			if err := tr.Update(ctx, g.ref(n), it); err != nil {
				t.Fatalf("Update: %v", err)
			}
		}
		if got := g.issues[n].Notes; !slices.Equal(got, []string{"Superseded by " + succ.URL}) {
			t.Fatalf("notes = %q, want exactly one %q", got, "Superseded by "+succ.URL)
		}
	})
	t.Run("superseded with no mirrored successor writes no comment", func(t *testing.T) {
		g := newFakeGL(t, "gitlab.com", "g/p")
		n := g.add(&glIssue{Title: "T"})
		if err := newGitLab(t, "https://gitlab.com/g/p").Update(ctx, g.ref(n), publicItem("F-0007", domain.StateSuperseded)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got := g.issues[n].Notes; len(got) != 0 {
			t.Fatalf("notes = %q, want none", got)
		}
	})
	t.Run("superseded on an issue already closed writes no comment", func(t *testing.T) {
		g := newFakeGL(t, "gitlab.com", "g/p")
		n := g.add(&glIssue{Title: "T", State: "closed", Labels: []string{"cerne:rejected"}})
		it := publicItem("F-0007", domain.StateSuperseded)
		it.Successor = succ
		if err := newGitLab(t, "https://gitlab.com/g/p").Update(ctx, g.ref(n), it); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got := g.issues[n].Notes; len(got) != 0 {
			t.Fatalf("notes = %q, want none: the issue was not open", got)
		}
	})
	t.Run("an unchanged item sends no PUT", func(t *testing.T) {
		g := newFakeGL(t, "gitlab.com", "g/p")
		n := g.add(&glIssue{Title: "T", Labels: []string{"bug"}})
		tr := newGitLab(t, "https://gitlab.com/g/p")
		it := publicItem("F-0007", domain.StateApproved)
		puts := func() int {
			k := 0
			for _, r := range g.reqs {
				if r.method == "PUT" {
					k++
				}
			}
			return k
		}
		if err := tr.Update(ctx, g.ref(n), it); err != nil {
			t.Fatalf("Update: %v", err)
		}
		before := puts()
		if err := tr.Update(ctx, g.ref(n), it); err != nil {
			t.Fatalf("second Update: %v", err)
		}
		if after := puts(); before == 0 || after != before {
			t.Fatalf("PUT calls = %d then %d, want the first Update to write and the second to send none", before, after)
		}
	})
}

// F-0450 crit. 4: the whole trailer — #N for the commit_url's own project,
// group/sub/project#N for another project on the same host, the issue URL on
// another host or forge — with Refs, or Closes when closing. Pure: no glab.
func TestGitLabCommitRefForms(t *testing.T) {
	for _, c := range []struct {
		name, trackerURL, commitURL string
		closing                     bool
		want                        string
	}{
		{"same project", "https://gitlab.com/g/sub/p", "https://gitlab.com/g/sub/p/-/commit/{sha}", false, "Refs: #12"},
		{"same project, closing", "https://gitlab.com/g/sub/p", "https://gitlab.com/g/sub/p/-/commit/{sha}", true, "Closes: #12"},
		{"other project, same host", "https://gitlab.com/g/sub/issues", "https://gitlab.com/g/code/-/commit/{sha}", false, "Refs: g/sub/issues#12"},
		{"other project, same host, closing", "https://gitlab.com/g/sub/issues", "https://gitlab.com/g/code/-/commit/{sha}", true, "Closes: g/sub/issues#12"},
		{"other forge", "https://gitlab.com/g/sub/p", "https://github.com/o/r/commit/{sha}", false, "Refs: https://gitlab.com/g/sub/p/-/issues/12"},
		{"other forge, closing", "https://gitlab.com/g/sub/p", "https://github.com/o/r/commit/{sha}", true, "Closes: https://gitlab.com/g/sub/p/-/issues/12"},
		{"same path on another host", "https://gitlab.example/g/sub/p", "https://gitlab.com/g/sub/p/-/commit/{sha}", false, "Refs: https://gitlab.example/g/sub/p/-/issues/12"},
		{"self-hosted, same project", "https://gitlab.example/g/sub/p", "https://gitlab.example/g/sub/p/-/commit/{sha}", false, "Refs: #12"},
		{"no commit_url", "https://gitlab.com/g/sub/p", "", false, "Refs: https://gitlab.com/g/sub/p/-/issues/12"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newFakeGL(t, "gitlab.com", "g/sub/p")
			r := tracker.Ref{Tracker: "gl", ID: "12", URL: c.trackerURL + "/-/issues/12"}
			if got := newGitLab(t, c.trackerURL).CommitRef(r, c.commitURL, c.closing); got != c.want {
				t.Fatalf("CommitRef = %q, want %q", got, c.want)
			}
			if len(g.calls) != 0 {
				t.Fatalf("CommitRef called glab: %q (it is pure)", g.calls)
			}
		})
	}
}

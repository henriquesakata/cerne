package tracker_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// TestMain keeps every test of this package off the real gh: a test that
// forgets to install the emulator fails instead of publishing as the owner.
func TestMain(m *testing.M) {
	forge.RunCLI = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("test guard: %s %s must not run for real (F-0449)", name, strings.Join(args, " "))
	}
	os.Exit(m.Run())
}

// ghIssue is one issue (or PR) the emulator holds.
type ghIssue struct {
	Number      int
	Title, Body string
	State       string // "open" | "closed"
	StateReason string // "" | "completed" | "not_planned" | "reopened"
	Labels      []string
	PR          bool
	Comments    []string
}

// fakeGH emulates, behind forge.RunCLI, the slice of the GitHub REST API the
// plan's call table names, for one repository on one host. It parses argv the
// way gh does: -X/--method, --hostname, -f/--raw-field key=value (key[] builds
// an array), the fields of a GET become its query, and fields with no method
// make a POST. -F/--field is refused: it expands @file.
type fakeGH struct {
	host, repo string // "github.com", "o/r"
	issues     map[int]*ghIssue
	next       int
	calls      [][]string // argv of every call, "gh" first
	reqs       []ghReq
}

type ghReq struct {
	method, path string
	query        url.Values
}

func newFakeGH(t *testing.T, host, repo string) *fakeGH {
	t.Helper()
	g := &fakeGH{host: host, repo: repo, issues: map[int]*ghIssue{}}
	old := forge.RunCLI
	t.Cleanup(func() { forge.RunCLI = old })
	forge.RunCLI = g.run
	return g
}

var ghValued = map[string]bool{"-X": true, "--method": true, "--hostname": true, "-f": true, "--raw-field": true, "-F": true, "--field": true, "-H": true, "--header": true}

func (g *fakeGH) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	g.calls = append(g.calls, append([]string{name}, args...))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name != "gh" || len(args) == 0 || args[0] != "api" {
		return nil, fmt.Errorf("fake gh: not a gh api call: %s %q", name, args)
	}
	method, host, path := "", "github.com", ""
	fields := map[string][]string{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			if path != "" {
				return nil, fmt.Errorf("fake gh: two endpoints %q and %q", path, a)
			}
			path = a
			continue
		}
		flag, val, inline := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "--") {
			flag, inline = a, false
		}
		if !ghValued[flag] {
			return nil, fmt.Errorf("fake gh: unsupported flag %q", a)
		}
		if !inline {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("fake gh: %s needs a value", a)
			}
			i++
			val = args[i]
		}
		switch flag {
		case "-X", "--method":
			method = strings.ToUpper(val)
		case "--hostname":
			host = val
		case "-f", "--raw-field":
			k, v, _ := strings.Cut(val, "=")
			fields[k] = append(fields[k], v)
		case "-F", "--field":
			return nil, fmt.Errorf("fake gh: typed field %s %q: -F expands @file and must never carry cerne's text", flag, val)
		}
	}
	if host != g.host {
		return nil, fmt.Errorf("gh: HTTP 404 on host %s (the repository lives on %s)", host, g.host)
	}
	if method == "" {
		method = "GET"
		if len(fields) > 0 {
			method = "POST"
		}
	}
	u, err := url.Parse(strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("fake gh: endpoint %q: %v", path, err)
	}
	q := u.Query()
	if method == "GET" { // gh sends the fields of a GET as its query
		for k, vs := range fields {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
	}
	g.reqs = append(g.reqs, ghReq{method: method, path: u.Path, query: q})

	rest, ok := strings.CutPrefix(u.Path, "repos/"+g.repo+"/issues")
	if !ok {
		return nil, fmt.Errorf("gh: Not Found (HTTP 404): %s", u.Path)
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
		return nil, fmt.Errorf("gh: Not Found (HTTP 404): %s", u.Path)
	}
	switch {
	case sub == "" && method == "GET":
		return g.answer(iss)
	case sub == "" && method == "PATCH":
		return g.patch(iss, fields)
	case sub == "comments" && method == "POST":
		if len(fields["body"]) != 1 {
			return nil, errors.New("gh: Validation Failed (HTTP 422): body is required")
		}
		iss.Comments = append(iss.Comments, fields["body"][0])
		return json.Marshal(map[string]any{"id": len(iss.Comments), "body": fields["body"][0]})
	}
	return nil, fmt.Errorf("gh: Not Found (HTTP 404): %s %s", method, u.Path)
}

func (g *fakeGH) list(q url.Values) ([]byte, error) {
	state := q.Get("state")
	if state == "" {
		state = "open"
	}
	var want []string
	if l := q.Get("labels"); l != "" {
		want = strings.Split(l, ",")
	}
	out := []map[string]any{}
	for _, n := range slices.Sorted(maps.Keys(g.issues)) {
		iss := g.issues[n]
		if (state == "all" || iss.State == state) && hasAll(iss.Labels, want) {
			out = append(out, g.view(iss))
		}
	}
	return json.Marshal(out)
}

func (g *fakeGH) create(fields map[string][]string) ([]byte, error) {
	if _, bad := fields["labels"]; bad {
		return nil, errors.New(`gh: Invalid request. For 'properties/labels', a string is not an array. (HTTP 422)`)
	}
	if len(fields["title"]) != 1 || fields["title"][0] == "" {
		return nil, errors.New("gh: Validation Failed (HTTP 422): title is required")
	}
	iss := &ghIssue{Title: fields["title"][0], Labels: fields["labels[]"]}
	if b := fields["body"]; len(b) == 1 {
		iss.Body = b[0]
	}
	g.add(iss)
	return g.answer(iss)
}

func (g *fakeGH) patch(iss *ghIssue, fields map[string][]string) ([]byte, error) {
	if _, bad := fields["labels"]; bad {
		return nil, errors.New(`gh: Invalid request. For 'properties/labels', a string is not an array. (HTTP 422)`)
	}
	one := func(k string) (string, bool) {
		v, ok := fields[k]
		if !ok || len(v) != 1 {
			return "", false
		}
		return v[0], true
	}
	reason, hasReason := one("state_reason")
	if hasReason && !slices.Contains([]string{"completed", "not_planned", "reopened"}, reason) {
		return nil, fmt.Errorf("gh: Validation Failed (HTTP 422): state_reason %q", reason)
	}
	if s, ok := one("state"); ok {
		switch s {
		case "closed":
			if !hasReason {
				reason = "completed" // GitHub's default close reason
			}
			iss.State, iss.StateReason = s, reason
		case "open":
			iss.State, iss.StateReason = s, reason // "reopened" only when the adapter says so
		default:
			return nil, fmt.Errorf("gh: Validation Failed (HTTP 422): state %q", s)
		}
	} else if hasReason {
		iss.StateReason = reason
	}
	if v, ok := one("title"); ok {
		iss.Title = v
	}
	if v, ok := one("body"); ok {
		iss.Body = v
	}
	if v, ok := fields["labels[]"]; ok {
		iss.Labels = slices.Clone(v)
	}
	return g.answer(iss)
}

func (g *fakeGH) add(iss *ghIssue) int {
	g.next++
	iss.Number = g.next
	if iss.State == "" {
		iss.State = "open"
	}
	g.issues[g.next] = iss
	return g.next
}

func (g *fakeGH) htmlURL(n int) string {
	kind := "issues"
	if iss := g.issues[n]; iss != nil && iss.PR {
		kind = "pull"
	}
	return fmt.Sprintf("https://%s/%s/%s/%d", g.host, g.repo, kind, n)
}

// view is the issue as the REST API answers it: labels as objects, an empty
// body or reason as null, a PR with its pull_request key.
func (g *fakeGH) view(iss *ghIssue) map[string]any {
	labels := []map[string]string{}
	for _, l := range iss.Labels {
		labels = append(labels, map[string]string{"name": l})
	}
	m := map[string]any{
		"number": iss.Number, "html_url": g.htmlURL(iss.Number), "title": iss.Title,
		"state": iss.State, "labels": labels, "body": nil, "state_reason": nil,
	}
	if iss.Body != "" {
		m["body"] = iss.Body
	}
	if iss.StateReason != "" {
		m["state_reason"] = iss.StateReason
	}
	if iss.PR {
		m["pull_request"] = map[string]any{"url": fmt.Sprintf("https://api.%s/repos/%s/pulls/%d", g.host, g.repo, iss.Number)}
	}
	return m
}

func (g *fakeGH) answer(iss *ghIssue) ([]byte, error) { return json.Marshal(g.view(iss)) }

// ref is the Ref the adapter named "gh" must hand back for issue n.
func (g *fakeGH) ref(n int) tracker.Ref {
	return tracker.Ref{Tracker: "gh", ID: strconv.Itoa(n), URL: g.htmlURL(n)}
}

func (g *fakeGH) count(method string) int {
	n := 0
	for _, r := range g.reqs {
		if r.method == method {
			n++
		}
	}
	return n
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// newGitHub builds the adapter named "gh" for the repository at repoURL.
func newGitHub(t *testing.T, repoURL string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "gh", Kind: "github", URL: repoURL})
	if err != nil {
		t.Fatalf("New(github, %s): %v", repoURL, err)
	}
	return tr
}

func sortedLabels(l []string) []string { return slices.Sorted(slices.Values(l)) }

func publicItem(card string, s domain.FeatureState) tracker.Item {
	return tracker.Item{CardID: card, State: s, Title: "Public title of " + card, Summary: "Public summary of " + card}
}

// F-0449 crit. 2: an unimplemented kind is refused with the exact text; the
// kinds are github only (no fake is registered in this package).
func TestNewUnknownKind(t *testing.T) {
	if got := tracker.KindNames(); !slices.Equal(got, []string{"github"}) {
		t.Fatalf("KindNames() = %v, want [github]", got)
	}
	_, err := tracker.New(tracker.Config{Name: "j", Kind: "jira", URL: "https://jira.example/PROJ"})
	if !errors.Is(err, tracker.ErrUnknownKind) {
		t.Fatalf("New(jira) = %v, want errors.Is ErrUnknownKind", err)
	}
	if got, want := err.Error(), `unknown tracker kind "jira" (supported: github)`; got != want {
		t.Fatalf("New(jira) error = %q, want %q", got, want)
	}
}

// F-0449 crit. 2: a tracker URL must be https, checked before the kind, and
// the same record over https is accepted.
func TestNewRefusesHTTP(t *testing.T) {
	for _, c := range []tracker.Config{
		{Name: "gh", Kind: "github", URL: "http://github.com/o/r"},
		{Name: "gh", Kind: "github", URL: "github.com/o/r"},
		{Name: "gh", Kind: "github", URL: ""},
		{Name: "j", Kind: "jira", URL: "http://jira.example/PROJ"},
	} {
		if _, err := tracker.New(c); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(%+v) = %v, want errors.Is ErrTrackerURL", c, err)
		}
	}
	if _, err := tracker.New(tracker.Config{Name: "gh", Kind: "github", URL: "https://github.com/o/r"}); err != nil {
		t.Fatalf("New(github over https) = %v, want an adapter", err)
	}
}

// F-0449 crit. 2 and 5: host and owner/repo come from the record's URL —
// github.com goes without --hostname, another host is named — and a URL with
// no owner/repo is refused at New, without calling gh.
func TestGitHubURL(t *testing.T) {
	for _, c := range []struct{ url, host, repo string }{
		{"https://github.com/o/r", "github.com", "o/r"},
		{"https://ghe.example/acme/issues", "ghe.example", "acme/issues"},
	} {
		t.Run(c.host, func(t *testing.T) {
			g := newFakeGH(t, c.host, c.repo)
			r, err := newGitHub(t, c.url).Create(context.Background(), publicItem("F-0001", domain.StateCandidate))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if want := g.ref(1); r != want {
				t.Fatalf("Create returned %+v, want %+v", r, want)
			}
			named := slices.Contains(g.calls[0], "--hostname")
			if named != (c.host != "github.com") {
				t.Fatalf("argv %q: --hostname present = %v, want %v", g.calls[0], named, c.host != "github.com")
			}
			if named && !slices.Contains(g.calls[0], c.host) {
				t.Fatalf("argv %q does not name host %s", g.calls[0], c.host)
			}
		})
	}
	for _, bad := range []string{"https://github.com", "https://github.com/", "https://github.com/o"} {
		g := newFakeGH(t, "github.com", "o/r")
		if _, err := tracker.New(tracker.Config{Name: "gh", Kind: "github", URL: bad}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(github, %q) = %v, want errors.Is ErrTrackerURL", bad, err)
		}
		if len(g.calls) != 0 {
			t.Errorf("New(github, %q) called gh: %q", bad, g.calls)
		}
	}
}

// F-0449 crit. 5: the plan's state table, through Create and through Update.
// Update swaps only the cerne:* labels and writes the public title and body.
func TestGitHubStateTable(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		state  domain.FeatureState
		open   bool
		reason string
		label  string
	}{
		{domain.StateCandidate, true, "", "cerne:candidate"},
		{domain.StateApproved, true, "", "cerne:approved"},
		{domain.StateInDevelopment, true, "", "cerne:in_development"},
		{domain.StateImplemented, true, "", "cerne:ready"},
		{domain.StateDone, false, "completed", "cerne:done"},
		{domain.StateRejected, false, "not_planned", "cerne:rejected"},
		{domain.StateSuperseded, false, "not_planned", "cerne:superseded"},
	} {
		t.Run(string(c.state), func(t *testing.T) {
			check := func(t *testing.T, iss *ghIssue, it tracker.Item, wantLabels []string) {
				t.Helper()
				if want := map[bool]string{true: "open", false: "closed"}[c.open]; iss.State != want {
					t.Fatalf("issue state = %s, want %s", iss.State, want)
				}
				if !c.open && iss.StateReason != c.reason {
					t.Fatalf("state_reason = %q, want %q", iss.StateReason, c.reason)
				}
				if got := sortedLabels(iss.Labels); !slices.Equal(got, sortedLabels(wantLabels)) {
					t.Fatalf("labels = %q, want %q", got, wantLabels)
				}
				if iss.Title != it.Title || iss.Body != it.Body() {
					t.Fatalf("title/body = %q / %q, want %q / %q", iss.Title, iss.Body, it.Title, it.Body())
				}
			}
			t.Run("create", func(t *testing.T) {
				g := newFakeGH(t, "github.com", "o/r")
				it := publicItem("F-0007", c.state)
				r, err := newGitHub(t, "https://github.com/o/r").Create(ctx, it)
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				n, _ := strconv.Atoi(r.ID)
				if g.issues[n] == nil || r != g.ref(n) {
					t.Fatalf("Create returned %+v, want the ref of a created issue", r)
				}
				check(t, g.issues[n], it, []string{c.label})
			})
			t.Run("update", func(t *testing.T) {
				g := newFakeGH(t, "github.com", "o/r")
				n := g.add(&ghIssue{Title: "Old", Body: "Old body", Labels: []string{"bug", "cerne:stale"}})
				it := publicItem("F-0007", c.state)
				if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), it); err != nil {
					t.Fatalf("Update: %v", err)
				}
				check(t, g.issues[n], it, []string{"bug", c.label})
			})
		})
	}

	t.Run("a closed issue reopens with state_reason reopened", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := g.add(&ghIssue{Title: "T", State: "closed", StateReason: "completed", Labels: []string{"cerne:done"}})
		if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), publicItem("F-0007", domain.StateInDevelopment)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if iss := g.issues[n]; iss.State != "open" || iss.StateReason != "reopened" || !slices.Equal(iss.Labels, []string{"cerne:in_development"}) {
			t.Fatalf("issue = %+v, want open, reopened, [cerne:in_development]", iss)
		}
	})

	succ := &tracker.Ref{Tracker: "gh", ID: "99", URL: "https://github.com/o/r/issues/99"}
	t.Run("superseded comments the mirrored successor once", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := g.add(&ghIssue{Title: "T"})
		tr := newGitHub(t, "https://github.com/o/r")
		it := publicItem("F-0007", domain.StateSuperseded)
		it.Successor = succ
		for range 2 {
			if err := tr.Update(ctx, g.ref(n), it); err != nil {
				t.Fatalf("Update: %v", err)
			}
		}
		if got := g.issues[n].Comments; !slices.Equal(got, []string{"Superseded by " + succ.URL}) {
			t.Fatalf("comments = %q, want exactly one %q", got, "Superseded by "+succ.URL)
		}
	})
	t.Run("superseded with no mirrored successor writes no comment", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := g.add(&ghIssue{Title: "T"})
		if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), publicItem("F-0007", domain.StateSuperseded)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got := g.issues[n].Comments; len(got) != 0 {
			t.Fatalf("comments = %q, want none", got)
		}
	})
	t.Run("superseded on an issue already closed writes no comment", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := g.add(&ghIssue{Title: "T", State: "closed", StateReason: "not_planned", Labels: []string{"cerne:rejected"}})
		it := publicItem("F-0007", domain.StateSuperseded)
		it.Successor = succ
		if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), it); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got := g.issues[n].Comments; len(got) != 0 {
			t.Fatalf("comments = %q, want none: the issue was not open", got)
		}
	})
	t.Run("an unchanged item sends no PATCH", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := g.add(&ghIssue{Title: "T"})
		tr := newGitHub(t, "https://github.com/o/r")
		it := publicItem("F-0007", domain.StateApproved)
		if err := tr.Update(ctx, g.ref(n), it); err != nil {
			t.Fatalf("Update: %v", err)
		}
		before := g.count("PATCH")
		if err := tr.Update(ctx, g.ref(n), it); err != nil {
			t.Fatalf("second Update: %v", err)
		}
		if after := g.count("PATCH"); before == 0 || after != before {
			t.Fatalf("PATCH calls = %d then %d, want the first Update to patch and the second to send none", before, after)
		}
	})
	t.Run("an empty title keeps the outside title and body", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := g.add(&ghIssue{Title: "Outside title", Body: "Outside body", Labels: []string{"question"}})
		if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), tracker.Item{CardID: "F-0007", State: domain.StateApproved}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		iss := g.issues[n]
		if iss.Title != "Outside title" || iss.Body != "Outside body" || !slices.Equal(sortedLabels(iss.Labels), []string{"cerne:approved", "question"}) {
			t.Fatalf("issue = %+v, want the outside text kept and labels [cerne:approved question]", iss)
		}
	})
}

// F-0449 crit. 7: with mirror.close_on = "merge" an issue found closed stays
// closed — neither its state nor its close reason changes, only the labels —
// and NoReopen never stops an open issue from closing.
func TestGitHubNoReopen(t *testing.T) {
	ctx := context.Background()
	closedByMerge := func(g *fakeGH) int {
		return g.add(&ghIssue{Title: "T", State: "closed", StateReason: "completed", Labels: []string{"bug", "cerne:in_development"}})
	}
	for _, c := range []struct {
		state domain.FeatureState
		label string
	}{
		{domain.StateImplemented, "cerne:ready"},
		{domain.StateInDevelopment, "cerne:in_development"},
		{domain.StateRejected, "cerne:rejected"},
	} {
		t.Run(string(c.state), func(t *testing.T) {
			g := newFakeGH(t, "github.com", "o/r")
			n := closedByMerge(g)
			it := publicItem("F-0007", c.state)
			it.NoReopen = true
			if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), it); err != nil {
				t.Fatalf("Update: %v", err)
			}
			iss := g.issues[n]
			if iss.State != "closed" || iss.StateReason != "completed" {
				t.Fatalf("issue = %s/%s, want closed/completed untouched", iss.State, iss.StateReason)
			}
			if got := sortedLabels(iss.Labels); !slices.Equal(got, sortedLabels([]string{"bug", c.label})) {
				t.Fatalf("labels = %q, want [bug %s]", got, c.label)
			}
		})
	}
	t.Run("without NoReopen the issue reopens", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := closedByMerge(g)
		if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), publicItem("F-0007", domain.StateImplemented)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if iss := g.issues[n]; iss.State != "open" {
			t.Fatalf("issue state = %s, want open (NoReopen unset)", iss.State)
		}
	})
	t.Run("NoReopen still closes an open issue", func(t *testing.T) {
		g := newFakeGH(t, "github.com", "o/r")
		n := g.add(&ghIssue{Title: "T", Labels: []string{"cerne:ready"}})
		it := publicItem("F-0007", domain.StateDone)
		it.NoReopen = true
		if err := newGitHub(t, "https://github.com/o/r").Update(ctx, g.ref(n), it); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if iss := g.issues[n]; iss.State != "closed" || iss.StateReason != "completed" {
			t.Fatalf("issue = %s/%s, want closed/completed", iss.State, iss.StateReason)
		}
	})
}

// F-0449 crit. 6: ListOpen asks for the open issues, 100 per page, filtered by
// the label when there is one, and never returns a pull request.
func TestGitHubListOpenSkipsPullRequests(t *testing.T) {
	ctx := context.Background()
	g := newFakeGH(t, "github.com", "o/r")
	a := g.add(&ghIssue{Title: "Outside A", Body: "Body A"})
	g.add(&ghIssue{Title: "A pull request", Body: "diff", PR: true})
	g.add(&ghIssue{Title: "Closed", State: "closed", StateReason: "completed"})
	b := g.add(&ghIssue{Title: "Outside B", Labels: []string{"triage"}}) // empty body: null on the wire
	g.add(&ghIssue{Title: "Labeled PR", PR: true, Labels: []string{"triage"}})
	tr := newGitHub(t, "https://github.com/o/r")

	got, err := tr.ListOpen(ctx, "")
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	want := []tracker.Remote{{Ref: g.ref(a), Title: "Outside A", Body: "Body A"}, {Ref: g.ref(b), Title: "Outside B"}}
	slices.SortFunc(got, func(x, y tracker.Remote) int { return strings.Compare(x.ID, y.ID) })
	if !slices.Equal(got, want) {
		t.Fatalf("ListOpen(\"\") = %+v, want %+v", got, want)
	}
	last := g.reqs[len(g.reqs)-1]
	if last.method != "GET" || last.query.Get("state") != "open" || last.query.Get("per_page") != "100" || last.query.Has("labels") {
		t.Fatalf("request = %s %s?%s, want GET with state=open, per_page=100 and no labels", last.method, last.path, last.query.Encode())
	}

	got, err = tr.ListOpen(ctx, "triage")
	if err != nil {
		t.Fatalf("ListOpen(triage): %v", err)
	}
	if want := []tracker.Remote{{Ref: g.ref(b), Title: "Outside B"}}; !slices.Equal(got, want) {
		t.Fatalf("ListOpen(triage) = %+v, want %+v", got, want)
	}
	if last := g.reqs[len(g.reqs)-1]; last.query.Get("labels") != "triage" {
		t.Fatalf("request query %q, want labels=triage", last.query.Encode())
	}
}

// F-0449 crit. 7 and 8: the whole trailer — #N for the commit_url's own
// repository, owner/repo#N for another repository on the same forge, the
// issue URL otherwise — with Refs, or Closes when closing.
func TestCommitRefForms(t *testing.T) {
	for _, c := range []struct {
		name, trackerURL, commitURL string
		closing                     bool
		want                        string
	}{
		{"same repository", "https://github.com/o/r", "https://github.com/o/r/commit/{sha}", false, "Refs: #12"},
		{"same repository, closing", "https://github.com/o/r", "https://github.com/o/r/commit/{sha}", true, "Closes: #12"},
		{"other repository, same forge", "https://github.com/o/issues", "https://github.com/o/r/commit/{sha}", false, "Refs: o/issues#12"},
		{"other repository, same forge, closing", "https://github.com/o/issues", "https://github.com/o/r/commit/{sha}", true, "Closes: o/issues#12"},
		{"other forge", "https://github.com/o/r", "https://gitlab.com/o/r/-/commit/{sha}", false, "Refs: https://github.com/o/r/issues/12"},
		{"other forge, closing", "https://github.com/o/r", "https://gitlab.com/o/r/-/commit/{sha}", true, "Closes: https://github.com/o/r/issues/12"},
		{"same path on another host", "https://ghe.example/o/r", "https://github.com/o/r/commit/{sha}", false, "Refs: https://ghe.example/o/r/issues/12"},
		{"enterprise host, same repository", "https://ghe.example/o/r", "https://ghe.example/o/r/commit/{sha}", false, "Refs: #12"},
		// No [delivery] commit_url: no repository to be relative to, so only the URL is unambiguous.
		{"no commit_url", "https://github.com/o/r", "", false, "Refs: https://github.com/o/r/issues/12"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newFakeGH(t, "github.com", "o/r")
			r := tracker.Ref{Tracker: "gh", ID: "12", URL: c.trackerURL + "/issues/12"}
			if got := newGitHub(t, c.trackerURL).CommitRef(r, c.commitURL, c.closing); got != c.want {
				t.Fatalf("CommitRef = %q, want %q", got, c.want)
			}
			if len(g.calls) != 0 {
				t.Fatalf("CommitRef called gh: %q (it is pure)", g.calls)
			}
		})
	}
}

// F-0449 crit. 4 (only the public text leaves, as text): every field reaches
// gh as a raw -f, so a title or summary starting with @ is sent as written,
// never read from a file.
func TestGitHubRawFieldsOnly(t *testing.T) {
	ctx := context.Background()
	g := newFakeGH(t, "github.com", "o/r")
	tr := newGitHub(t, "https://github.com/o/r")
	it := tracker.Item{CardID: "F-0007", State: domain.StateCandidate, Title: "@/etc/passwd", Summary: "@~/.ssh/id_rsa"}
	r, err := tr.Create(ctx, it)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	gone := it
	gone.State, gone.Successor = domain.StateSuperseded, &tracker.Ref{Tracker: "gh", ID: "9", URL: "@/etc/hosts"}
	if err := tr.Update(ctx, r, gone); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := tr.ListOpen(ctx, "@label"); err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	for _, argv := range g.calls {
		for _, a := range argv {
			if a == "-F" || a == "--field" || strings.HasPrefix(a, "--field=") || a == "--input" || strings.HasPrefix(a, "--input=") {
				t.Fatalf("argv %q carries %q: only -f is allowed", argv, a)
			}
		}
	}
	n, _ := strconv.Atoi(r.ID)
	iss := g.issues[n]
	if iss.Title != "@/etc/passwd" || iss.Body != it.Body() {
		t.Fatalf("issue title/body = %q / %q, want the literal %q / %q", iss.Title, iss.Body, it.Title, it.Body())
	}
	if !slices.Equal(iss.Comments, []string{"Superseded by @/etc/hosts"}) {
		t.Fatalf("comments = %q, want the literal successor URL", iss.Comments)
	}
}

// F-0449 crit. 9: the GitHub adapter passes the contract suite the fake passes.
func TestGitHubContract(t *testing.T) {
	var g *fakeGH
	trackertest.RunContract(t, trackertest.Harness{
		New: func(t *testing.T) tracker.IssueTracker {
			g = newFakeGH(t, "github.com", "o/r")
			return newGitHub(t, "https://github.com/o/r")
		},
		Open: func(_ *testing.T, title, body, label string) tracker.Ref {
			var labels []string
			if label != "" {
				labels = []string{label}
			}
			return g.ref(g.add(&ghIssue{Title: title, Body: body, Labels: labels}))
		},
		ClosesOnMerge: true,
	})
}

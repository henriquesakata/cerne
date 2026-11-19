package tracker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// unitGH emulates the slice of the GitHub issues API the adapter uses, from
// the gh argv. The acceptance tests carry their own emulator; this one backs
// the edge cases they do not name.
type unitGH struct {
	issues map[int]*unitIssue
	calls  [][]string
}

type unitIssue struct {
	Title, Body, State, Reason string
	Labels, Comments           []string
	PR                         bool
}

func installUnitGH(t *testing.T) *unitGH {
	t.Helper()
	g := &unitGH{issues: map[int]*unitIssue{}}
	prev := forge.RunCLI
	forge.RunCLI = g.run
	t.Cleanup(func() { forge.RunCLI = prev })
	return g
}

func (g *unitGH) seed(iss *unitIssue) tracker.Ref {
	n := len(g.issues) + 1
	if iss.State == "" {
		iss.State = "open"
	}
	g.issues[n] = iss
	return tracker.Ref{Tracker: "gh", ID: strconv.Itoa(n), URL: g.url(n)}
}

func (g *unitGH) url(n int) string { return fmt.Sprintf("https://github.com/o/r/issues/%d", n) }

func (g *unitGH) run(_ context.Context, name string, args ...string) ([]byte, error) {
	g.calls = append(g.calls, args)
	if name != "gh" || len(args) == 0 || args[0] != "api" {
		return nil, fmt.Errorf("unexpected command %s %v", name, args)
	}
	method, endpoint, fields := "GET", "", map[string][]string{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--hostname":
			i++
		case "-X":
			i++
			method = args[i]
		case "-f":
			i++
			k, v, _ := strings.Cut(args[i], "=")
			fields[k] = append(fields[k], v)
		default:
			endpoint = args[i]
		}
	}
	path, query, _ := strings.Cut(endpoint, "?")
	p := strings.Split(path, "/") // repos o r issues [n [comments]]
	if len(p) < 4 || p[0] != "repos" || p[3] != "issues" {
		return nil, fmt.Errorf("unexpected endpoint %q", endpoint)
	}
	switch {
	case len(p) == 4 && method == "GET":
		var out []map[string]any
		for _, n := range slices.Sorted(maps.Keys(g.issues)) {
			iss := g.issues[n]
			if iss.State == "open" && (!strings.Contains(query, "labels=") || slices.ContainsFunc(iss.Labels, func(l string) bool { return strings.Contains(query, "labels="+l) })) {
				out = append(out, g.view(n))
			}
		}
		return json.Marshal(out)
	case len(p) == 4 && method == "POST":
		n := len(g.issues) + 1
		g.issues[n] = &unitIssue{Title: fields["title"][0], Body: fields["body"][0], State: "open", Labels: fields["labels[]"]}
		return json.Marshal(g.view(n))
	}
	n, _ := strconv.Atoi(p[4])
	iss, ok := g.issues[n]
	if !ok {
		return nil, fmt.Errorf("gh: HTTP 404")
	}
	switch {
	case len(p) == 5 && method == "GET":
	case len(p) == 5 && method == "PATCH":
		for k, v := range fields {
			switch k {
			case "title":
				iss.Title = v[0]
			case "body":
				iss.Body = v[0]
			case "state":
				iss.State = v[0]
			case "state_reason":
				iss.Reason = v[0]
			case "labels[]":
				iss.Labels = v
			}
		}
	case len(p) == 6 && p[5] == "comments" && method == "POST":
		iss.Comments = append(iss.Comments, fields["body"][0])
		return []byte(`{}`), nil
	default:
		return nil, fmt.Errorf("unexpected %s %q", method, endpoint)
	}
	return json.Marshal(g.view(n))
}

func (g *unitGH) view(n int) map[string]any {
	iss := g.issues[n]
	var labels []map[string]string
	for _, l := range iss.Labels {
		labels = append(labels, map[string]string{"name": l})
	}
	v := map[string]any{"number": n, "html_url": g.url(n), "title": iss.Title, "body": iss.Body,
		"state": iss.State, "state_reason": iss.Reason, "labels": labels}
	if iss.PR {
		v["pull_request"] = map[string]string{"url": "x"}
	}
	return v
}

func (g *unitGH) patches() int {
	n := 0
	for _, c := range g.calls {
		if slices.Contains(c, "PATCH") {
			n++
		}
	}
	return n
}

func unitTracker(t *testing.T, url string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "gh", Kind: "github", URL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tr
}

func unitItem(s domain.FeatureState) tracker.Item {
	return tracker.Item{CardID: "F-0001", State: s, Title: "Public title", Summary: "Public summary"}
}

// TestDevEmulatorContract runs the contract suite over this file's emulator.
func TestDevEmulatorContract(t *testing.T) {
	var g *unitGH
	trackertest.RunContract(t, trackertest.Harness{
		New: func(t *testing.T) tracker.IssueTracker {
			g = installUnitGH(t)
			return unitTracker(t, "https://github.com/o/r")
		},
		Open: func(_ *testing.T, title, body, label string) tracker.Ref {
			var labels []string
			if label != "" {
				labels = []string{label}
			}
			return g.seed(&unitIssue{Title: title, Body: body, Labels: labels})
		},
		ClosesOnMerge: true,
	})
}

func TestGitHubUpdateSendsOnlyWhatDiffers(t *testing.T) {
	g := installUnitGH(t)
	tr := unitTracker(t, "https://github.com/o/r")
	ctx := context.Background()
	r, err := tr.Create(ctx, unitItem(domain.StateInDevelopment))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, r, unitItem(domain.StateInDevelopment)); err != nil {
		t.Fatal(err)
	}
	if n := g.patches(); n != 0 {
		t.Fatalf("an Update with nothing new sent %d PATCH, want 0", n)
	}
	if err := tr.Update(ctx, r, unitItem(domain.StateImplemented)); err != nil {
		t.Fatal(err)
	}
	last := g.calls[len(g.calls)-1]
	if g.patches() != 1 || !slices.Contains(last, "labels[]=cerne:ready") || slices.ContainsFunc(last, func(a string) bool {
		return strings.HasPrefix(a, "title=") || strings.HasPrefix(a, "body=") || strings.HasPrefix(a, "state")
	}) {
		t.Fatalf("implemented sent %v, want one PATCH with only the cerne:ready label", last)
	}
}

func TestGitHubKeepsForeignLabels(t *testing.T) {
	g := installUnitGH(t)
	r := g.seed(&unitIssue{Title: "Outside", Body: "b", Labels: []string{"bug", "cerne:approved", "help wanted"}})
	if err := unitTracker(t, "https://github.com/o/r").Update(context.Background(), r, tracker.Item{CardID: "I-0001", State: domain.StateInDevelopment}); err != nil {
		t.Fatal(err)
	}
	if got, want := g.issues[1].Labels, []string{"bug", "help wanted", "cerne:in_development"}; !slices.Equal(got, want) {
		t.Fatalf("labels = %q, want %q", got, want)
	}
}

func TestGitHubSupersededCommentsOnce(t *testing.T) {
	g := installUnitGH(t)
	tr := unitTracker(t, "https://github.com/o/r")
	ctx := context.Background()
	r, err := tr.Create(ctx, unitItem(domain.StateImplemented))
	if err != nil {
		t.Fatal(err)
	}
	it := unitItem(domain.StateSuperseded)
	it.Successor = &tracker.Ref{Tracker: "gh", ID: "7", URL: "https://github.com/o/r/issues/7"}
	for range 2 {
		if err := tr.Update(ctx, r, it); err != nil {
			t.Fatal(err)
		}
	}
	iss := g.issues[1]
	if iss.State != "closed" || iss.Reason != "not_planned" || !slices.Equal(iss.Comments, []string{"Superseded by " + it.Successor.URL}) {
		t.Fatalf("issue = %+v, want closed not_planned with one successor comment", iss)
	}
}

func TestGitHubCreateLandsInClosedState(t *testing.T) {
	g := installUnitGH(t)
	if _, err := unitTracker(t, "https://github.com/o/r").Create(context.Background(), unitItem(domain.StateDone)); err != nil {
		t.Fatal(err)
	}
	if iss := g.issues[1]; iss.State != "closed" || iss.Reason != "completed" || !slices.Equal(iss.Labels, []string{"cerne:done"}) {
		t.Fatalf("issue = %+v, want closed completed with cerne:done", iss)
	}
}

func TestGitHubEnterpriseHostname(t *testing.T) {
	for _, tc := range []struct{ url, hostname string }{
		{"https://github.com/o/r", ""},
		{"https://ghe.example:8443/o/r", "ghe.example:8443"},
	} {
		g := installUnitGH(t)
		if _, err := unitTracker(t, tc.url).ListOpen(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		i := slices.Index(g.calls[0], "--hostname")
		if got := ""; i >= 0 {
			got = g.calls[0][i+1]
			if got != tc.hostname {
				t.Fatalf("%s: --hostname %q, want %q", tc.url, got, tc.hostname)
			}
		} else if tc.hostname != "" {
			t.Fatalf("%s: no --hostname, want %q", tc.url, tc.hostname)
		}
	}
}

func TestGitHubListOpenEscapesLabel(t *testing.T) {
	g := installUnitGH(t)
	if _, err := unitTracker(t, "https://github.com/o/r").ListOpen(context.Background(), "good first&issue"); err != nil {
		t.Fatal(err)
	}
	if got := g.calls[0][len(g.calls[0])-1]; got != "repos/o/r/issues?state=open&per_page=100&labels=good+first%26issue" {
		t.Fatalf("endpoint = %q", got)
	}
}

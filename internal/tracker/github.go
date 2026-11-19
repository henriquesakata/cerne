package tracker

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
)

// github mirrors cards as issues on one GitHub repository through `gh api`,
// with the user's gh auth: no token in .cernerc. Every field goes with -f
// (raw string), never -F, which would read an @file.
type github struct {
	name string
	repo forge.Repo
}

// ghPath is the /<owner>/<repo> of a mirror.trackers url.
var ghPath = regexp.MustCompile(`^/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)/?$`)

func newGitHub(c Config) (IssueTracker, error) {
	u, err := url.Parse(c.URL)
	if err == nil {
		// gh sends the user's token for the host it reaches: never an IP
		// literal or localhost, which name a service on this machine or
		// network rather than a GitHub. A DNS host stays allowed, for GitHub
		// Enterprise.
		if h := u.Hostname(); net.ParseIP(h) != nil || strings.EqualFold(h, "localhost") {
			return nil, fmt.Errorf("%w: github tracker host %q is an IP address or localhost; use the GitHub host name", ErrTrackerURL, h)
		}
	}
	if err == nil && u.RawQuery == "" && u.Fragment == "" { // tracker.New already refused a user and password
		if m := ghPath.FindStringSubmatch(u.Path); m != nil && !dots(m[1]) && !dots(m[2]) {
			return github{name: c.Name, repo: forge.Repo{Host: u.Host, Path: m[1] + "/" + m[2]}}, nil
		}
	}
	return nil, fmt.Errorf("%w %q: want https://<host>/<owner>/<repo>", ErrTrackerURL, redactURL(c.URL))
}

func dots(seg string) bool { return strings.Trim(seg, ".") == "" }

// ghIssue is the part of a GitHub issue the adapter reads.
type ghIssue struct {
	Number      int    `json:"number"`
	HTMLURL     string `json:"html_url"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`        // open | closed
	StateReason string `json:"state_reason"` // completed | not_planned | reopened
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *struct{} `json:"pull_request"` // the issues API lists PRs too
}

func (g github) api(ctx context.Context, v any, args ...string) error {
	a := append([]string{"api"}, forge.HostArgs(g.repo.Host, "github.com")...)
	return forge.CallJSON(ctx, v, "gh", append(a, args...)...)
}

func (g github) issues() string { return "repos/" + g.repo.Path + "/issues" }

func (g github) ref(iss ghIssue) Ref {
	return Ref{Tracker: g.name, ID: strconv.Itoa(iss.Number), URL: iss.HTMLURL}
}

// Create opens the issue, then moves it to it.State when that is a closed one.
func (g github) Create(ctx context.Context, it Item) (Ref, error) {
	var iss ghIssue
	if err := g.api(ctx, &iss, "-X", "POST", g.issues(),
		"-f", "title="+it.Title, "-f", "body="+it.Body(), "-f", "labels[]="+stateLabel(it.State)); err != nil {
		return Ref{}, err
	}
	r := g.ref(iss)
	return r, g.apply(ctx, iss, it) // a Ref with an error: the issue exists, its closed state did not land
}

// Update reads the issue and writes only what differs from it.
func (g github) Update(ctx context.Context, r Ref, it Item) error {
	if !digits(r.ID) {
		return fmt.Errorf("%w: issue number %q", errBadRef, r.ID)
	}
	var iss ghIssue
	if err := g.api(ctx, &iss, g.issues()+"/"+r.ID); err != nil {
		return err
	}
	return g.apply(ctx, iss, it)
}

// apply PATCHes cur toward it, in one call or none. The superseded comment
// goes only with the call that closes the issue, so a sync never repeats it.
func (g github) apply(ctx context.Context, cur ghIssue, it Item) error {
	var f []string
	if it.Title != "" && cur.Title != it.Title {
		f = append(f, "title="+it.Title)
	}
	if it.Title != "" && cur.Body != it.Body() {
		f = append(f, "body="+it.Body())
	}
	if labels, changed := withStateLabel(cur, stateLabel(it.State)); changed {
		for _, l := range labels {
			f = append(f, "labels[]="+l)
		}
	}
	reason, closed := closeReason(it.State), cur.State == "closed"
	closing := false
	switch {
	case closed && it.NoReopen: // the merge closed it: only the labels move
	case reason == "" && closed:
		f = append(f, "state=open", "state_reason=reopened")
	case reason != "" && (!closed || cur.StateReason != reason):
		f = append(f, "state=closed", "state_reason="+reason)
		closing = !closed
	}
	if len(f) > 0 {
		args := []string{"-X", "PATCH", g.issues() + "/" + strconv.Itoa(cur.Number)}
		for _, kv := range f {
			args = append(args, "-f", kv)
		}
		if err := g.api(ctx, &ghIssue{}, args...); err != nil {
			return err
		}
	}
	if closing && it.State == domain.StateSuperseded && it.Successor != nil {
		path := g.issues() + "/" + strconv.Itoa(cur.Number) + "/comments"
		return g.api(ctx, &struct{}{}, "-X", "POST", path, "-f", "body=Superseded by "+it.Successor.URL)
	}
	return nil
}

// ListOpen lists up to 100 open issues, PRs dropped. ponytail: one page; paginate when a tracker outgrows it.
func (g github) ListOpen(ctx context.Context, label string) ([]Remote, error) {
	q := "?state=open&per_page=100"
	if label != "" {
		q += "&labels=" + url.QueryEscape(label)
	}
	var list []ghIssue
	if err := g.api(ctx, &list, g.issues()+q); err != nil {
		return nil, err
	}
	var out []Remote
	for _, iss := range list {
		if iss.PullRequest == nil {
			out = append(out, Remote{Ref: g.ref(iss), Title: iss.Title, Body: iss.Body})
		}
	}
	return out, nil
}

// CommitRef cites the issue the shortest way GitHub links it from a commit in
// the commitURL repository: #N in the same repository, owner/repo#N on the
// same host, the issue URL anywhere else.
func (g github) CommitRef(r Ref, commitURL string, closing bool) string {
	if !digits(r.ID) {
		return ""
	}
	word := "Refs"
	if closing {
		word = "Closes"
	}
	ref := r.URL
	if code, _, ok := forge.ParseCommitURL(commitURL); ok && strings.EqualFold(code.Host, g.repo.Host) {
		ref = g.repo.Path + "#" + r.ID
		if strings.EqualFold(code.Path, g.repo.Path) {
			ref = "#" + r.ID
		}
	}
	return word + ": " + ref
}

// Owns reports whether r is an issue of this repository on this host: the
// link a card holds is checked before a tracker re-pointed since could act on
// issue #N of another repository.
func (g github) Owns(r Ref) bool {
	u, err := url.Parse(r.URL)
	return err == nil && u.Scheme == "https" && u.User == nil && digits(r.ID) && strings.EqualFold(u.Host, g.repo.Host) &&
		strings.EqualFold(strings.TrimSuffix(u.Path, "/"), "/"+g.repo.Path+"/issues/"+r.ID)
}

// digits reports a non-empty run of ASCII digits: a GitHub issue number.
func digits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// stateLabel is the cerne:<state> label; implemented reads as ready, like the board.
func stateLabel(s domain.FeatureState) string {
	if s == domain.StateImplemented {
		return "cerne:ready"
	}
	return "cerne:" + string(s)
}

// closeReason is the state_reason a card state closes its issue with; "" keeps it open.
func closeReason(s domain.FeatureState) string {
	switch s {
	case domain.StateDone:
		return "completed"
	case domain.StateRejected, domain.StateSuperseded:
		return "not_planned"
	}
	return ""
}

// withStateLabel swaps every cerne:* label of iss for want, keeping the others
// in order, and reports whether that changes anything.
func withStateLabel(iss ghIssue, want string) ([]string, bool) {
	var keep, ours []string
	for _, l := range iss.Labels {
		if strings.HasPrefix(l.Name, "cerne:") {
			ours = append(ours, l.Name)
		} else {
			keep = append(keep, l.Name)
		}
	}
	return append(keep, want), !slices.Equal(ours, []string{want})
}

package tracker

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
)

// gitlab mirrors cards as issues on one GitLab project through `glab api`,
// with the user's glab auth: no token in .cernerc. Every field goes with -f
// (raw string), never -F, which would read an @file.
type gitlab struct {
	name string
	repo forge.Repo // Path: group[/subgroup…]/project
}

// glPath is the /<group>[/<subgroup>…]/<project> of a mirror.trackers url. No
// segment starts with "-": "/-/" opens a page of the project (.../p/-/issues),
// it is not part of the project path.
var glPath = regexp.MustCompile(`^/((?:[A-Za-z0-9_.][A-Za-z0-9._-]*/)+[A-Za-z0-9_.][A-Za-z0-9._-]*)/?$`)

func newGitLab(c Config) (IssueTracker, error) {
	u, err := url.Parse(c.URL)
	if err == nil {
		// glab sends the user's token for the host it reaches: never a local
		// or numeric host, as for github.
		if h := u.Hostname(); localHost(h) {
			return nil, fmt.Errorf("%w: gitlab tracker host %q is an IP address or a local or numeric name; use the GitLab host name", ErrTrackerURL, h)
		}
	}
	if err == nil && u.RawQuery == "" && u.Fragment == "" { // tracker.New already refused a user and password
		if m := glPath.FindStringSubmatch(u.Path); m != nil && !slices.ContainsFunc(strings.Split(m[1], "/"), dots) {
			return gitlab{name: c.Name, repo: forge.Repo{Host: u.Host, Path: m[1]}}, nil
		}
	}
	return nil, fmt.Errorf("%w %q: want https://<host>/<group>/<project>", ErrTrackerURL, redactURL(c.URL))
}

// glIssue is the part of a GitLab issue the adapter reads. The issues API
// lists no merge requests, and labels come as names.
type glIssue struct {
	IID         int      `json:"iid"` // the number in the project; "id" is global
	WebURL      string   `json:"web_url"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"` // opened | closed
	Labels      []string `json:"labels"`
}

// api always names the host: with no --hostname, glab calls the host of the
// current directory's repository, which need not be the tracker's.
func (g gitlab) api(ctx context.Context, v any, args ...string) error {
	return forge.CallJSON(ctx, v, "glab", append([]string{"api", "--hostname", g.repo.Host}, args...)...)
}

func (g gitlab) issues() string { return "projects/" + url.PathEscape(g.repo.Path) + "/issues" }

func (g gitlab) ref(iss glIssue) Ref {
	return Ref{Tracker: g.name, ID: strconv.Itoa(iss.IID), URL: iss.WebURL}
}

// Create opens the issue, then moves it to it.State when that is a closed one.
func (g gitlab) Create(ctx context.Context, it Item) (Ref, error) {
	var iss glIssue
	if err := g.api(ctx, &iss, "-X", "POST", g.issues(),
		"-f", "title="+it.Title, "-f", "description="+it.Body(), "-f", "labels="+stateLabel(it.State)); err != nil {
		return Ref{}, err
	}
	return g.ref(iss), g.apply(ctx, iss, it) // a Ref with an error: the issue exists, its closed state did not land
}

// Update reads the issue and writes only what differs from it.
func (g gitlab) Update(ctx context.Context, r Ref, it Item) error {
	if !digits(r.ID) {
		return fmt.Errorf("%w: issue iid %q", errBadRef, r.ID)
	}
	var iss glIssue
	if err := g.api(ctx, &iss, g.issues()+"/"+r.ID); err != nil {
		return err
	}
	return g.apply(ctx, iss, it)
}

// apply PUTs cur toward it, in one call or none. GitLab has no close reason:
// the cerne:<state> label says why the issue closed, so only the cerne:*
// labels move and the others stay. The superseded comment goes only with the
// call that closes the issue, so a sync never repeats it.
func (g gitlab) apply(ctx context.Context, cur glIssue, it Item) error {
	var f []string
	if it.Title != "" && cur.Title != it.Title {
		f = append(f, "title="+it.Title)
	}
	if it.Title != "" && cur.Description != it.Body() {
		f = append(f, "description="+it.Body())
	}
	want := stateLabel(it.State)
	var stale []string
	for _, l := range cur.Labels {
		// remove_labels is comma-joined: a name with a comma could name others.
		if strings.HasPrefix(l, "cerne:") && l != want && !strings.Contains(l, ",") {
			stale = append(stale, l)
		}
	}
	if len(stale) > 0 {
		f = append(f, "remove_labels="+strings.Join(stale, ","))
	}
	if !slices.Contains(cur.Labels, want) {
		f = append(f, "add_labels="+want)
	}
	closed, wantClosed := cur.State == "closed", closeReason(it.State) != ""
	closing := false
	switch {
	case closed && it.NoReopen: // the merge closed it: only the labels move
	case closed && !wantClosed:
		f = append(f, "state_event=reopen")
	case !closed && wantClosed:
		f = append(f, "state_event=close")
		closing = true
	}
	if len(f) > 0 {
		args := []string{"-X", "PUT", g.issues() + "/" + strconv.Itoa(cur.IID)}
		for _, kv := range f {
			args = append(args, "-f", kv)
		}
		if err := g.api(ctx, &glIssue{}, args...); err != nil {
			return err
		}
	}
	if closing && it.State == domain.StateSuperseded && it.Successor != nil {
		path := g.issues() + "/" + strconv.Itoa(cur.IID) + "/notes"
		return g.api(ctx, &struct{}{}, "-X", "POST", path, "-f", "body=Superseded by "+it.Successor.URL)
	}
	return nil
}

// ListOpen lists up to 100 open issues. ponytail: one page; paginate when a tracker outgrows it.
func (g gitlab) ListOpen(ctx context.Context, label string) ([]Remote, error) {
	q := "?state=opened&per_page=100"
	if label != "" {
		q += "&labels=" + url.QueryEscape(label)
	}
	var list []glIssue
	if err := g.api(ctx, &list, g.issues()+q); err != nil {
		return nil, err
	}
	var out []Remote
	for _, iss := range list {
		out = append(out, Remote{Ref: g.ref(iss), Title: iss.Title, Body: iss.Description})
	}
	return out, nil
}

// CommitRef cites the issue the shortest way GitLab links it from a commit in
// the commitURL project: #N in the same project, group/project#N on the same
// host, the issue URL anywhere else.
func (g gitlab) CommitRef(r Ref, commitURL string, closing bool) string {
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

// Owns reports whether r is an issue of this project on this host, subgroups
// included: the link a card holds is checked before a tracker re-pointed
// since could act on issue #N of another project. The issue page is
// /-/issues/N, or /-/work_items/N on gitlab.com 19.x.
func (g gitlab) Owns(r Ref) bool {
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || strings.ContainsAny(r.URL, "?#") || !digits(r.ID) || nonASCII(u.Host) || !strings.EqualFold(u.Host, g.repo.Host) {
		return false
	}
	p := strings.TrimSuffix(u.EscapedPath(), "/")
	return strings.EqualFold(p, "/"+g.repo.Path+"/-/issues/"+r.ID) || strings.EqualFold(p, "/"+g.repo.Path+"/-/work_items/"+r.ID)
}

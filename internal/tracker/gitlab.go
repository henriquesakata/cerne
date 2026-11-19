package tracker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"

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
		// glab sends the user's token for the host it reaches: never an IP
		// literal or localhost, as for github.
		if h := u.Hostname(); net.ParseIP(h) != nil || strings.EqualFold(h, "localhost") {
			return nil, fmt.Errorf("%w: gitlab tracker host %q is an IP address or localhost; use the GitLab host name", ErrTrackerURL, h)
		}
	}
	if err == nil && u.RawQuery == "" && u.Fragment == "" { // tracker.New already refused a user and password
		if m := glPath.FindStringSubmatch(u.Path); m != nil && !slices.ContainsFunc(strings.Split(m[1], "/"), dots) {
			return gitlab{name: c.Name, repo: forge.Repo{Host: u.Host, Path: m[1]}}, nil
		}
	}
	return nil, fmt.Errorf("%w %q: want https://<host>/<group>/<project>", ErrTrackerURL, redactURL(c.URL))
}

var errGitLabTODO = errors.New("gitlab tracker: not implemented yet")

func (g gitlab) Create(context.Context, Item) (Ref, error)          { return Ref{}, errGitLabTODO }
func (g gitlab) Update(context.Context, Ref, Item) error            { return errGitLabTODO }
func (g gitlab) ListOpen(context.Context, string) ([]Remote, error) { return nil, errGitLabTODO }
func (g gitlab) CommitRef(Ref, string, bool) string                 { return "" }
func (g gitlab) Owns(Ref) bool                                      { return false }

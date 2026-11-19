// Package tracker mirrors cerne cards into an external issue tracker (F-0449,
// E-0002). The core hands each adapter cerne's own state in an Item; the
// adapter maps it onto its tracker (labels, close reason, list, status
// category). A new tracker is one entry in Kinds plus its <kind>.go: the core
// never changes.
package tracker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/henriquesakata/cerne/internal/domain"
)

// Config is one mirror.trackers record; Label is optional.
type Config struct{ Name, Kind, URL, Label string }

// Ref points at one issue on one configured tracker.
type Ref struct {
	Tracker string // Config.Name
	ID      string // "12" (GitHub), "PROJ-123" (Jira), shortLink (Trello)
	URL     string // the issue's web page
}

// Item is everything cerne publishes about a card. Nothing else leaves the backend.
type Item struct {
	CardID    string
	State     domain.FeatureState
	Title     string // public title from the opt-in; "" = issue came from outside: keep its title and body
	Summary   string // public summary from the opt-in
	Successor *Ref   // State == superseded and the successor card is mirrored
	NoReopen  bool   // mirror.close_on = "merge": an issue found closed stays closed
}

// Body is the issue body every adapter writes: the public summary, then the card id.
func (it Item) Body() string { return it.Summary + "\n\ncerne " + it.CardID }

// Remote is an open issue as its tracker lists it.
type Remote struct {
	Ref
	Title, Body string
}

// IssueTracker is the contract every adapter implements; trackertest.RunContract checks it.
type IssueTracker interface {
	Create(ctx context.Context, it Item) (Ref, error)             // publishes it, leaves the issue in it.State
	Update(ctx context.Context, r Ref, it Item) error             // idempotent; writes only what differs; one-shot effects only on the closing call
	ListOpen(ctx context.Context, label string) ([]Remote, error) // <=100 open issues, never PRs; label "" = no filter
	CommitRef(r Ref, commitURL string, closing bool) string       // whole trailer: "Refs: #12" | "Closes: o/r#12" | "Refs: <url>"; pure, no network
	Owns(r Ref) bool                                              // r is an issue of this tracker's own repository/project; pure, no network
}

// Kinds maps a tracker kind to its constructor. A new tracker = one entry + <kind>.go.
var Kinds = map[string]func(Config) (IssueTracker, error){"github": newGitHub, "gitlab": newGitLab}

var (
	ErrUnknownKind = errors.New("unknown tracker kind") // text: unknown tracker kind "x" (supported: <comma-separated KindNames>)
	ErrTrackerURL  = errors.New("bad tracker url")      // not https, or a URL the kind cannot read
)

// KindNames returns the sorted keys of Kinds.
func KindNames() []string { return slices.Sorted(maps.Keys(Kinds)) }

// New builds the adapter for c without touching the network: the URL must be
// https and carry no credential, then the kind's constructor reads it.
func New(c Config) (IssueTracker, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || nonASCII(u.Host) {
		return nil, fmt.Errorf("%w %q: want an https URL with an ASCII host (punycode for an international name)", ErrTrackerURL, redactURL(c.URL))
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w %q: remove the user and password; .cernerc is committed, and the tracker's own login holds the credential", ErrTrackerURL, redactURL(c.URL))
	}
	mk, ok := Kinds[c.Kind]
	if !ok {
		return nil, fmt.Errorf("%w %q (supported: %s)", ErrUnknownKind, c.Kind, strings.Join(KindNames(), ", "))
	}
	tr, err := mk(c)
	if err != nil {
		return nil, err
	}
	return checked{tr}, nil
}

// localHost reports a host name that names this machine or its network
// rather than a tracker, in any spelling: an IP literal (zoned IPv6 too),
// an empty name, localhost or *.localhost, a trailing dot, or a last label a resolver reads
// as a number (127.1, 2130706433, 0x7f000001). Real top-level domains are
// alphabetic. A kind whose CLI sends the user's token checks it at New.
func localHost(h string) bool {
	h = strings.ToLower(h)
	if h == "" {
		return true
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return true
	}
	last := h[strings.LastIndex(h, ".")+1:]
	return h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".") ||
		digits(last) || strings.HasPrefix(last, "0x")
}

// userinfoRe is the user[:password]@ of a URL's authority.
var userinfoRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*://)[^/?#]*@`)

// redactURL drops the user and password from raw, for an error message that
// repeats a tracker URL.
func redactURL(raw string) string { return userinfoRe.ReplaceAllString(raw, "${1}") }

// errBadRef is an issue ref with a control character in its id or URL, or a
// URL that is not https: it could forge a line in a commit message.
var errBadRef = errors.New("malformed issue ref")

// checked is what New hands out: the adapter, with every ref it answers
// (Create, ListOpen) and every ref it is given from the backend (Update,
// CommitRef, Owns) checked by validRef, so no adapter has to remember to
// (F-0449). A ref it answers must also be one its own Owns accepts. The core
// passes a successor only when Owns accepts it.
type checked struct{ IssueTracker }

func validRef(r Ref) bool {
	if r.ID == "" || strings.ContainsFunc(r.ID+r.URL, unicode.IsControl) {
		return false
	}
	u, err := url.Parse(r.URL)
	return err == nil && u.Scheme == "https" && u.Host != "" && !nonASCII(u.Host) && u.User == nil
}

// nonASCII reports a byte outside ASCII: EqualFold folds some of those onto
// ASCII letters (U+212A onto k), so a host that has one is never compared.
func nonASCII(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r > unicode.MaxASCII })
}

func (c checked) Create(ctx context.Context, it Item) (Ref, error) {
	r, err := c.IssueTracker.Create(ctx, it)
	if r != (Ref{}) && !validRef(r) {
		return Ref{}, fmt.Errorf("%w: the tracker answered id %q, url %q", errBadRef, r.ID, r.URL)
	}
	if r != (Ref{}) && !c.IssueTracker.Owns(r) {
		u, _ := url.Parse(r.URL) // validRef parsed it
		return Ref{}, fmt.Errorf("%w: the tracker answered an issue outside its repository, on host %q", ErrTrackerURL, u.Host)
	}
	return r, err
}

func (c checked) Update(ctx context.Context, r Ref, it Item) error {
	if !validRef(r) {
		return fmt.Errorf("%w: id %q, url %q", errBadRef, r.ID, r.URL)
	}
	return c.IssueTracker.Update(ctx, r, it)
}

func (c checked) ListOpen(ctx context.Context, label string) ([]Remote, error) {
	list, err := c.IssueTracker.ListOpen(ctx, label)
	var out []Remote
	for _, rm := range list {
		if validRef(rm.Ref) && c.IssueTracker.Owns(rm.Ref) {
			out = append(out, rm)
		}
	}
	return out, err
}

func (c checked) CommitRef(r Ref, commitURL string, closing bool) string {
	if !validRef(r) {
		return ""
	}
	if s := c.IssueTracker.CommitRef(r, commitURL, closing); !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	return ""
}

func (c checked) Owns(r Ref) bool { return validRef(r) && c.IssueTracker.Owns(r) }

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
	"net/url"
	"slices"
	"strings"

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
}

// Kinds maps a tracker kind to its constructor. A new tracker = one entry + <kind>.go.
var Kinds = map[string]func(Config) (IssueTracker, error){"github": newGitHub}

var (
	ErrUnknownKind = errors.New("unknown tracker kind") // text: unknown tracker kind "x" (supported: github)
	ErrTrackerURL  = errors.New("bad tracker url")      // not https, or a URL the kind cannot read
)

// KindNames returns the sorted keys of Kinds.
func KindNames() []string { return slices.Sorted(maps.Keys(Kinds)) }

// New builds the adapter for c without touching the network: the URL must be
// https, then the kind's constructor reads it.
func New(c Config) (IssueTracker, error) {
	if u, err := url.Parse(c.URL); err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("%w %q: want an https URL", ErrTrackerURL, c.URL)
	}
	mk, ok := Kinds[c.Kind]
	if !ok {
		return nil, fmt.Errorf("%w %q (supported: %s)", ErrUnknownKind, c.Kind, strings.Join(KindNames(), ", "))
	}
	return mk(c)
}

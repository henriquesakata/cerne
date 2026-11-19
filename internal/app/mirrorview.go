package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// The read side of the mirror (F-0465): what the Mirror tab shows. Nothing here
// writes to a tracker or to the backend.

// MirrorTrackerRun is what one sync did on one tracker: the cards it brought
// to their state, the ideas it imported, and the lines of what failed.
type MirrorTrackerRun struct {
	Name     string   `json:"name"`
	Synced   int      `json:"synced"`
	Imported int      `json:"imported"`
	Errors   []string `json:"errors"`
}

// MirrorLastSync is the record of the last sync on this computer.
type MirrorLastSync struct {
	At       time.Time          `json:"at"`
	Trackers []MirrorTrackerRun `json:"trackers"`
}

// lastSyncPath is the record's file, next to the project's index.db: cache, not
// backend, so a sync in a loop commits nothing.
func lastSyncPath(repoDir string) string {
	return filepath.Join(filepath.Dir(repoDir), "mirror-sync.json")
}

// recordLastSync writes the record of rep.
func recordLastSync(repoDir string, rep MirrorSyncReport) error {
	data, err := json.Marshal(MirrorLastSync{At: time.Now().UTC(), Trackers: rep.Trackers})
	if err != nil {
		return err
	}
	p := lastSyncPath(repoDir)
	return project.WriteFileAtomic(filepath.Dir(p), filepath.Base(p), data)
}

// LastMirrorSync reads the record; nil when there is none or it is unreadable.
func LastMirrorSync(repoDir string) *MirrorLastSync {
	b, err := os.ReadFile(lastSyncPath(repoDir))
	if err != nil {
		return nil
	}
	var l MirrorLastSync
	if json.Unmarshal(b, &l) != nil {
		return nil
	}
	return &l
}

// MirrorTracker is a configured tracker as the tab shows it; the URL carries no
// credential.
type MirrorTracker struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// MirrorIssue is the issue a card or an idea is linked to.
type MirrorIssue struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// MirrorCard is a card with a mirror link.
type MirrorCard struct {
	ID      string              `json:"id"`
	Title   string              `json:"title"`
	State   domain.FeatureState `json:"state"`
	Tracker string              `json:"tracker"`
	Ref     MirrorIssue         `json:"ref"`
}

// MirrorIdea is an idea imported from an outside issue.
type MirrorIdea struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	State   domain.IdeaState `json:"state"`
	Tracker string           `json:"tracker"`
	URL     string           `json:"url"`
}

// MirrorView is everything the Mirror tab shows without touching a tracker.
type MirrorView struct {
	Enabled  bool            `json:"enabled"`
	Trackers []MirrorTracker `json:"trackers"`
	Cards    []MirrorCard    `json:"cards"`
	Ideas    []MirrorIdea    `json:"ideas"`
	LastSync *MirrorLastSync `json:"lastSync"`
}

// Status and reason codes of a check; the UI translates the reasons.
const (
	MirrorOK        = "ok"
	MirrorDrift     = "drift"
	MirrorUnchecked = "unchecked"

	ReasonTrackerFailed  = "tracker-failed"
	ReasonTrackerUnknown = "tracker-unknown"
	ReasonListCapped     = "list-capped"
	ReasonOpenCardClosed = "open-but-card-closed"
	ReasonClosedCardOpen = "closed-but-card-open"
)

// MirrorCheck is the verdict on one mirrored card: Reason is a code, Detail the
// tracker's own (secret-free) error text.
type MirrorCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// MirrorOverview reads the store, the settings and the cache; it never calls a
// tracker. With the mirror off the lists are empty.
func MirrorOverview(repoDir, projDir string) (MirrorView, error) {
	cfg := project.MirrorSettings(projDir)
	v := MirrorView{Enabled: cfg.Enabled, Trackers: []MirrorTracker{}, Cards: []MirrorCard{}, Ideas: []MirrorIdea{}}
	if !cfg.Enabled {
		return v, nil
	}
	for _, t := range cfg.Trackers {
		v.Trackers = append(v.Trackers, MirrorTracker{Name: t.Name, Kind: t.Kind, URL: stripUserinfo(t.URL)})
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return v, err
	}
	for _, f := range features {
		if f.Mirror != nil {
			v.Cards = append(v.Cards, MirrorCard{ID: f.ID, Title: f.Title, State: f.State, Tracker: f.Mirror.Tracker, Ref: MirrorIssue{ID: f.Mirror.ID, URL: f.Mirror.URL}})
		}
	}
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return v, err
	}
	for _, i := range ideas {
		if i.Mirror != nil {
			v.Ideas = append(v.Ideas, MirrorIdea{ID: i.ID, Title: i.Title, State: i.State, Tracker: i.Mirror.Tracker, URL: i.Mirror.URL})
		}
	}
	v.LastSync = LastMirrorSync(repoDir)
	return v, nil
}

// stripUserinfo drops the user and password of a hand-edited .cernerc URL; ""
// when it does not parse.
func stripUserinfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	return u.String()
}

// CheckMirror asks each tracker, read-only, which issues are open and judges
// each mirrored card against that. It writes nothing. ponytail: sequential, one
// tracker at a time; parallelize if the number of trackers grows.
func CheckMirror(repoDir, projDir string) ([]MirrorCheck, error) {
	cfg := project.MirrorSettings(projDir)
	if !cfg.Enabled {
		return nil, fmt.Errorf("%w: a human turns on mirror.enabled in the Settings screen of cerne ui", ErrMirrorOff)
	}
	type listing struct {
		open map[string]bool
		n    int
		err  error
	}
	lists := map[string]listing{}
	for _, tc := range cfg.Trackers {
		// No label filter: the issues cerne publishes carry only the state label,
		// not the one the sync uses to pick outside issues.
		remotes, err := listOpenLabeled(tc, "")
		l := listing{open: map[string]bool{}, n: len(remotes), err: err}
		for _, r := range remotes {
			l.open[r.URL] = true
		}
		lists[tc.Name] = l
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	out := []MirrorCheck{}
	for _, f := range features {
		if f.Mirror == nil {
			continue
		}
		c := MirrorCheck{ID: f.ID, Status: MirrorOK}
		l, known := lists[f.Mirror.Tracker]
		isOpen := l.open[f.Mirror.URL]
		closedCard := f.State == domain.StateDone || f.State == domain.StateRejected || f.State == domain.StateSuperseded
		// With close_on = "merge" only a merge closes an issue, and only from
		// in_development on.
		mergeCanClose := f.State == domain.StateInDevelopment || f.State == domain.StateImplemented
		switch {
		case !known:
			c.Status, c.Reason = MirrorUnchecked, ReasonTrackerUnknown
		case l.err != nil:
			c.Status, c.Reason, c.Detail = MirrorUnchecked, ReasonTrackerFailed, l.err.Error()
		case !isOpen && l.n >= mirrorListCap:
			c.Status, c.Reason = MirrorUnchecked, ReasonListCapped
		case closedCard && isOpen:
			c.Status, c.Reason = MirrorDrift, ReasonOpenCardClosed
		case !closedCard && !isOpen && !(cfg.CloseOn == "merge" && mergeCanClose):
			c.Status, c.Reason = MirrorDrift, ReasonClosedCardOpen
		}
		out = append(out, c)
	}
	return out, nil
}

// mirrorListCap is the most open issues a tracker's ListOpen returns (the
// contract's ceiling): a list this long may have cut the card's issue off.
const mirrorListCap = 100

// Refusals of the commit list.
var (
	ErrBadCardID = errors.New("not a card id")
	ErrNoGitRepo = errors.New("the project is not a git repository")
)

// MirrorCommit is a commit whose Cerne trailer cites a card; URL is empty
// without a usable delivery.commit_url.
type MirrorCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	URL     string `json:"url,omitempty"`
}

// MirrorCommits lists, newest first, up to 20 commits of the project's git
// repository whose own "Cerne: <id>" line cites card id.
func MirrorCommits(projDir, id string) ([]MirrorCommit, error) {
	if !store.ValidEntityID(id) {
		return nil, fmt.Errorf("%w: %q", ErrBadCardID, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mirrorTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "git", "-C", projDir, "rev-parse", "--git-dir").Run(); err != nil {
		return nil, ErrNoGitRepo
	}
	// The id is validated: it holds no regexp metacharacter but the dash.
	out, err := exec.CommandContext(ctx, "git", "-C", projDir, "log", "-n", "20", "-E",
		"--grep=^Cerne: "+id+"$", "--format=%H%x00%h%x00%s%x1e").Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	commitURL := deliveryCommitURL(projDir)
	_, _, linkable := forge.ParseCommitURL(commitURL)
	commits := []MirrorCommit{}
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x00")
		if len(f) != 3 {
			continue
		}
		c := MirrorCommit{SHA: f[1], Subject: f[2]}
		if linkable { // the configured URL ends in a sample sha: swap in this commit's
			c.URL = commitURL[:strings.LastIndex(commitURL, "/")+1] + f[0]
		}
		commits = append(commits, c)
	}
	return commits, nil
}

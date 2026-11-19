package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker"
)

// The mirror core (F-0449, D-0009) knows only the IssueTracker contract: it
// hands each tracker cerne's own state in a tracker.Item and the adapter maps
// it. Nothing here names a tracker kind.

// Refusals of the mirror.
var (
	ErrMirrorOff       = errors.New("mirror is off")
	ErrMirrorText      = errors.New("a public title and summary are required")
	ErrUnknownTracker  = errors.New("unknown tracker")
	ErrAlreadyMirrored = errors.New("card is already mirrored")
)

// mirrorTimeout bounds the whole tracker round of one transition, and each
// card in a sync; a variable so a test can shrink it.
var mirrorTimeout = 5 * time.Second

// MirrorFeature publishes card id to the tracker named trackerName ("" = the
// first of mirror.trackers) with the public title and summary a human gave,
// then records the link on the card. An agent is refused before anything is
// read. The issue is created outside the backend lock; the link is written
// inside it, re-checked there.
func MirrorFeature(repoDir, projDir, id, trackerName, title, summary string) (store.Feature, error) {
	if err := checkHumanMirror(); err != nil {
		return store.Feature{}, err
	}
	title, summary = strings.TrimSpace(title), strings.TrimSpace(summary)
	if title == "" || summary == "" {
		return store.Feature{}, fmt.Errorf("%w: pass --title and --summary, the text the issue shows publicly", ErrMirrorText)
	}
	cfg := project.MirrorSettings(projDir)
	if !cfg.Enabled {
		return store.Feature{}, fmt.Errorf("%w: a human turns on mirror.enabled in the Settings screen of cerne ui", ErrMirrorOff)
	}
	tc, err := pickTracker(cfg, trackerName)
	if err != nil {
		return store.Feature{}, err
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return store.Feature{}, err
	}
	if f.Mirror != nil {
		return store.Feature{}, fmt.Errorf("%w: %s -> %s", ErrAlreadyMirrored, id, f.Mirror.URL)
	}
	tr, err := tracker.New(tc)
	if err != nil {
		return store.Feature{}, err
	}
	link := store.MirrorRef{Tracker: tc.Name, Kind: tc.Kind, Title: title, Summary: summary}
	ctx, cancel := context.WithTimeout(context.Background(), mirrorTimeout)
	defer cancel()
	r, createErr := tr.Create(ctx, mirrorItem(repoDir, f, link, cfg))
	if r.URL == "" {
		return store.Feature{}, fmt.Errorf("mirror %s -> %s: %w", id, tc.Name, createErr)
	}
	// The issue exists even when its state did not land: record the link, so
	// a retry cannot publish a twin and the sync reapplies the state.
	link.ID, link.URL = r.ID, r.URL
	var out store.Feature
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			return "", err
		}
		if f.Mirror != nil {
			return "", fmt.Errorf("%w: %s -> %s (an issue was also created at %s)", ErrAlreadyMirrored, id, f.Mirror.URL, r.URL)
		}
		l := link
		f.Mirror = &l
		f.UpdatedAt = time.Now().UTC()
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "feature.mirrored", map[string]string{"tracker": tc.Name, "url": r.URL}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		out = f
		return fmt.Sprintf("cerne: mirror %s to %s", id, tc.Name), nil
	})
	if err != nil {
		return store.Feature{}, err
	}
	if createErr != nil {
		return out, fmt.Errorf("mirror %s -> %s: the issue is at %s but its state did not land: %w; run 'cerne mirror sync'", id, tc.Name, r.URL, createErr)
	}
	return out, nil
}

// pickTracker finds the tracker named name in cfg; "" is the first one.
func pickTracker(cfg project.MirrorConfig, name string) (tracker.Config, error) {
	if len(cfg.Trackers) == 0 {
		return tracker.Config{}, fmt.Errorf("%w: mirror.trackers is empty; a human adds one in the Settings screen of cerne ui", ErrUnknownTracker)
	}
	if name == "" {
		return cfg.Trackers[0], nil
	}
	names := make([]string, len(cfg.Trackers))
	for i, t := range cfg.Trackers {
		if t.Name == name {
			return t, nil
		}
		names[i] = t.Name
	}
	return tracker.Config{}, fmt.Errorf("%w %q (configured: %s)", ErrUnknownTracker, name, strings.Join(names, ", "))
}

// mirrorItem is everything cerne publishes about f: its state, the public text
// the link holds, and the successor's issue when f is superseded by a card
// that is mirrored too.
func mirrorItem(repoDir string, f store.Feature, link store.MirrorRef, cfg project.MirrorConfig) tracker.Item {
	it := tracker.Item{CardID: f.ID, State: f.State, Title: link.Title, Summary: link.Summary, NoReopen: cfg.CloseOn == "merge"}
	if f.State == domain.StateSuperseded && f.SupersededBy != "" {
		if s, err := store.LoadFeature(repoDir, f.SupersededBy); err == nil && s.Mirror != nil {
			it.Successor = &tracker.Ref{Tracker: s.Mirror.Tracker, ID: s.Mirror.ID, URL: s.Mirror.URL}
		}
	}
	return it
}

// mirrorAfterTransition moves the issue of each mirrored card among ids to the
// card's stored state, under one mirrorTimeout for the whole round, and
// returns a warning line per card whose tracker failed or did not answer in
// time. The transition is already committed: a failure never undoes it, and
// 'cerne mirror sync' reapplies the state. With the mirror off it only reads
// the settings.
func mirrorAfterTransition(repoDir, projDir string, ids []string) []string {
	cfg := project.MirrorSettings(projDir)
	if !cfg.Enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), mirrorTimeout)
	defer cancel()
	var warnings []string
	for _, id := range ids {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil || f.Mirror == nil {
			continue
		}
		if err := applyMirror(ctx, repoDir, f, cfg); err != nil {
			warnings = append(warnings, fmt.Sprintf("cerne: mirror %s -> %s: %v; run 'cerne mirror sync'", id, f.Mirror.Tracker, err))
		}
	}
	return warnings
}

// applyMirror moves the issue f is linked to toward f's state, on the tracker
// the link names; a tracker renamed or removed since is no fallback to another.
func applyMirror(ctx context.Context, repoDir string, f store.Feature, cfg project.MirrorConfig) error {
	k := slices.IndexFunc(cfg.Trackers, func(t tracker.Config) bool { return t.Name == f.Mirror.Tracker })
	if k < 0 {
		return fmt.Errorf("tracker %s not configured in mirror.trackers: %w", f.Mirror.Tracker, ErrUnknownTracker)
	}
	tr, err := tracker.New(cfg.Trackers[k])
	if err != nil {
		return err
	}
	r := tracker.Ref{Tracker: f.Mirror.Tracker, ID: f.Mirror.ID, URL: f.Mirror.URL}
	return tr.Update(ctx, r, mirrorItem(repoDir, f, *f.Mirror, cfg))
}

// transitionIDs is every card a transition moved: the card itself, the epics
// its start propagated to, and the children its cascade carried.
func transitionIDs(id string, propagated []string, moved []store.Feature) []string {
	ids := append([]string{id}, propagated...)
	for _, c := range moved {
		ids = append(ids, c.ID)
	}
	return ids
}

// MirrorSyncReport is what one sync did: the ideas it imported (ids), the
// cards whose issue it brought to their state (ids), and one warning line per
// tracker or card that failed.
type MirrorSyncReport struct{ Ideas, Applied, Warnings []string }

// SyncMirror imports, as active ideas, the open issues of every tracker that
// no card or idea is linked to (filtered by the tracker's label), all in one
// commit, then reapplies the state of every mirrored card: the way back from
// a transition whose tracker round failed. The link's URL is the idempotency
// key, checked again inside the lock against the backend's tip, so a second
// sync imports nothing. A tracker or card that fails is a warning, not an
// error. ponytail: sequential, one card at a time; parallelize if a sync gets slow.
func SyncMirror(repoDir, projDir string) (MirrorSyncReport, error) {
	var rep MirrorSyncReport
	cfg := project.MirrorSettings(projDir)
	if !cfg.Enabled {
		return rep, fmt.Errorf("%w: a human turns on mirror.enabled in the Settings screen of cerne ui", ErrMirrorOff)
	}
	linked, err := linkedURLs(repoDir)
	if err != nil {
		return rep, err
	}
	var fresh []store.Idea
	for _, tc := range cfg.Trackers {
		remotes, err := listOpen(tc)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("cerne: mirror sync <- %s: %v", tc.Name, err))
			continue
		}
		for _, r := range remotes {
			if !linked[r.URL] {
				linked[r.URL] = true
				m := store.MirrorRef{Tracker: tc.Name, Kind: tc.Kind, ID: r.ID, URL: r.URL}
				fresh = append(fresh, store.Idea{Title: ideaTitle(r), Body: strings.TrimSpace(r.Body), Mirror: &m})
			}
		}
	}
	if len(fresh) > 0 {
		if rep.Ideas, err = saveImportedIdeas(repoDir, projDir, fresh); err != nil {
			return rep, err
		}
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return rep, err
	}
	for _, f := range features {
		if f.Mirror == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), mirrorTimeout)
		err := applyMirror(ctx, repoDir, f, cfg)
		cancel()
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("cerne: mirror %s -> %s: %v", f.ID, f.Mirror.Tracker, err))
			continue
		}
		rep.Applied = append(rep.Applied, f.ID)
	}
	return rep, nil
}

// linkedURLs is the issue URL of every card and idea linked to one.
func linkedURLs(repoDir string) (map[string]bool, error) {
	urls := map[string]bool{}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	for _, f := range features {
		if f.Mirror != nil {
			urls[f.Mirror.URL] = true
		}
	}
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	for _, i := range ideas {
		if i.Mirror != nil {
			urls[i.Mirror.URL] = true
		}
	}
	return urls, nil
}

// listOpen lists tc's open issues carrying its label, under mirrorTimeout.
func listOpen(tc tracker.Config) ([]tracker.Remote, error) {
	tr, err := tracker.New(tc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), mirrorTimeout)
	defer cancel()
	return tr.ListOpen(ctx, tc.Label)
}

// ideaTitle is an outside issue's title on one line; its URL when it has none.
func ideaTitle(r tracker.Remote) string {
	if t := strings.Join(strings.Fields(r.Title), " "); t != "" {
		return t
	}
	return r.URL
}

// MirrorTrailers is a placeholder until the prepare-commit-msg trailer lands
// (plan T9): no card is cited.
func MirrorTrailers(repoDir, projDir string, cardIDs []string) []string { return nil }

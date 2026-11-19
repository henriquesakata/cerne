package app

import (
	"context"
	"errors"
	"fmt"
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

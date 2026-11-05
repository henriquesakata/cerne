package models

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"time"
)

// MaxAge is the refresh window: a catalog refreshed less than this ago is fresh
// and the provider is not queried (F-0311 criteria 18, 19, 25).
const MaxAge = 24 * time.Hour

// Diff is what one refresh changed, by id.
type Diff struct {
	Added    []string `json:"added"`
	Delisted []string `json:"delisted"`
}

// Empty reports whether the refresh changed nothing.
func (d Diff) Empty() bool { return len(d.Added) == 0 && len(d.Delisted) == 0 }

// Fresh reports whether cat was refreshed within MaxAge of now. A catalog that
// was never refreshed is stale.
func Fresh(cat Catalog, now time.Time) bool {
	return !cat.UpdatedAt.IsZero() && now.Sub(cat.UpdatedAt) < MaxAge
}

// Apply folds one successful listing of provider into the global layer. The
// listing is the authority on availability: a listed id the catalog lacks
// enters as current; a catalog entry of provider absent from the listing is
// delisted — kept, so its label still resolves for history. Nothing is removed
// and current never becomes legacy (demotion is a hand edit). Only the layer
// is written: untouched entries keep following the baseline.
func Apply(layer Catalog, provider string, listed []Entry, now time.Time) (Catalog, Diff) {
	cur := merge(Baseline(), layer)
	known := make(map[string]bool, len(cur.Entries))
	for _, e := range cur.Entries {
		known[e.ID] = true
	}
	seen := make(map[string]bool, len(listed))
	for _, l := range listed {
		seen[l.ID] = true
	}

	out := layer
	out.Entries = append([]Entry(nil), layer.Entries...)
	var d Diff
	for _, e := range cur.Entries {
		if e.Provider != provider || seen[e.ID] || e.Visibility == Delisted {
			continue
		}
		out.Entries = setVisibility(out.Entries, e.ID, Delisted)
		d.Delisted = append(d.Delisted, e.ID)
	}
	for _, l := range listed {
		if known[l.ID] {
			continue
		}
		known[l.ID] = true
		label := l.Label
		if label == "" {
			label = l.ID
		}
		out.Entries = append(out.Entries, Entry{ID: l.ID, Label: label, Provider: provider, Visibility: Current})
		d.Added = append(d.Added, l.ID)
	}
	out.Listed = maps.Clone(layer.Listed)
	if out.Listed == nil {
		out.Listed = map[string]bool{}
	}
	out.Listed[provider] = true
	out.UpdatedAt = now
	return out, d
}

// setVisibility overrides id's visibility in the layer, adding a minimal
// override entry when the layer does not carry id yet.
func setVisibility(entries []Entry, id, visibility string) []Entry {
	for i := range entries {
		if entries[i].ID == id {
			entries[i].Visibility = visibility
			return entries
		}
	}
	return append(entries, Entry{ID: id, Visibility: visibility})
}

// Refresh lists provider through list and folds the result into the global
// layer at dir. Any failure — the listing, or a global file that is not a
// valid catalog (never overwritten) — leaves the file as it was.
func Refresh(ctx context.Context, dir, provider string, list func(context.Context) ([]Entry, error), now time.Time) (Diff, error) {
	layer, err := readLayer(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Diff{}, err
	}
	listed, err := list(ctx)
	if err != nil {
		return Diff{}, err
	}
	out, d := Apply(layer, provider, listed, now)
	return d, saveLayer(dir, out)
}

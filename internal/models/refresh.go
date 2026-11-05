package models

import (
	"context"
	"encoding/json"
	"time"
)

// MaxAge is the refresh window: a catalog refreshed less than this ago is fresh
// and the provider is not queried (F-0311 criteria 18, 19, 25).
const MaxAge = 24 * time.Hour

// Diff is what one refresh changed, by id. Relisted are models a listing
// brought back after an earlier one had delisted them.
type Diff struct {
	Added    []string `json:"added"`
	Delisted []string `json:"delisted"`
	Relisted []string `json:"relisted"`
}

// Empty reports whether the refresh changed nothing.
func (d Diff) Empty() bool { return len(d.Added)+len(d.Delisted)+len(d.Relisted) == 0 }

// Fresh reports whether cat was refreshed within MaxAge of now. A catalog that
// was never refreshed is stale.
func Fresh(cat Catalog, now time.Time) bool {
	return !cat.UpdatedAt.IsZero() && now.Sub(cat.UpdatedAt) < MaxAge
}

// apply folds one successful listing of provider into the global file. The
// listing is the authority on availability: a listed id the catalog lacks
// enters as current; a catalog entry of provider absent from the listing is
// delisted — kept, so its label still resolves for history — and one a later
// listing brings back is relisted. Nothing is removed and current never
// becomes legacy (demotion is a hand edit). The listing only owns the
// "unlisted" mark: visibility underneath (baseline or hand) is never touched.
func apply(f *layerFile, provider string, listed []Entry, now time.Time) Diff {
	cur := merge(Baseline(), f.typed)
	known := make(map[string]bool, len(cur.Entries))
	for _, e := range cur.Entries {
		known[e.ID] = true
	}
	seen := make(map[string]bool, len(listed))
	for _, l := range listed {
		seen[l.ID] = true
	}

	unlisted := make(map[string]bool)
	for _, e := range f.typed.Entries {
		unlisted[e.ID] = e.Unlisted
	}

	var d Diff
	for _, e := range cur.Entries {
		switch {
		case e.Provider != provider:
		case seen[e.ID] && unlisted[e.ID]:
			f.patch(e.ID, func(m map[string]json.RawMessage) { delete(m, "unlisted") })
			d.Relisted = append(d.Relisted, e.ID)
		case !seen[e.ID] && e.Visibility != Delisted:
			f.patch(e.ID, func(m map[string]json.RawMessage) { m["unlisted"], _ = json.Marshal(true) })
			d.Delisted = append(d.Delisted, e.ID)
		}
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
		f.patch(l.ID, func(m map[string]json.RawMessage) {
			m["label"], _ = json.Marshal(label)
			m["provider"], _ = json.Marshal(provider)
			m["visibility"], _ = json.Marshal(Current)
		})
		d.Added = append(d.Added, l.ID)
	}
	f.set("updated_at", now)
	return d
}

// Refresh lists provider through list and folds the result into the global
// file at dir. Any failure — the listing, or a global file that is not a
// valid catalog (never overwritten) — leaves the file as it was.
func Refresh(ctx context.Context, dir, provider string, list func(context.Context) ([]Entry, error), now time.Time) (Diff, error) {
	f, err := readLayerFile(dir)
	if err != nil {
		return Diff{}, err
	}
	listed, err := list(ctx)
	if err != nil {
		return Diff{}, err
	}
	d := apply(&f, provider, listed, now)
	return d, f.save(dir)
}

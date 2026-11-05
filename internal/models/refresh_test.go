package models

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// listedAll returns every baseline entry of provider as a listing would.
func listedAll(provider string) []Entry {
	var out []Entry
	for _, e := range Baseline().Entries {
		if e.Provider == provider {
			out = append(out, Entry{ID: e.ID, Label: e.Label})
		}
	}
	return out
}

// refreshWith runs one successful refresh of anthropic over the global file
// in dir and returns the resolved catalog and the diff.
func refreshWith(t *testing.T, dir string, listed []Entry) (Catalog, Diff) {
	t.Helper()
	d, err := Refresh(context.Background(), dir, "anthropic", func(context.Context) ([]Entry, error) { return listed, nil }, now)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	cat, warn := Load(dir)
	if warn != nil {
		t.Fatalf("Load after refresh: %v", warn)
	}
	return cat, d
}

// TestRefreshAddsAsCurrent: criterion 7 — a listed model the catalog lacks
// enters as current, labelled by the provider's display name.
func TestRefreshAddsAsCurrent(t *testing.T) {
	listed := append(listedAll("anthropic"), Entry{ID: "claude-new-9", Label: "Claude New 9"})
	cat, diff := refreshWith(t, t.TempDir(), listed)

	if !slices.Equal(diff.Added, []string{"claude-new-9"}) || len(diff.Delisted) != 0 {
		t.Fatalf("diff = %+v, want only claude-new-9 added", diff)
	}
	got, ok := cat.Find("claude-new-9")
	if !ok || got.Visibility != Current || got.Label != "Claude New 9" || got.Provider != "anthropic" {
		t.Fatalf("added entry = %+v", got)
	}
	if !cat.UpdatedAt.Equal(now) {
		t.Fatalf("listing not stamped: updated=%v", cat.UpdatedAt)
	}
}

// TestRefreshDelistsMissing: criterion 5 — a catalog entry absent from a
// successful listing becomes delisted and is kept (label still resolves);
// nothing is removed and a repeat listing changes nothing.
func TestRefreshDelistsMissing(t *testing.T) {
	all := listedAll("anthropic")
	gone := all[len(all)-1]
	dir := t.TempDir()
	cat, diff := refreshWith(t, dir, all[:len(all)-1])

	if !slices.Equal(diff.Delisted, []string{gone.ID}) || len(diff.Added) != 0 {
		t.Fatalf("diff = %+v, want only %s delisted", diff, gone.ID)
	}
	got, ok := cat.Find(gone.ID)
	if !ok || got.Visibility != Delisted || got.Label != gone.Label {
		t.Fatalf("delisted entry = %+v (found %v), want kept with its label", got, ok)
	}
	if len(cat.Entries) != len(Baseline().Entries) {
		t.Fatalf("entries = %d, want %d (nothing removed)", len(cat.Entries), len(Baseline().Entries))
	}
	if _, again := refreshWith(t, dir, all[:len(all)-1]); !again.Empty() {
		t.Fatalf("repeat listing changed the catalog: %+v", again)
	}
}

// TestRefreshNeverDemotes: criterion 7 — the refresh never moves an entry
// from current to legacy (nor touches legacy ones it still sees): demotion is
// a hand edit of the global file.
func TestRefreshNeverDemotes(t *testing.T) {
	listed := append(listedAll("anthropic"), Entry{ID: "claude-new-9", Label: "Claude New 9"})
	cat, _ := refreshWith(t, t.TempDir(), listed)
	for _, b := range Baseline().Entries {
		got, _ := cat.Find(b.ID)
		if got.Visibility != b.Visibility {
			t.Fatalf("%s visibility %q → %q after a listing that still has it", b.ID, b.Visibility, got.Visibility)
		}
	}
}

// TestNoListingKeepsBaselineAuthority: criterion 6 — with no successful
// listing ever (first boot, offline, provider without a listing endpoint)
// nothing is delisted; and a listing of one provider never delists another's.
func TestNoListingKeepsBaselineAuthority(t *testing.T) {
	cat, _ := Load(t.TempDir())
	var current, legacy int
	for _, e := range cat.Entries {
		switch e.Visibility {
		case Delisted:
			t.Fatalf("%s delisted without any listing", e.ID)
		case Current:
			current++
		case Legacy:
			legacy++
		}
	}
	if current == 0 || legacy == 0 {
		t.Fatalf("baseline offers %d current / %d legacy, want both", current, legacy)
	}

	dir := t.TempDir()
	other := `{"entries":[{"id":"vendor-x-1","label":"Vendor X 1","provider":"vendor"}]}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, diff := refreshWith(t, dir, listedAll("anthropic"))
	if !diff.Empty() {
		t.Fatalf("diff = %+v, want none", diff)
	}
	if got, _ := cat.Find("vendor-x-1"); got.Visibility != Current {
		t.Fatalf("another provider's entry became %q", got.Visibility)
	}
}

// TestRefreshFailureKeepsFile: criterion 23 — a failed listing leaves the
// global file byte for byte; so does a malformed file (never overwritten).
func TestRefreshFailureKeepsFile(t *testing.T) {
	cases := map[string]string{
		"listing fails": `{"preferred_provider":"anthropic","entries":[]}`,
		"malformed":     `{broken`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, FileName)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			fail := errors.New("offline")
			list := func(context.Context) ([]Entry, error) {
				if name == "malformed" {
					return listedAll("anthropic"), nil
				}
				return nil, fail
			}
			if _, err := Refresh(context.Background(), dir, "anthropic", list, now); err == nil {
				t.Fatal("Refresh succeeded, want the failure reported")
			}
			if got, _ := os.ReadFile(path); string(got) != content {
				t.Fatalf("file changed to %q", got)
			}
		})
	}
}

// TestRefreshWritesTheLayer: a successful refresh persists the diff, so the
// next Load (any project, any process) sees it.
func TestRefreshWritesTheLayer(t *testing.T) {
	dir := t.TempDir()
	list := func(context.Context) ([]Entry, error) {
		return append(listedAll("anthropic"), Entry{ID: "claude-new-9", Label: "Claude New 9"}), nil
	}
	diff, err := Refresh(context.Background(), dir, "anthropic", list, now)
	if err != nil || !slices.Equal(diff.Added, []string{"claude-new-9"}) {
		t.Fatalf("Refresh = %+v, %v", diff, err)
	}
	cat, warn := Load(dir)
	if _, ok := cat.Find("claude-new-9"); !ok || warn != nil || !Fresh(cat, now.Add(time.Hour)) {
		t.Fatalf("after Refresh: warn %v, fresh %v", warn, Fresh(cat, now.Add(time.Hour)))
	}
	if Fresh(cat, now.Add(MaxAge+time.Minute)) {
		t.Fatal("still fresh past the window")
	}
}

// TestRefreshRelistsWhatReappears: C1 — visibility follows the listing both
// ways: a model a later listing brings back leaves delisted and resolves to
// its baseline/hand visibility again; the diff names it. A transient partial
// listing never hides a model for good.
func TestRefreshRelistsWhatReappears(t *testing.T) {
	dir := t.TempDir()
	all := listedAll("anthropic")
	gone := all[0] // a baseline current entry
	hand := all[1] // demoted to legacy by hand
	body := `{"entries":[{"id":"` + hand.ID + `","visibility":"legacy"}]}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	withNew := append(append([]Entry(nil), all...), Entry{ID: "claude-new-9", Label: "Claude New 9"})
	refreshWith(t, dir, withNew)

	// A partial listing drops three: a baseline entry, a hand-demoted one and
	// one the refresh itself added.
	partial := []Entry{}
	for _, e := range all {
		if e.ID != gone.ID && e.ID != hand.ID {
			partial = append(partial, e)
		}
	}
	cat, diff := refreshWith(t, dir, partial)
	if len(diff.Delisted) != 3 {
		t.Fatalf("partial listing diff = %+v, want 3 delisted", diff)
	}
	for _, id := range []string{gone.ID, hand.ID, "claude-new-9"} {
		if e, _ := cat.Find(id); e.Visibility != Delisted {
			t.Fatalf("%s = %q after the partial listing, want delisted", id, e.Visibility)
		}
	}

	cat, diff = refreshWith(t, dir, withNew)
	if !slices.Equal(diff.Relisted, []string{gone.ID, hand.ID, "claude-new-9"}) || len(diff.Added)+len(diff.Delisted) != 0 {
		t.Fatalf("relisting diff = %+v, want the three relisted and nothing else", diff)
	}
	want := map[string]string{gone.ID: Current, hand.ID: Legacy, "claude-new-9": Current}
	for id, vis := range want {
		if e, _ := cat.Find(id); e.Visibility != vis {
			t.Fatalf("%s = %q after relisting, want %q", id, e.Visibility, vis)
		}
	}
	if _, again := refreshWith(t, dir, withNew); !again.Empty() {
		t.Fatalf("repeat listing changed the catalog: %+v", again)
	}
}

// TestBaselineClassifiesTheRealListing: C2 — the vendor's real listing
// (collected 2026-11-05) holds older ids the baseline must pre-classify as
// legacy, or the first refresh drops them on the top level as current. A
// listing equal to the real one adds nothing and the top level stays the
// four current entries.
func TestBaselineClassifiesTheRealListing(t *testing.T) {
	older := []string{"claude-opus-4-7", "claude-sonnet-4-6", "claude-opus-4-6", "claude-opus-4-5-20251101", "claude-sonnet-4-5-20250929"}
	base := Baseline()
	for _, id := range older {
		e, ok := base.Find(id)
		if !ok || e.Visibility != Legacy || e.Label == "" || e.Label == id || len(e.Efforts) != 0 {
			t.Fatalf("baseline %s = %+v (found %v), want a labelled legacy entry without efforts", id, e, ok)
		}
	}
	real := listedAll("anthropic")
	cat, diff := refreshWith(t, t.TempDir(), real)
	if !diff.Empty() {
		t.Fatalf("a listing equal to the real one changed the catalog: %+v", diff)
	}
	var current []string
	for _, e := range cat.Entries {
		if e.Visibility == Current {
			current = append(current, e.ID)
		}
	}
	if want := []string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}; !slices.Equal(current, want) {
		t.Fatalf("top level = %v, want %v", current, want)
	}
}

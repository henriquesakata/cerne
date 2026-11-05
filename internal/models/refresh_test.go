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

// TestRefreshAddsAsCurrent: criterion 7 — a listed model the catalog lacks
// enters as current, labelled by the provider's display name.
func TestRefreshAddsAsCurrent(t *testing.T) {
	listed := append(listedAll("anthropic"), Entry{ID: "claude-new-9", Label: "Claude New 9"})
	layer, diff := Apply(Catalog{}, "anthropic", listed, now)

	if !slices.Equal(diff.Added, []string{"claude-new-9"}) || len(diff.Delisted) != 0 {
		t.Fatalf("diff = %+v, want only claude-new-9 added", diff)
	}
	cat := merge(Baseline(), layer)
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
	layer, diff := Apply(Catalog{}, "anthropic", all[:len(all)-1], now)

	if !slices.Equal(diff.Delisted, []string{gone.ID}) || len(diff.Added) != 0 {
		t.Fatalf("diff = %+v, want only %s delisted", diff, gone.ID)
	}
	cat := merge(Baseline(), layer)
	got, ok := cat.Find(gone.ID)
	if !ok || got.Visibility != Delisted || got.Label != gone.Label {
		t.Fatalf("delisted entry = %+v (found %v), want kept with its label", got, ok)
	}
	if len(cat.Entries) != len(Baseline().Entries) {
		t.Fatalf("entries = %d, want %d (nothing removed)", len(cat.Entries), len(Baseline().Entries))
	}
	if _, again := Apply(layer, "anthropic", all[:len(all)-1], now); !again.Empty() {
		t.Fatalf("repeat listing changed the catalog: %+v", again)
	}
}

// TestRefreshNeverDemotes: criterion 7 — the refresh never moves an entry
// from current to legacy (nor touches legacy ones it still sees): demotion is
// a hand edit of the global file.
func TestRefreshNeverDemotes(t *testing.T) {
	listed := append(listedAll("anthropic"), Entry{ID: "claude-new-9", Label: "Claude New 9"})
	layer, _ := Apply(Catalog{}, "anthropic", listed, now)
	cat := merge(Baseline(), layer)
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

	other := Catalog{Entries: []Entry{{ID: "vendor-x-1", Label: "Vendor X 1", Provider: "vendor"}}}
	layer, diff := Apply(other, "anthropic", listedAll("anthropic"), now)
	if !diff.Empty() {
		t.Fatalf("diff = %+v, want none", diff)
	}
	if got, _ := merge(Baseline(), layer).Find("vendor-x-1"); got.Visibility != Current {
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

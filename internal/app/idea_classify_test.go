package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// assertPromoteWroteNothing pins a refused promote: no feature, the idea still
// active, the backend HEAD where it was.
func assertPromoteWroteNothing(t *testing.T, repoDir, ideaID, head string) {
	t.Helper()
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("a refused promote created %d feature(s)", len(fs))
	}
	if i, _ := store.LoadIdea(repoDir, ideaID); i.State != domain.IdeaActive {
		t.Fatalf("a refused promote moved the idea to %s", i.State)
	}
	if got := backendHead(t, repoDir); got != head {
		t.Fatalf("a refused promote committed: HEAD %s -> %s", head, got)
	}
}

// F-0510 crit. 1: a passed axis replaces the idea's; an axis not passed is
// inherited.
func TestPromoteIdeaTagsPerAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a, _ := CreateIdea(repoDir, projDir, "Ideia A", "", []string{"process"}, []string{"idea"}, "", false)
	b, _ := CreateIdea(repoDir, projDir, "Ideia B", "", []string{"process"}, []string{"idea"}, "", false)

	_, f, err := PromoteIdea(repoDir, projDir, a.ID, PromoteOptions{
		Facets: []string{"code"}, SetFacets: true, Components: []string{"cli"}, SetComponents: true})
	if err != nil {
		t.Fatalf("PromoteIdea: %v", err)
	}
	if !slices.Equal(f.Facets, []string{"code"}) || !slices.Equal(f.Components, []string{"cli"}) {
		t.Fatalf("tags = %v / %v, want [code] / [cli]", f.Facets, f.Components)
	}

	_, f, err = PromoteIdea(repoDir, projDir, b.ID, PromoteOptions{Components: []string{"cli"}, SetComponents: true})
	if err != nil {
		t.Fatalf("PromoteIdea: %v", err)
	}
	if !slices.Equal(f.Facets, []string{"process"}) || !slices.Equal(f.Components, []string{"cli"}) {
		t.Fatalf("tags = %v / %v, want the idea's [process] / [cli]", f.Facets, f.Components)
	}
	if i, _ := store.LoadIdea(repoDir, b.ID); !slices.Equal(i.Components, []string{"idea"}) {
		t.Fatalf("the idea's own components changed to %v", i.Components)
	}
}

// F-0510 crit. 2: an unknown facet is refused with nothing written.
func TestPromoteIdeaUnknownFacetWritesNothing(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "Ideia", "", nil, nil, "", false)
	head := backendHead(t, repoDir)
	_, _, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{Facets: []string{"bogus"}, SetFacets: true})
	if !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want ErrUnknownFacet", err)
	}
	assertPromoteWroteNothing(t, repoDir, i.ID, head)
}

// F-0510 crit. 4: the capabilities land on the new card in the same backend
// commit that mints it and marks the idea; a lowercase id is canonicalized.
func TestPromoteIdeaLinksCapabilitiesInOneCommit(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	c, err := CreateCapability(repoDir, projDir, "Gestão de demandas", "")
	if err != nil {
		t.Fatal(err)
	}
	i, _ := CreateIdea(repoDir, projDir, "Ideia", "", nil, nil, "", false)
	before := commitCount(t, repoDir)
	got, f, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{Capabilities: []string{"c-0001"}})
	if err != nil {
		t.Fatalf("PromoteIdea: %v", err)
	}
	if !slices.Equal(f.Capabilities, []string{c.ID}) {
		t.Fatalf("capabilities = %v, want [%s]", f.Capabilities, c.ID)
	}
	if stored, _ := store.LoadFeature(repoDir, f.ID); !slices.Equal(stored.Capabilities, []string{c.ID}) {
		t.Fatalf("stored capabilities = %v, want [%s]", stored.Capabilities, c.ID)
	}
	if got.State != domain.IdeaPromoted {
		t.Fatalf("idea state = %s, want promoted", got.State)
	}
	if after := commitCount(t, repoDir); after != before+1 {
		t.Fatalf("promote made %d commits, want 1", after-before)
	}
}

// F-0510 crit. 5: an unknown capability id is refused with nothing written.
func TestPromoteIdeaUnknownCapabilityWritesNothing(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateCapability(repoDir, projDir, "Gestão de demandas", ""); err != nil {
		t.Fatal(err)
	}
	i, _ := CreateIdea(repoDir, projDir, "Ideia", "", nil, nil, "", false)
	head := backendHead(t, repoDir)
	_, _, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{Capabilities: []string{"C-9999"}})
	if !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("err = %v, want ErrUnknownCapability", err)
	}
	assertPromoteWroteNothing(t, repoDir, i.ID, head)
}

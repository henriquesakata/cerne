package app

import (
	"errors"
	"slices"
	"strings"
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
// commit that mints it and marks the idea; a lowercase id is canonicalized and
// a repeated one is stored once.
func TestPromoteIdeaLinksCapabilitiesInOneCommit(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	c, err := CreateCapability(repoDir, projDir, "Gestão de demandas", "")
	if err != nil {
		t.Fatal(err)
	}
	i, _ := CreateIdea(repoDir, projDir, "Ideia", "", nil, nil, "", false)
	before := commitCount(t, repoDir)
	got, f, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{Capabilities: []string{"c-0001", "C-0001"}})
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

// F-0514: a retired capability is refused at promote too (the check lives in
// mint), with nothing written.
func TestPromoteIdeaRetiredCapabilityWritesNothing(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	c, err := CreateCapability(repoDir, projDir, "Velha", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RetireCapability(repoDir, projDir, c.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	i, _ := CreateIdea(repoDir, projDir, "Ideia", "", nil, nil, "", false)
	head := backendHead(t, repoDir)
	_, _, err = PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{Capabilities: []string{c.ID}})
	if !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("err = %v, want ErrUnknownCapability", err)
	}
	assertPromoteWroteNothing(t, repoDir, i.ID, head)
}

// F-0510 crit. 6: with RequireComplete, a promote that would leave the card
// untagged, or without a capability while the project has a taxonomy, is
// refused with nothing written and a message naming the flags; without it the
// plain promote still creates.
func TestPromoteIdeaRequireComplete(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "Ideia", "", nil, nil, "", false)

	// No taxonomy yet: only the tag is required.
	head := backendHead(t, repoDir)
	_, _, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{RequireComplete: true})
	if !errors.Is(err, ErrIncompleteCard) {
		t.Fatalf("untagged err = %v, want ErrIncompleteCard", err)
	}
	for _, want := range []string{i.ID, "no tag", "--facet", "--component", "--allow-incomplete"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "--capability") {
		t.Fatalf("refusal %q asks for a capability the project does not curate", err)
	}
	assertPromoteWroteNothing(t, repoDir, i.ID, head)

	// With a taxonomy, the capability is required too.
	if _, err := CreateCapability(repoDir, projDir, "Gestão de demandas", ""); err != nil {
		t.Fatal(err)
	}
	head = backendHead(t, repoDir)
	_, _, err = PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{RequireComplete: true,
		Facets: []string{"code"}, SetFacets: true})
	if !errors.Is(err, ErrIncompleteCard) || !strings.Contains(err.Error(), "no capability") ||
		!strings.Contains(err.Error(), "--capability") || strings.Contains(err.Error(), "no tag") {
		t.Fatalf("capability-missing err = %v, want ErrIncompleteCard naming only the capability", err)
	}
	assertPromoteWroteNothing(t, repoDir, i.ID, head)

	if _, _, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{RequireComplete: true,
		Facets: []string{"code"}, SetFacets: true, Capabilities: []string{"C-0001"}}); err != nil {
		t.Fatalf("complete promote: %v", err)
	}
	j, _ := CreateIdea(repoDir, projDir, "Outra", "", nil, nil, "", false)
	if _, f, err := PromoteIdea(repoDir, projDir, j.ID, PromoteOptions{}); err != nil || len(f.Facets)+len(f.Components) != 0 {
		t.Fatalf("plain promote = %+v, %v; want an untagged card, as today", f, err)
	}
}

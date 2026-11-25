package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0510 crit. 1: --facet/--component replace the idea's axis; an axis not
// passed is inherited.
func TestIdeaPromoteTagFlags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "Ideia A", "--facet", "process", "--component", "idea")
	ideaCmd(t, projDir, "new", "Ideia B", "--facet", "process", "--component", "idea")
	ideaCmd(t, projDir, "promote", "I-0001", "--facet", "code", "--component", "cli")
	ideaCmd(t, projDir, "promote", "I-0002", "--component", "cli")
	for id, want := range map[string][2][]string{
		"F-0001": {{"code"}, {"cli"}},
		"F-0002": {{"process"}, {"cli"}},
	} {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil || !slices.Equal(f.Facets, want[0]) || !slices.Equal(f.Components, want[1]) {
			t.Fatalf("%s = %v / %v (%v), want %v / %v", id, f.Facets, f.Components, err, want[0], want[1])
		}
	}
}

// F-0510 crit. 3: a new component near an existing one is refused as in
// feature new, with nothing written; --force takes it and stderr announces it.
func TestIdeaPromoteNearComponent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "Vocabulario", "--component", "gitback")
	ideaCmd(t, projDir, "new", "Ideia")
	if out, err := execCerne(t, "idea", "promote", "I-0002", "--facet", "code", "--component", "gitbak", "--dir", projDir); !errors.Is(err, app.ErrNearComponent) {
		t.Fatalf("near component err = %v, want ErrNearComponent\n%s", err, out)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("a refused promote created %d feature(s)", len(fs))
	}
	if i, _ := store.LoadIdea(repoDir, "I-0002"); i.State != domain.IdeaActive {
		t.Fatalf("a refused promote moved the idea to %s", i.State)
	}
	_, stderr, err := execCerneSplit(t, "idea", "promote", "I-0002", "--facet", "code", "--component", "gitbak", "--force", "--dir", projDir)
	if err != nil || !strings.Contains(stderr, `new component "gitbak"`) {
		t.Fatalf("--force promote err = %v, stderr = %q; want the new component announced", err, stderr)
	}
}

// F-0510 crit. 6: the CLI refuses a promote that would leave the card
// incomplete, naming the flags; --allow-incomplete creates it as before.
func TestIdeaPromoteRefusesIncompleteCard(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "capability", "new", "Gestão de demandas", "--dir", projDir); err != nil {
		t.Fatalf("capability new: %v\n%s", err, out)
	}
	ideaCmd(t, projDir, "new", "Ideia espelhada")
	out, err := execCerne(t, "idea", "promote", "I-0001", "--dir", projDir)
	if !errors.Is(err, app.ErrIncompleteCard) {
		t.Fatalf("incomplete promote err = %v, want ErrIncompleteCard\n%s", err, out)
	}
	for _, want := range []string{"--facet", "--component", "--capability", "--allow-incomplete"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err, want)
		}
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("a refused promote created %d feature(s)", len(fs))
	}
	if i, _ := store.LoadIdea(repoDir, "I-0001"); i.State != domain.IdeaActive {
		t.Fatalf("a refused promote moved the idea to %s", i.State)
	}
	if out := ideaCmd(t, projDir, "promote", "I-0001", "--allow-incomplete"); !strings.Contains(out, "I-0001: active -> promoted (F-0001)") {
		t.Fatalf("--allow-incomplete promote = %q", out)
	}
}

// F-0510 crit. 7: a card born tagged and linked gets no tag or capability step,
// and the approve check lists neither impediment.
func TestIdeaPromoteCompleteCardHasNoCardSteps(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "capability", "new", "Gestão de demandas", "--dir", projDir); err != nil {
		t.Fatalf("capability new: %v\n%s", err, out)
	}
	ideaCmd(t, projDir, "new", "Ideia espelhada")
	out := ideaCmd(t, projDir, "promote", "I-0001", "--facet", "code", "--component", "cli", "--capability", "C-0001")
	block := stepBlock(out)
	if block == "" || strings.Contains(block, stepTags) || strings.Contains(block, stepCapability) {
		t.Fatalf("complete card still asks for tags or capability:\n%s", out)
	}
	check, _ := execCerne(t, "feature", "approve", "F-0001", "--check", "--dir", projDir)
	if strings.Contains(check, app.ImpedimentUntagged) || strings.Contains(check, app.ImpedimentCapabilityMissing) {
		t.Fatalf("approve --check lists a card impediment:\n%s", check)
	}
}

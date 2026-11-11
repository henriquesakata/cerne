package skills

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

// TestCerneAuthoringDocumentsTags pins the canonical authoring skill to the
// shipped tag surface (F-0081 notes, F-0084 references). The skill is the only
// authoring guide an agent reads; when note/reference tags shipped in the CLI
// but the skill never documented them, the guide drifted from reality. Each
// assertion maps to an F-0086 acceptance criterion.
func TestCerneAuthoringDocumentsTags(t *testing.T) {
	c := All()[0].Content

	contains := func(crit, sub string) {
		t.Helper()
		if !strings.Contains(c, sub) {
			t.Errorf("%s: skill missing %q", crit, sub)
		}
	}

	// C1 — note new carries both tag axes.
	contains("C1", "note new")
	contains("C1", "--facet")
	contains("C1", "--component")

	// C2 — the closed facet set in the prose is exactly domain.ValidFacets.
	// Adding a facet to the source without updating the skill fails here.
	for _, f := range domain.ValidFacets {
		contains("C2", f)
	}

	// C3 — component is the open, slug-normalized axis (Gitea -> gitea).
	contains("C3", "gitea")

	// C4 — note list filters by the axes.
	contains("C4", "note list --facet")

	// C5 — reference new carries the same axes.
	contains("C5", "reference new")
	contains("C5", "[--facet <f>] [--component <c>]")

	// C6 — the new reference list command and its flags.
	contains("C6", "reference list")
	contains("C6", "--all")
	contains("C6", "--json")

	// C7 — tags fold into the FTS body (search/ask find by tag).
	contains("C7", "FTS")

	// C8 (F-0087) — decisions carry the same tag surface: new, list, retag.
	contains("C8", "decision new")
	contains("C8", "decision list")
	contains("C8", "decision retag")

	// C9 (F-0093) — references close the parity with an in-place retag. Assert
	// the command form (not just the bare words, which the old "no reference
	// retag yet" disclaimer also matched) and that the disclaimer is gone.
	contains("C9", "cerne reference retag")
	if strings.Contains(c, "there is no `reference retag` yet") {
		t.Errorf("C9: skill still says reference retag does not exist")
	}

	// C10 (F-0097) — the skill warns that a live entity untagged on both axes
	// is an `untagged-live` lint debt, so agents tag proactively.
	contains("C10", "untagged-live")

	// C11 (F-0101) — features carry the same tag surface: an in-place retag and a
	// facet/component filter on the listing (F-0099 shipped the CLI; the kanban UI
	// F-0100 reads it). Pins the skill against dropping features from the axis.
	contains("C11", "feature retag")
	contains("C11", "feature list --facet")
}

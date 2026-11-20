package cli

import (
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestIdeaRetag (F-0482 criterion 3): idea retag works in any state, only the
// axis passed changes, --facet "" clears, and a bad facet is refused.
func TestIdeaRetag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ideaCmd(t, projDir, "new", "uma ideia", "--facet", "code")
	ideaCmd(t, projDir, "drop", "I-0001", "--reason", "nao")
	ideaCmd(t, projDir, "retag", "I-0001", "--component", "Lint")
	i, _ := store.LoadIdea(repoDir, "I-0001")
	if i.State != domain.IdeaDropped || !slices.Equal(i.Facets, []string{"code"}) || !slices.Equal(i.Components, []string{"lint"}) {
		t.Fatalf("after component retag of a dropped idea: %+v", i)
	}
	ideaCmd(t, projDir, "retag", "I-0001", "--facet", "")
	if i, _ = store.LoadIdea(repoDir, "I-0001"); len(i.Facets) != 0 || len(i.Components) != 1 {
		t.Fatalf("after --facet \"\": %+v", i)
	}
	if _, err := execCerne(t, "idea", "retag", "I-0001", "--facet", "prod", "--dir", projDir); err == nil {
		t.Fatalf("an unknown facet must be refused")
	}
	if _, err := execCerne(t, "idea", "retag", "I-0001", "--dir", projDir); err == nil {
		t.Fatalf("retag with no axis must be refused")
	}
}

// TestPendingRetag (F-0482 criterion 3): the same for pending, on a resolved one.
func TestPendingRetag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "uma divida", "--from", "F-0001")
	if out, err := execCerne(t, "pending", "resolve", "P-0001", "--dir", projDir); err != nil {
		t.Fatalf("pending resolve: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "pending", "retag", "P-0001", "--facet", "infra", "--component", "Gitea", "--dir", projDir); err != nil {
		t.Fatalf("pending retag: %v\n%s", err, out)
	}
	p, _ := store.LoadPending(repoDir, "P-0001")
	if p.State != domain.PendingResolved || !slices.Equal(p.Facets, []string{"infra"}) || !slices.Equal(p.Components, []string{"gitea"}) {
		t.Fatalf("after retag of a resolved pending: %+v", p)
	}
	if _, err := execCerne(t, "pending", "retag", "P-0001", "--facet", "prod", "--dir", projDir); err == nil {
		t.Fatalf("an unknown facet must be refused")
	}
}

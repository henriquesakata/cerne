package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// E-0005 criterion 22 (T-0018): decision new --informs links the decision at
// birth — validated like a note's, recorded on decision.created — and an
// unknown target mints nothing.
func TestCreateDecisionInforms(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, _ := CreateFeature(repoDir, projDir, "Uma feature", "", "", nil, nil)
	d, err := CreateDecision(repoDir, projDir, "Usar Keycloak", "corpo", []string{f.ID, f.ID}, nil, nil, false)
	if err != nil || !slices.Equal(d.Informs, []string{f.ID}) {
		t.Fatalf("create: %v %v", d.Informs, err)
	}
	if got, _ := store.LoadDecision(repoDir, d.ID); !slices.Equal(got.Informs, []string{f.ID}) {
		t.Fatalf("persisted informs = %v", got.Informs)
	}
	if p := createdPayload(t, repoDir, d.ID, "decision.created"); p["informs"] != f.ID {
		t.Fatalf("decision.created payload = %v", p)
	}
	if _, err := CreateDecision(repoDir, projDir, "Outra decisão", "corpo", []string{"F-0999"}, nil, nil, false); !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "F-0999") {
		t.Fatalf("unknown target: %v", err)
	}
	if ds, _ := store.LoadAllDecisions(repoDir); len(ds) != 1 {
		t.Fatalf("an unknown target minted a decision: %d decisions", len(ds))
	}
}

// Criterion 22: decision link adds and removes what an active decision
// informs, the mirror of note link — no duplicate, the events logged, an
// unknown target or an id in both lists refused, a no-op committing nothing,
// the id in any case (N-0061), a superseded decision refused.
func TestLinkDecision(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, _ := CreateFeature(repoDir, projDir, "Uma feature", "", "", nil, nil)
	d, err := CreateDecision(repoDir, projDir, "Usar Keycloak", "corpo", nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := LinkDecision(repoDir, projDir, strings.ToLower(d.ID), []string{f.ID, f.ID, " "}, nil)
	if err != nil || got.ID != d.ID || !slices.Equal(got.Informs, []string{f.ID}) {
		t.Fatalf("link: %+v %v", got, err)
	}
	if p := createdPayload(t, repoDir, d.ID, "decision.linked"); p["informs"] != f.ID {
		t.Fatalf("decision.linked payload = %v", p)
	}
	before, _ := gitback.HeadSHA(repoDir)
	if _, absent, err := LinkDecision(repoDir, projDir, d.ID, []string{f.ID}, []string{"F-0042"}); err != nil || !slices.Equal(absent, []string{"F-0042"}) {
		t.Fatalf("no-op: absent %v, %v", absent, err)
	}
	if after, _ := gitback.HeadSHA(repoDir); after != before {
		t.Fatal("a no-op link committed")
	}
	if _, _, err := LinkDecision(repoDir, projDir, d.ID, []string{"F-0999"}, nil); !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "F-0999") {
		t.Fatalf("unknown target: %v", err)
	}
	if _, _, err := LinkDecision(repoDir, projDir, d.ID, []string{f.ID}, []string{f.ID}); err == nil || !strings.Contains(err.Error(), "both name") {
		t.Fatalf("both lists: %v", err)
	}
	if _, _, err := LinkDecision(repoDir, projDir, "D-0999", []string{f.ID}, nil); !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "D-0999") {
		t.Fatalf("unknown decision: %v", err)
	}
	got, _, err = LinkDecision(repoDir, projDir, d.ID, nil, []string{f.ID})
	if err != nil || len(got.Informs) != 0 {
		t.Fatalf("unlink: %v %v", got.Informs, err)
	}
	if p := createdPayload(t, repoDir, d.ID, "decision.unlinked"); p["informs"] != f.ID {
		t.Fatalf("decision.unlinked payload = %v", p)
	}
	other, err := CreateDecision(repoDir, projDir, "Usar Gitea", "corpo", nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionDecision(repoDir, projDir, d.ID, other.ID, "trocou"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LinkDecision(repoDir, projDir, d.ID, []string{f.ID}, nil); !errors.Is(err, ErrEntityNotLive) {
		t.Fatalf("superseded decision: %v", err)
	}
}

// Critic L3: promoting an idea carries the active decisions informing it to
// the new feature, as it does for references (F-0389); a superseded one
// stays as it was.
func TestPromoteIdeaCarriesDecisionLinks(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "Exportar CSV", "", nil, nil, "", false)
	live, err := CreateDecision(repoDir, projDir, "Usar CSV", "corpo", []string{i.ID}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	dead, _ := CreateDecision(repoDir, projDir, "Usar planilha", "corpo", []string{i.ID}, nil, nil, false)
	if _, err := TransitionDecision(repoDir, projDir, dead.ID, live.ID, "trocou"); err != nil {
		t.Fatal(err)
	}
	_, f, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.LoadDecision(repoDir, live.ID); !slices.Equal(got.Informs, []string{i.ID, f.ID}) {
		t.Fatalf("live informs = %v, want [%s %s]", got.Informs, i.ID, f.ID)
	}
	if got, _ := store.LoadDecision(repoDir, dead.ID); !slices.Equal(got.Informs, []string{i.ID}) {
		t.Fatalf("superseded informs = %v, want untouched", got.Informs)
	}
	if p := createdPayload(t, repoDir, live.ID, "decision.linked"); p["via"] != "promote" || p["informs"] != f.ID {
		t.Fatalf("payload = %v", p)
	}
}

// N-0061: the targets of --informs and --unlink are ids in any case, stored
// and removed by the canonical id.
func TestLinkDecisionTargetsInAnyCase(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, _ := CreateFeature(repoDir, projDir, "Uma feature", "", "", nil, nil)
	d, err := CreateDecision(repoDir, projDir, "Usar Keycloak", "corpo", []string{strings.ToLower(f.ID)}, nil, nil, false)
	if err != nil || !slices.Equal(d.Informs, []string{f.ID}) {
		t.Fatalf("create with a lowercase target: %v %v", d.Informs, err)
	}
	got, _, err := LinkDecision(repoDir, projDir, d.ID, nil, []string{strings.ToLower(f.ID)})
	if err != nil || len(got.Informs) != 0 {
		t.Fatalf("unlink a lowercase target: %v %v", got.Informs, err)
	}
}

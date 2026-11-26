package cli

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestFeatureSetAcceptMode: the owner marks a card not yet concluded as
// needing a person, as passing alone, or back to the gate's decision; an
// unknown value and a concluded card are refused, nothing written.
// F-0518 crit. 14
func TestFeatureSetAcceptMode(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for id, st := range map[string]domain.FeatureState{"F-0001": domain.StateInDevelopment, "F-0002": domain.StateImplemented} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Card " + id, State: st}); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	for _, c := range []struct {
		id, mode, stored string
		want             error
	}{
		{"F-0001", "human", "human", nil},
		{"F-0001", "auto", "auto", nil},
		{"F-0001", "gate", "", nil},
		{"F-0001", "always", "", app.ErrInvalidAcceptMode},
		{"F-0002", "human", "", app.ErrFeatureFrozen},
	} {
		out, err := execCerne(t, "feature", "set", c.id, "--accept-mode", c.mode, "--dir", projDir)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Fatalf("%s --accept-mode %s: err = %v, want %v\n%s", c.id, c.mode, err, c.want, out)
		}
		f, err := store.LoadFeature(repoDir, c.id)
		if err != nil {
			t.Fatal(err)
		}
		if f.AcceptMode != c.stored {
			t.Errorf("%s --accept-mode %s: stored %q, want %q", c.id, c.mode, f.AcceptMode, c.stored)
		}
	}
}

// TestFeatureSetAcceptModeRefusesAgent: the mark is the owner's; an agent's
// is ErrHumanGate, and nothing else the call carries is written either.
// F-0518 crit. 16
func TestFeatureSetAcceptModeRefusesAgent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Card", State: domain.StateInDevelopment}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent")
	for _, args := range [][]string{{"--accept-mode", "auto"}, {"--accept-mode", "human", "--owner", "ana"}} {
		out, err := execCerne(t, append([]string{"feature", "set", "F-0001", "--dir", projDir}, args...)...)
		if !errors.Is(err, app.ErrHumanGate) {
			t.Errorf("%v: err = %v (%s), want ErrHumanGate", args, err, out)
		}
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.AcceptMode != "" || f.Owner != "" {
		t.Fatalf("an agent's mark was written: %+v", f)
	}
	if out, err := execCerne(t, "feature", "set", "F-0001", "--owner", "ana", "--dir", projDir); err != nil {
		t.Fatalf("an agent's set without --accept-mode is still its own: %v\n%s", err, out)
	}
}

package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// cardInState creates a feature and forces it into state in the backend.
func cardInState(t *testing.T, projDir, repoDir string, state domain.FeatureState) string {
	t.Helper()
	id := newFeature(t, projDir, "Feature A", 1)
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	f.State = state
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	return id
}

// F-0506 crit. 1, 3: a repeated accept exits 0 and says nothing changed, and
// with --reason says the reason was not recorded.
func TestFeatureAcceptRepeatedIsNoOp(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := cardInState(t, projDir, repoDir, domain.StateDone)
	eventsBefore, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	stdout, _, err := execCerneSplit(t, "feature", "accept", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("repeated accept: %v", err)
	}
	if want := id + " was already done; nothing changed"; strings.TrimSpace(stdout) != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}

	stdout, _, err = execCerneSplit(t, "feature", "accept", id, "--reason", "again", "--dir", projDir)
	if err != nil {
		t.Fatalf("repeated accept with reason: %v", err)
	}
	if !strings.Contains(stdout, id+" was already done; nothing changed") || !strings.Contains(stdout, "reason was not recorded") {
		t.Fatalf("stdout = %q, want the line plus the reason note", stdout)
	}
	eventsAfter, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("events %d → %d: the repeated accept wrote", len(eventsBefore), len(eventsAfter))
	}
}

// F-0506 crit. 1: the repeated accept never asks the forge about the delivery
// commit and prints nothing on stderr.
func TestFeatureAcceptRepeatedSkipsForge(t *testing.T) {
	f := &cliFakeForge{on: false}
	projDir, repoDir := forgeProject(t, "github", f)
	id := cardInState(t, projDir, repoDir, domain.StateDone)
	card, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	card.DeliveryCommitURL = "https://github.com/o/r/commit/abc1234"
	if err := store.SaveFeature(repoDir, card); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	_, stderr, err := execCerneSplit(t, "feature", "accept", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("repeated accept: %v", err)
	}
	if f.calls != 0 || stderr != "" {
		t.Fatalf("forge calls = %d, stderr = %q; want none", f.calls, stderr)
	}
}

// F-0506 crit. 6: every other state keeps today's refusal.
func TestFeatureAcceptOtherStatesStillRefused(t *testing.T) {
	for _, st := range []domain.FeatureState{domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment, domain.StateRejected, domain.StateSuperseded} {
		t.Run(string(st), func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			id := cardInState(t, projDir, repoDir, st)
			_, err := execCerne(t, "feature", "accept", id, "--dir", projDir)
			want := "invalid transition: cannot accept from " + string(st)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

// F-0506 crit. 7: the human gate is read before the state.
func TestFeatureAcceptDoneStillGated(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := cardInState(t, projDir, repoDir, domain.StateDone)
	t.Setenv("CERNE_ACTOR", "agent")
	_, err := execCerne(t, "feature", "accept", id, "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "human") {
		t.Fatalf("err = %v, want the human gate refusal", err)
	}
}

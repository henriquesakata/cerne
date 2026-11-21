package app

import (
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestValidateTransitionFlagsRevert (F-0169 crit. 4): undoing an approval
// carries its why — --reason is required, --by makes no sense here.
func TestValidateTransitionFlagsRevert(t *testing.T) {
	cases := []struct {
		name    string
		by      string
		reason  string
		wantErr string // empty = valid
	}{
		{"reason present", "", "aprovada por engano", ""},
		{"missing reason", "", "", "--reason"},
		{"by not allowed", "F-0002", "aprovada por engano", "--by"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTransitionFlags(domain.ActionRevert, c.by, c.reason)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want error mentioning %q", err, c.wantErr)
			}
		})
	}
}

// seedRevertable seeds id in state with its lifecycle docs and the delivery
// links (and, for done, the acceptance on merge) a delivered card carries.
func seedRevertable(t *testing.T, repoDir, id string, state domain.FeatureState) {
	t.Helper()
	seedFeatureInState(t, repoDir, id, state, "spec.md", "plan.md", "result.md")
	f := reload(t, repoDir, id)
	f.DeliveryCommitURL = "https://github.com/o/r/commit/abc1234"
	f.DeliveryMRURL = prURL("7")
	if state == domain.StateDone {
		f.AcceptedVia = "merge"
	}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed delivery "+id); err != nil {
		t.Fatal(err)
	}
}

// F-0499 crit. 1: a Ready card the human rejects goes back to development
// with the why on the event; its documents and delivery links stay.
func TestRevertImplementedBackToDevelopment(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedRevertable(t, repoDir, "F-0001", domain.StateImplemented)
	result, err := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "result.md"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionRevert, "", "bug no login", nil)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if out.From != domain.StateImplemented || out.To != domain.StateInDevelopment {
		t.Fatalf("outcome = %s → %s, want implemented → in_development", out.From, out.To)
	}
	got := reload(t, repoDir, "F-0001")
	if got.State != domain.StateInDevelopment || got.DeliveryMRURL != prURL("7") || got.DeliveryCommitURL == "" {
		t.Fatalf("entity = %+v, want in_development with the delivery links kept", got)
	}
	if after, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "result.md")); string(after) != string(result) {
		t.Fatalf("result changed by the revert:\n%s", after)
	}
	if ev := lastEventOf(t, repoDir, "F-0001"); ev.Type != "feature.reverted" || ev.Payload["reason"] != "bug no login" {
		t.Fatalf("event = %+v, want feature.reverted with the reason", ev)
	}
}

// F-0499 crit. 2: undoing an acceptance takes the card back to Ready and
// drops the mark of the acceptance on merge.
func TestRevertDoneBackToReady(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedRevertable(t, repoDir, "F-0001", domain.StateDone)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionRevert, "", "aceito por engano", nil)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if out.To != domain.StateImplemented {
		t.Fatalf("to = %s, want implemented", out.To)
	}
	if got := reload(t, repoDir, "F-0001"); got.State != domain.StateImplemented || got.AcceptedVia != "" || got.DeliveryMRURL != prURL("7") {
		t.Fatalf("entity = %+v, want implemented, accepted_via cleared, delivery kept", got)
	}
	if ev := lastEventOf(t, repoDir, "F-0001"); ev.Type != "feature.reverted" || ev.Payload["reason"] != "aceito por engano" {
		t.Fatalf("event = %+v, want feature.reverted with the reason", ev)
	}
}

// F-0499 crit. 4: the card sent back to development completes again through
// the same complete and its gates.
func TestRevertedFeatureCompletesAgain(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedRevertable(t, repoDir, "F-0001", domain.StateImplemented)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionRevert, "", "bug no login", nil); err != nil {
		t.Fatalf("revert: %v", err)
	}
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete again: %v", err)
	}
	if out.From != domain.StateInDevelopment || out.To != domain.StateImplemented {
		t.Fatalf("outcome = %s → %s, want in_development → implemented", out.From, out.To)
	}
}

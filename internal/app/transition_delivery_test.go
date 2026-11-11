package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestCompleteWithMRPersistsDeliveryAndEvent (F-0179 crit. 1, 2): complete
// with a delivery persists the URLs on the entity and stamps the completion
// event payload, all in the same transition.
func TestCompleteWithMRPersistsDeliveryAndEvent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	d := &Delivery{
		CommitURL: "https://gitlab.example/repo/-/commit/abc1234",
		MRURL:     "https://gitlab.example/repo/-/merge_requests/7",
	}
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", d)
	if err != nil {
		t.Fatalf("TransitionFeature complete: %v", err)
	}
	if out.To != domain.StateImplemented {
		t.Fatalf("outcome To = %s, want implemented", out.To)
	}
	got, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if got.DeliveryCommitURL != d.CommitURL || got.DeliveryMRURL != d.MRURL {
		t.Fatalf("delivery on entity = %q / %q, want %q / %q",
			got.DeliveryCommitURL, got.DeliveryMRURL, d.CommitURL, d.MRURL)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var completed *domain.Event
	for i := range evs {
		if evs[i].EntityID == "F-0001" && evs[i].Type == "feature.completed" {
			completed = &evs[i]
		}
	}
	if completed == nil {
		t.Fatalf("no feature.completed event")
	}
	if completed.Payload["commit_url"] != d.CommitURL || completed.Payload["mr_url"] != d.MRURL {
		t.Fatalf("completed payload = %+v, want commit_url/mr_url", completed.Payload)
	}
}

// TestCompleteWithoutDeliveryUnchanged (crit. 6): a nil delivery leaves the
// entity fields empty and the event payload without delivery keys — today's
// behavior, byte for byte.
func TestCompleteWithoutDeliveryUnchanged(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("TransitionFeature complete: %v", err)
	}
	got, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if got.DeliveryCommitURL != "" || got.DeliveryMRURL != "" {
		t.Fatalf("delivery fields must stay empty, got %q / %q", got.DeliveryCommitURL, got.DeliveryMRURL)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, ev := range evs {
		if ev.Type == "feature.completed" {
			if _, ok := ev.Payload["commit_url"]; ok {
				t.Fatalf("completed payload must not carry commit_url: %+v", ev.Payload)
			}
			if _, ok := ev.Payload["mr_url"]; ok {
				t.Fatalf("completed payload must not carry mr_url: %+v", ev.Payload)
			}
		}
	}
}

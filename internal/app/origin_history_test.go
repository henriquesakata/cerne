package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// B-0085 (review): the correction must not move UpdatedAt (stale-ready and the
// accept queue read it as the implemented-at time), must leave the creation
// event byte-identical, and a human->agent correction keeps a given quote.
func TestSetFeatureOriginKeepsHistory(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "human")
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feat", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	f, _ := store.LoadFeature(repoDir, "F-0001")
	f.State = domain.StateImplemented
	f.UpdatedAt = time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatal(err)
	}
	before, _ := store.ReadEvents(repoDir)
	created, _ := json.Marshal(before[0])

	if _, err := SetFeatureOrigin(repoDir, projDir, "F-0001", "agent", "ele disse"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.LoadFeature(repoDir, "F-0001")
	if !got.UpdatedAt.Equal(f.UpdatedAt) {
		t.Fatalf("UpdatedAt moved: %v -> %v", f.UpdatedAt, got.UpdatedAt)
	}
	after, _ := store.ReadEvents(repoDir)
	if again, _ := json.Marshal(after[0]); string(again) != string(created) {
		t.Fatalf("creation event changed:\n%s\n%s", created, again)
	}
	last := after[len(after)-1]
	if last.Payload["on_behalf"] != "ele disse" || last.Payload["to"] != "agent" {
		t.Fatalf("payload = %v, want the quote kept on a human->agent correction", last.Payload)
	}
}

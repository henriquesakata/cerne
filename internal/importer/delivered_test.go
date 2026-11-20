package importer

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestResolveDeliveredState(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"", "implemented", nil},
		{"implemented", "implemented", nil},
		{"done", "done", nil},
		{"candidate", "", ErrInvalidDeliveredState},
		{"Done", "", ErrInvalidDeliveredState},
	}
	for _, tt := range tests {
		got, err := ResolveDeliveredState(tt.in)
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("ResolveDeliveredState(%q) err = %v, want %v", tt.in, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("ResolveDeliveredState(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSetDeliveredOnlyTouchesDeliveredFeatures(t *testing.T) {
	cands := []Candidate{
		{Kind: "feature", State: "implemented"},
		{Kind: "feature", State: "candidate"},
		{Kind: "feature", State: "superseded"},
		{Kind: "decision", State: "active"},
	}
	SetDelivered(cands, "done")
	want := []string{"done", "candidate", "superseded", "active"}
	for i, c := range cands {
		if c.State != want[i] {
			t.Errorf("cands[%d].State = %q, want %q", i, c.State, want[i])
		}
	}
	items := []PlanItem{{Kind: "feature", State: "implemented"}, {Kind: "feature", State: "candidate"}, {Kind: "reference", State: "active"}}
	SetPlanDelivered(items, "done")
	wantItems := []string{"done", "candidate", "active"}
	for i, it := range items {
		if it.State != wantItems[i] {
			t.Errorf("items[%d].State = %q, want %q", i, it.State, wantItems[i])
		}
	}
}

func TestApplyDeliveredDoneStillWritesOnlyImportedEvents(t *testing.T) {
	repo := t.TempDir()
	cands := []Candidate{{Kind: "feature", Title: "Login", State: "implemented", Source: "docs/0001_login/", Files: map[string]string{}}}
	SetDelivered(cands, "done")
	sum, err := Apply(repo, cands, "tester", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sum.ByState["done"] != 1 {
		t.Fatalf("ByState = %v, want done 1", sum.ByState)
	}
	f, err := store.LoadFeature(repo, "F-0001")
	if err != nil || f.State != domain.StateDone {
		t.Fatalf("feature = %+v err=%v, want state done", f, err)
	}
	evs, err := store.ReadAllEvents(repo)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no events written")
	}
	for _, e := range evs {
		if e.Type != "feature.imported" {
			t.Errorf("event %q written, want only feature.imported", e.Type)
		}
	}
}

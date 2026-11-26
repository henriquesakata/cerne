package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

func createWithOnBehalf(t *testing.T, actor, onBehalf string) (string, store.Feature, error) {
	t.Helper()
	t.Setenv("CERNE_ACTOR", actor)
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateLike(repoDir, projDir, "E", "Origem", "", "", "", nil, nil, nil, "", false, "", "", onBehalf)
	return repoDir, f, err
}

func TestCreateFeatureOriginHuman(t *testing.T) {
	repoDir, f, err := createWithOnBehalf(t, "", "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.LoadFeature(repoDir, f.ID)
	if got.Origin != "human" {
		t.Fatalf("origin = %q, want human", got.Origin)
	}
}

func TestCreateFeatureOriginAgent(t *testing.T) {
	repoDir, f, err := createWithOnBehalf(t, "agent", "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.LoadFeature(repoDir, f.ID)
	if got.Origin != "agent" {
		t.Fatalf("origin = %q, want agent", got.Origin)
	}
}

func TestCreateFeatureOnBehalf(t *testing.T) {
	repoDir, f, err := createWithOnBehalf(t, "agent", "  corrige o bug do login  ")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.LoadFeature(repoDir, f.ID)
	if got.Origin != "human" {
		t.Fatalf("origin = %q, want human", got.Origin)
	}
	evs, err := store.ReadAllEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.EntityID == f.ID && e.Type == "feature.created" {
			if q := e.Payload["on_behalf"]; q != "corrige o bug do login" {
				t.Fatalf("on_behalf = %q", q)
			}
			return
		}
	}
	t.Fatal("no feature.created event")
}

func TestCreateFeatureOnBehalfEmptyRefused(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	_, err := CreateLike(repoDir, projDir, "E", "Origem", "", "", "", nil, nil, nil, "", false, "", "", "   ")
	if !errors.Is(err, ErrOnBehalfEmpty) {
		t.Fatalf("err = %v, want ErrOnBehalfEmpty", err)
	}
	if _, err := store.LoadFeature(repoDir, "E-0001"); err == nil {
		t.Fatal("a card was written despite the refusal")
	}
}

func TestPromoteIdeaOrigin(t *testing.T) {
	for actor, want := range map[string]string{"": "human", "agent": "agent"} {
		t.Setenv("CERNE_ACTOR", actor)
		repoDir, projDir, _ := newFeatureBackend(t)
		i, err := CreateIdea(repoDir, projDir, "Uma ideia", "corpo", nil, nil, "", false)
		if err != nil {
			t.Fatal(err)
		}
		_, f, err := PromoteIdea(repoDir, projDir, i.ID, PromoteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if f.Origin != want {
			t.Fatalf("actor %q: origin = %q, want %q", actor, f.Origin, want)
		}
	}
}

func TestPromotePendingOrigin(t *testing.T) {
	for actor, want := range map[string]string{"": "human", "agent": "agent"} {
		t.Setenv("CERNE_ACTOR", actor)
		repoDir, projDir, _ := newFeatureBackend(t)
		o := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
		p, err := CreatePending(repoDir, projDir, "Um débito", "corpo", o.ID, nil, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		_, f, err := PromotePending(repoDir, projDir, p.ID, "feature", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if f.Origin != want {
			t.Fatalf("actor %q: origin = %q, want %q", actor, f.Origin, want)
		}
	}
}

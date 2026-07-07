package app

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestDupGuardSimilarity pins the deterministic matcher (F-0147 crit. 3):
// normalized (case/diacritics) token overlap against min(len) with exact
// titles always matching; single-token titles only match exactly.
func TestDupGuardSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "Corrigir bug do lock", "Corrigir bug do lock", true},
		{"case+diacritics", "CORREÇÃO do lock", "correcao do lock", true},
		{"shorter title contained", "Guarda de duplicidade", "Guarda de duplicidade no registro: feature new recusa", true},
		{"same-shape different bug", "Corrigir bug do lock", "Corrigir bug do push", false},
		{"unrelated", "Tema dark no grafo", "Endpoint de versão", false},
		{"single token needs exact", "lock", "lock do push", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := similarTitles(c.a, c.b); got != c.want {
				t.Errorf("similarTitles(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// TestDupGuardRefusesLiveFeature (crit. 1): a live feature with a similar
// (not identical — that is the F-0121 exact guard) title refuses the new
// registration, naming the match; --force overrides and the created event
// carries the guard marker (crit. 4).
func TestDupGuardRefusesLiveFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md")

	_, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001 v2", "", "", nil, nil, "", false)
	if !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("err = %v, want ErrSimilarExists", err)
	}

	f, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001 v2", "", "", nil, nil, "", true)
	if err != nil {
		t.Fatalf("forced create: %v", err)
	}
	evs, _ := store.ReadEvents(repoDir)
	var forced bool
	for _, ev := range evs {
		if ev.EntityID == f.ID && ev.Type == "feature.created" && ev.Payload["guard"] == "forced" {
			forced = true
		}
	}
	if !forced {
		t.Fatalf("feature.created missing guard=forced payload")
	}
}

// TestDupGuardRefusesActiveWork (crit. 2): an active work with a similar title
// refuses both work start and feature new.
func TestDupGuardRefusesActiveWork(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	seedActiveWork(t, repoDir, "W-0001", "a@b", "F-0001", false)
	// seedActiveWork titles it "Atividade W-0001".

	if _, err := StartWork(repoDir, projDir, "Atividade W-0001", nil, "", false); !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("work start err = %v, want ErrSimilarExists", err)
	}
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Atividade W-0001", "", "", nil, nil, "", false); !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("feature new err = %v, want ErrSimilarExists", err)
	}
	if w, err := StartWork(repoDir, projDir, "Atividade W-0001", nil, "", true); err != nil || w.ID == "" {
		t.Fatalf("forced work start: %v", err)
	}
}

// TestDupGuardReadyOnlyWarns (crit. 5): a Ready (implemented) match does not
// refuse — it comes back as a warning for the caller to print.
func TestDupGuardReadyOnlyWarns(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented, "spec.md")

	matches, err := FindSimilar(repoDir, "Feature F-0001")
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(matches.Live) != 0 || len(matches.Ready) != 1 || matches.Ready[0].ID != "F-0001" {
		t.Fatalf("matches = %+v, want ready-only F-0001", matches)
	}
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001", "", "", nil, nil, "", false); err != nil {
		t.Fatalf("ready match must not refuse: %v", err)
	}
}

// TestDupGuardIgnoresTerminal (crit. 1 scope): rejected/superseded/done never
// block a new registration.
func TestDupGuardIgnoresTerminal(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateRejected, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateDone, "spec.md")

	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001", "", "", nil, nil, "", false); err != nil {
		t.Fatalf("terminal must not refuse: %v", err)
	}
}

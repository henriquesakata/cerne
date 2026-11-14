package app

import (
	"errors"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestDupGuardRefusesLiveFeature (crit. 1): a live feature with a similar
// (not identical — that is the F-0121 exact guard) title refuses the new
// registration, naming the match; --force overrides and the created event
// carries the guard marker (crit. 4).
func TestDupGuardRefusesLiveFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md")

	_, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001 v2", "", "", nil, nil, "", false, "", "")
	if !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("err = %v, want ErrSimilarExists", err)
	}

	f, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001 v2", "", "", nil, nil, "", true, "", "")
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

	if _, err := StartWork(repoDir, projDir, "Atividade W-0001", nil, "", false, nil, ""); !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("work start err = %v, want ErrSimilarExists", err)
	}
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Atividade W-0001", "", "", nil, nil, "", false, "", ""); !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("feature new err = %v, want ErrSimilarExists", err)
	}
	if w, err := StartWork(repoDir, projDir, "Atividade W-0001", nil, "", true, nil, ""); err != nil || w.ID == "" {
		t.Fatalf("forced work start: %v", err)
	}
}

// planningTitle is the work title the F-0208 rule prescribes for picking up
// F-0001: it repeats the feature's own title.
const planningTitle = "F-0001: Painel de custo por feature — planejamento"

// seedRefFeatures seeds F-0001 (the feature the planning title names) and an
// unrelated F-0002, both approved, and pushes them.
func seedRefFeatures(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newFeatureBackend(t)
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Painel de custo por feature", State: domain.StateApproved},
		{ID: "F-0002", Title: "Exportar o kanban em CSV", State: domain.StateApproved},
	} {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed features"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	return repoDir, projDir
}

// workStartedForced reports whether id's work.started event carries the
// F-0147 override marker.
func workStartedForced(t *testing.T, repoDir, id string) bool {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == "work.started" && ev.Payload["guard"] == "forced" {
			return true
		}
	}
	return false
}

// TestDupGuardIgnoresReferencedFeature (F-0423): the planning work the F-0208
// rule prescribes repeats its feature's title; the feature it names in --ref
// is not a duplicate of it. A similar unreferenced feature, and a similar
// active work on the same feature, still refuse.
func TestDupGuardIgnoresReferencedFeature(t *testing.T) {
	t.Run("crit 1 and 4: the referenced feature is no collision", func(t *testing.T) {
		for _, force := range []bool{false, true} {
			repoDir, projDir := seedRefFeatures(t)
			w, err := StartWork(repoDir, projDir, planningTitle, []string{"F-0001"}, "", force, nil, "")
			if err != nil {
				t.Fatalf("force=%v: work start --ref F-0001: %v", force, err)
			}
			if workStartedForced(t, repoDir, w.ID) {
				t.Fatalf("force=%v: %s work.started carries guard=forced, want no override marker", force, w.ID)
			}
		}
	})

	t.Run("crit 2: an unreferenced similar feature still refuses", func(t *testing.T) {
		repoDir, projDir := seedRefFeatures(t)
		title := "F-0002: Exportar o kanban em CSV — planejamento"
		for _, refs := range [][]string{nil, {"F-0001"}} {
			if _, err := StartWork(repoDir, projDir, title, refs, "", false, nil, ""); !errors.Is(err, ErrSimilarExists) {
				t.Fatalf("refs=%v: err = %v, want ErrSimilarExists", refs, err)
			}
		}
	})

	t.Run("crit 3: a similar active work on the same feature still refuses", func(t *testing.T) {
		repoDir, projDir := seedRefFeatures(t)
		if _, err := StartWork(repoDir, projDir, planningTitle, []string{"F-0001"}, "other@host", false, nil, ""); err != nil {
			t.Fatalf("first planning work: %v", err)
		}
		_, err := StartWork(repoDir, projDir, planningTitle, []string{"F-0001"}, "", false, nil, "")
		var se *SimilarExistsError
		if !errors.As(err, &se) {
			t.Fatalf("second planning work err = %v, want ErrSimilarExists", err)
		}
		if len(se.Live) != 1 || se.Live[0].ID != "W-0001" {
			t.Fatalf("matches = %+v, want only W-0001 (F-0001 is referenced)", se.Live)
		}
	})

	t.Run("crit 5: the ignore list compares ids without case", func(t *testing.T) {
		repoDir, _ := seedRefFeatures(t)
		if _, err := guardRegistration(repoDir, planningTitle, false, "f-0001"); err != nil {
			t.Fatalf("ignore f-0001: %v, want F-0001 ignored", err)
		}
	})
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
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001", "", "", nil, nil, "", false, "", ""); err != nil {
		t.Fatalf("ready match must not refuse: %v", err)
	}
}

// TestDupGuardIgnoresTerminal (crit. 1 scope): rejected/superseded/done never
// block a new registration.
func TestDupGuardIgnoresTerminal(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateRejected, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateDone, "spec.md")

	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature F-0001", "", "", nil, nil, "", false, "", ""); err != nil {
		t.Fatalf("terminal must not refuse: %v", err)
	}
}

// TestFindPathOverlap (F-0151 crit. 6): a new work declaring paths that can
// touch the same files as an active work's paths is reported (warning-level;
// the caller never blocks on it). Terminal works and disjoint globs stay out.
func TestFindPathOverlap(t *testing.T) {
	repoDir := t.TempDir()
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	seed := func(id string, state domain.WorkState, paths ...string) {
		t.Helper()
		if err := store.SaveWork(repoDir, store.Work{
			ID: id, Title: "atividade " + id, State: state, Actor: "agent",
			Paths: paths, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("SaveWork(%s): %v", id, err)
		}
	}
	seed("W-0001", domain.WorkActive, "internal/server/**")
	seed("W-0002", domain.WorkActive, "ui/src/**")
	seed("W-0003", domain.WorkDone, "internal/server/**") // terminal: never matches
	seed("W-0004", domain.WorkActive)                     // no declared paths: never matches

	cases := []struct {
		name  string
		paths []string
		want  []string // overlapping work ids
	}{
		{"nested glob under active glob", []string{"internal/server/api/**"}, []string{"W-0001"}},
		{"exact file under active glob", []string{"internal/server/server.go"}, []string{"W-0001"}},
		{"broader glob covering active glob", []string{"internal/**"}, []string{"W-0001"}},
		{"disjoint", []string{"cmd/cerne/**"}, nil},
		{"prefix must respect segment boundary", []string{"internal/serverless/**"}, nil},
		{"two overlaps", []string{"internal/server/**", "ui/src/App.tsx"}, []string{"W-0001", "W-0002"}},
		{"empty declaration", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FindPathOverlap(repoDir, tc.paths)
			if err != nil {
				t.Fatalf("FindPathOverlap: %v", err)
			}
			var ids []string
			for _, m := range got {
				ids = append(ids, m.ID)
			}
			if len(ids) != len(tc.want) {
				t.Fatalf("got %v, want %v", ids, tc.want)
			}
			for i := range ids {
				if ids[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", ids, tc.want)
				}
			}
		})
	}
}

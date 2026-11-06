package app

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// ideaEvents returns the types of every event of id in the raw log (idea.*
// never reaches the funnel's ReadEvents).
func ideaEvents(t *testing.T, repoDir, id string) ([]string, []map[string]string) {
	t.Helper()
	evs, err := store.ReadAllEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	var types []string
	var payloads []map[string]string
	for _, ev := range evs {
		if ev.EntityID == id {
			types = append(types, ev.Type)
			payloads = append(payloads, ev.Payload)
		}
	}
	return types, payloads
}

// Crit. 1: text only — no work, no module, no tags — mints an active I-.
func TestCreateIdeaTextOnly(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "  talvez um dia X  ", nil, nil)
	if err != nil {
		t.Fatalf("CreateIdea: %v", err)
	}
	if i.ID != "I-0001" || i.Text != "talvez um dia X" || i.State != domain.IdeaActive ||
		len(i.Facets) != 0 || len(i.Components) != 0 || i.CreatedAt.IsZero() {
		t.Fatalf("idea = %+v, want I-0001 active, untagged", i)
	}
	if body, ok := originHas(t, origin, "entities/ideas/I-0001.json"); !ok || !strings.Contains(body, "talvez um dia X") {
		t.Fatalf("I-0001 on origin: ok=%v body=%q", ok, body)
	}
	if types, _ := ideaEvents(t, repoDir, "I-0001"); !slices.Equal(types, []string{"idea.created"}) {
		t.Fatalf("events = %v, want [idea.created]", types)
	}
	if _, err := CreateIdea(repoDir, projDir, " \n ", nil, nil); !errors.Is(err, ErrIdeaTextRequired) {
		t.Fatalf("blank text err = %v, want ErrIdeaTextRequired", err)
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 1 {
		t.Fatalf("a refused capture wrote: %d ideas", len(all))
	}
}

// Crit. 2: optional tags are normalized and persisted; an unknown facet is
// refused before anything is written.
func TestCreateIdeaTags(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "com tags", []string{"code"}, []string{"Lint"})
	if err != nil {
		t.Fatalf("CreateIdea: %v", err)
	}
	stored, _ := store.LoadIdea(repoDir, i.ID)
	if !slices.Equal(stored.Facets, []string{"code"}) || !slices.Equal(stored.Components, []string{"lint"}) {
		t.Fatalf("tags = %v / %v, want [code] / [lint]", stored.Facets, stored.Components)
	}
	if _, err := CreateIdea(repoDir, projDir, "faceta ruim", []string{"nope"}, nil); !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("unknown facet err = %v, want ErrUnknownFacet", err)
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 1 {
		t.Fatalf("a refused capture wrote: %d ideas", len(all))
	}
}

// Crit. 4: active only, newest first; all includes closed; search folds case
// and accents.
func TestListIdeasFilters(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	for _, text := range []string{"primeira", "conciliação do OFX", "terceira"} {
		if _, err := CreateIdea(repoDir, projDir, text, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := DropIdea(repoDir, projDir, "I-0002", ""); err != nil {
		t.Fatal(err)
	}
	ids := func(is []store.Idea) []string {
		var out []string
		for _, i := range is {
			out = append(out, i.ID)
		}
		return out
	}
	active, err := ListIdeas(repoDir, false, "")
	if err != nil || !slices.Equal(ids(active), []string{"I-0003", "I-0001"}) {
		t.Fatalf("active = %v, %v; want [I-0003 I-0001]", ids(active), err)
	}
	if all, _ := ListIdeas(repoDir, true, ""); !slices.Equal(ids(all), []string{"I-0003", "I-0002", "I-0001"}) {
		t.Fatalf("all = %v, want [I-0003 I-0002 I-0001]", ids(all))
	}
	if hit, _ := ListIdeas(repoDir, true, "CONCILIACAO"); !slices.Equal(ids(hit), []string{"I-0002"}) {
		t.Fatalf("search = %v, want [I-0002]", ids(hit))
	}
	if hit, _ := ListIdeas(repoDir, false, "CONCILIACAO"); len(hit) != 0 {
		t.Fatalf("search over active = %v, want none (I-0002 is dropped)", ids(hit))
	}
}

// Crit. 8: drop keeps the record, with the optional reason; a second drop is
// refused naming the state.
func TestDropIdeaKeepsRecordWithReason(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "talvez", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DropIdea(repoDir, projDir, i.ID, " duplicada ")
	if err != nil {
		t.Fatalf("DropIdea: %v", err)
	}
	stored, _ := store.LoadIdea(repoDir, i.ID)
	if got.State != domain.IdeaDropped || stored.State != domain.IdeaDropped || stored.Reason != "duplicada" || stored.ClosedAt == nil {
		t.Fatalf("dropped = %+v, want dropped with reason and closed_at", stored)
	}
	types, payloads := ideaEvents(t, repoDir, i.ID)
	if !slices.Equal(types, []string{"idea.created", "idea.dropped"}) || payloads[1]["reason"] != "duplicada" {
		t.Fatalf("events = %v %v, want idea.dropped with reason", types, payloads)
	}
	if body, ok := originHas(t, origin, "entities/ideas/"+i.ID+".json"); !ok || !strings.Contains(body, `"dropped"`) {
		t.Fatalf("dropped idea on origin: ok=%v body=%q", ok, body)
	}
	_, err = DropIdea(repoDir, projDir, i.ID, "")
	if !errors.Is(err, ErrIdeaNotActive) || !strings.Contains(err.Error(), "dropped") {
		t.Fatalf("second drop err = %v, want ErrIdeaNotActive naming dropped", err)
	}
	if _, err := DropIdea(repoDir, projDir, "I-0099", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

// Crit. 12: concurrent drops of one idea — the state is re-read inside the
// lock, so exactly one wins and the log has one idea.dropped.
func TestDropIdeaRaceOneWinner(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "disputada", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	const n = 3
	errs := make([]error, n)
	var wg sync.WaitGroup
	for k := 0; k < n; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			_, errs[k] = DropIdea(repoDir, projDir, i.ID, "")
		}(k)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrIdeaNotActive):
			t.Fatalf("loser err = %v, want ErrIdeaNotActive", err)
		}
	}
	types, _ := ideaEvents(t, repoDir, i.ID)
	if wins != 1 || !slices.Equal(types, []string{"idea.created", "idea.dropped"}) {
		t.Fatalf("wins = %d, events = %v; want 1 winner, one idea.dropped", wins, types)
	}
}

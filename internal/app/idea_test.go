package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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
	i, err := CreateIdea(repoDir, projDir, "  talvez um dia X  ", nil, nil, "")
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
	if _, err := CreateIdea(repoDir, projDir, " \n ", nil, nil, ""); !errors.Is(err, ErrIdeaTextRequired) {
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
	i, err := CreateIdea(repoDir, projDir, "com tags", []string{"code"}, []string{"Lint"}, "")
	if err != nil {
		t.Fatalf("CreateIdea: %v", err)
	}
	stored, _ := store.LoadIdea(repoDir, i.ID)
	if !slices.Equal(stored.Facets, []string{"code"}) || !slices.Equal(stored.Components, []string{"lint"}) {
		t.Fatalf("tags = %v / %v, want [code] / [lint]", stored.Facets, stored.Components)
	}
	if _, err := CreateIdea(repoDir, projDir, "faceta ruim", []string{"nope"}, nil, ""); !errors.Is(err, domain.ErrUnknownFacet) {
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
		if _, err := CreateIdea(repoDir, projDir, text, nil, nil, ""); err != nil {
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
	i, err := CreateIdea(repoDir, projDir, "talvez", nil, nil, "")
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
	i, err := CreateIdea(repoDir, projDir, "disputada", nil, nil, "")
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

// Crit. 6: promote mints a candidate from the idea — first line as title, the
// text as the spec's context with the [[I-NNNN]] back-link, tags inherited —
// and the idea records promoted_to and leaves the active list.
func TestPromoteIdeaCreatesLinkedCandidate(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "Exportar o kanban em CSV\ncom filtros", nil, []string{"kanban"}, "")
	if err != nil {
		t.Fatal(err)
	}
	got, f, err := PromoteIdea(repoDir, projDir, i.ID, "", "", false)
	if err != nil {
		t.Fatalf("PromoteIdea: %v", err)
	}
	if !strings.HasPrefix(f.ID, "F-") || f.State != domain.StateCandidate || f.Title != "Exportar o kanban em CSV" ||
		!slices.Equal(f.Components, []string{"kanban"}) {
		t.Fatalf("feature = %+v, want a candidate titled by the first line, component kanban", f)
	}
	spec := promotedSpec(t, repoDir, f.ID)
	for _, want := range []string{"**Origem:** ideia [[I-0001]] (capturada em " + i.CreatedAt.Format(time.DateOnly) + ")", "com filtros"} {
		if !strings.Contains(spec, want) {
			t.Fatalf("spec lacks %q:\n%s", want, spec)
		}
	}
	if got.State != domain.IdeaPromoted || got.PromotedTo != f.ID || got.ClosedAt == nil {
		t.Fatalf("idea = %+v, want promoted to %s", got, f.ID)
	}
	if active, _ := ListIdeas(repoDir, false, ""); len(active) != 0 {
		t.Fatalf("a promoted idea is still active: %+v", active)
	}
	types, payloads := ideaEvents(t, repoDir, i.ID)
	if !slices.Equal(types, []string{"idea.created", "idea.promoted"}) || payloads[1]["promoted_to"] != f.ID {
		t.Fatalf("idea events = %v %v, want idea.promoted with promoted_to", types, payloads)
	}
	createdPayload(t, repoDir, f.ID, "feature.created")

	long, err := CreateIdea(repoDir, projDir, strings.Repeat("a", 200), nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_, f2, err := PromoteIdea(repoDir, projDir, long.ID, "", "", false)
	if err != nil {
		t.Fatalf("PromoteIdea long: %v", err)
	}
	if n := utf8.RuneCountInString(f2.Title); n != 81 || !strings.HasSuffix(f2.Title, "…") {
		t.Fatalf("long title = %q (%d runes), want 80 + …", f2.Title, n)
	}
}

// Crit. 7: promoting a closed idea is refused before any feature is created,
// naming the state and the existing feature.
func TestPromoteIdeaRefusesClosed(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "Promovida uma vez", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_, f, err := PromoteIdea(repoDir, projDir, i.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.LoadAllFeatures(repoDir)
	_, _, err = PromoteIdea(repoDir, projDir, i.ID, "", "", false)
	if !errors.Is(err, ErrIdeaNotActive) || !strings.Contains(err.Error(), "promoted") || !strings.Contains(err.Error(), f.ID) {
		t.Fatalf("second promote err = %v, want ErrIdeaNotActive naming promoted and %s", err, f.ID)
	}
	d, err := CreateIdea(repoDir, projDir, "Descartada", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DropIdea(repoDir, projDir, d.ID, "não"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PromoteIdea(repoDir, projDir, d.ID, "", "", false); !errors.Is(err, ErrIdeaNotActive) || !strings.Contains(err.Error(), "dropped") {
		t.Fatalf("promote of a dropped idea err = %v, want ErrIdeaNotActive naming dropped", err)
	}
	if after, _ := store.LoadAllFeatures(repoDir); len(after) != len(before) {
		t.Fatalf("a refused promote created a feature: %d -> %d", len(before), len(after))
	}
}

// C6: an id that is not I-NNNN is refused before it touches a path — a
// traversal like ../works/W-0001 used to load the work as an "active idea"
// and plant entities/ideas/W-0001.json. Case is normalized.
func TestIdeaIDValidatedBeforeAnyPath(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if err := store.SaveWork(repoDir, store.Work{ID: "W-0001", Title: "x", State: domain.WorkActive}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	for _, id := range []string{"../works/W-0001", "F-0001", "I-12", "I-0001/x"} {
		if _, err := DropIdea(repoDir, projDir, id, ""); !errors.Is(err, ErrNotIdeaID) {
			t.Fatalf("DropIdea(%q) err = %v, want ErrNotIdeaID", id, err)
		}
		if _, _, err := PromoteIdea(repoDir, projDir, id, "", "", false); !errors.Is(err, ErrNotIdeaID) {
			t.Fatalf("PromoteIdea(%q) err = %v, want ErrNotIdeaID", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(repoDir, "entities", "ideas", "W-0001.json")); err == nil {
		t.Fatal("a traversal planted entities/ideas/W-0001.json")
	}
	if _, err := CreateIdea(repoDir, projDir, "minúscula", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := DropIdea(repoDir, projDir, "i-0001", ""); err != nil || got.ID != "I-0001" {
		t.Fatalf("DropIdea(i-0001) = %+v, %v; want I-0001 dropped", got, err)
	}
}

// C7: --search matches when every word is present, in any order, folding case
// and accents — a subject is several words, not one phrase.
func TestListIdeasSearchEveryWord(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	for _, text := range []string{"zebraquux plain", "Ação rápida — émoji", "só zebraquux"} {
		if _, err := CreateIdea(repoDir, projDir, text, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	for term, want := range map[string][]string{
		"plain zebraquux": {"I-0001"},
		"rapida EMOJI":    {"I-0002"},
		"zebraquux":       {"I-0003", "I-0001"},
		"zebraquux nada":  nil,
	} {
		got, err := ListIdeas(repoDir, false, term)
		var ids []string
		for _, i := range got {
			ids = append(ids, i.ID)
		}
		if err != nil || !slices.Equal(ids, want) {
			t.Fatalf("search %q = %v, %v; want %v", term, ids, err, want)
		}
	}
}

// C1: promote racing drop. Checking the state, minting the candidate and
// marking the idea happen in ONE mutation under the backend lock, so exactly
// one side wins and a lost promote never leaves an orphan candidate (the old
// two-step promote did, about one round in six).
func TestPromoteIdeaRaceDropNoOrphan(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	for n := 0; n < 20; n++ {
		i, err := CreateIdea(repoDir, projDir, fmt.Sprintf("alvo de corrida %d", n), nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		before, _ := store.LoadAllFeatures(repoDir)
		var perr, derr error
		var f store.Feature
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, f, perr = PromoteIdea(repoDir, projDir, i.ID, "", "", true) }()
		go func() { defer wg.Done(); _, derr = DropIdea(repoDir, projDir, i.ID, "corrida") }()
		wg.Wait()
		stored, _ := store.LoadIdea(repoDir, i.ID)
		after, _ := store.LoadAllFeatures(repoDir)
		switch {
		case perr == nil && errors.Is(derr, ErrIdeaNotActive):
			if stored.State != domain.IdeaPromoted || stored.PromotedTo != f.ID || len(after) != len(before)+1 {
				t.Fatalf("round %d: promote won but idea = %+v, features %d -> %d", n, stored, len(before), len(after))
			}
		case derr == nil && errors.Is(perr, ErrIdeaNotActive):
			if stored.State != domain.IdeaDropped || len(after) != len(before) {
				t.Fatalf("round %d: drop won but left %d orphan candidate(s) (promote err: %v)", n, len(after)-len(before), perr)
			}
		default:
			t.Fatalf("round %d: promote err %v, drop err %v — want exactly one winner", n, perr, derr)
		}
	}
}

// C2: a capture whose push landed but whose tail failed (here the snapshot:
// .docs is a file, which defeats root too) is retried with the same token —
// the retry returns the idea already in the backend instead of minting a twin.
func TestCreateIdeaRetryAfterPostPushFailureIsIdempotent(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	docs := filepath.Join(projDir, ".docs")
	if err := os.WriteFile(docs, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateIdea(repoDir, projDir, "sonda pós-push", nil, nil, "tok-1"); err == nil {
		t.Fatal("the snapshot failure was not reported")
	}
	if _, ok := originHas(t, origin, "entities/ideas/I-0001.json"); !ok {
		t.Fatal("fixture: the first attempt did not reach the origin")
	}
	if err := os.Remove(docs); err != nil {
		t.Fatal(err)
	}
	again, err := CreateIdea(repoDir, projDir, "sonda pós-push", nil, nil, "tok-1")
	if err != nil || again.ID != "I-0001" {
		t.Fatalf("retry = %+v, %v; want the existing I-0001", again, err)
	}
	if all, _ := store.LoadAllIdeas(repoDir); len(all) != 1 {
		t.Fatalf("the retry minted a twin: %d ideas", len(all))
	}
	if other, err := CreateIdea(repoDir, projDir, "sonda pós-push", nil, nil, "tok-2"); err != nil || other.ID != "I-0002" {
		t.Fatalf("a new token = %+v, %v; want a new idea I-0002", other, err)
	}
}

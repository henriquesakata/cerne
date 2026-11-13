package app

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0389 crit. 1: --informs accepts an existing idea, refuses a missing one.
func TestCreateReferenceInformsIdea(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "uma ideia", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", []string{i.ID}, nil, nil, false)
	if err != nil {
		t.Fatalf("informs an existing idea: %v", err)
	}
	if !slices.Equal(r.Informs, []string{i.ID}) {
		t.Fatalf("informs = %v", r.Informs)
	}
	_, err = CreateExternalReference(repoDir, projDir, "Outra", "http://b", "c", []string{"I-9999"}, nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "idea I-9999 not found in backend") {
		t.Fatalf("err = %v, want idea I-9999 not found", err)
	}
}

// F-0389 crit. 2/3: link adds without duplicating, unlink removes, events logged.
func TestLinkReference(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "uma ideia", nil, nil, "")
	r, err := CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", []string{i.ID}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := LinkReference(repoDir, projDir, r.ID, []string{i.ID, "  "}, nil)
	if err != nil {
		t.Fatalf("LinkReference: %v", err)
	}
	if !slices.Equal(got.Informs, []string{i.ID}) {
		t.Fatalf("duplicated: %v", got.Informs)
	}
	f, _ := CreateFeature(repoDir, projDir, "Uma feature", "", "", nil, nil)
	got, _, err = LinkReference(repoDir, projDir, r.ID, []string{f.ID}, nil)
	if err != nil || !slices.Equal(got.Informs, []string{i.ID, f.ID}) {
		t.Fatalf("add: %v %v", got.Informs, err)
	}
	if p := createdPayload(t, repoDir, r.ID, "reference.linked"); !strings.Contains(p["informs"], f.ID) {
		t.Fatalf("linked payload = %v", p)
	}
	if _, _, err := LinkReference(repoDir, projDir, r.ID, []string{"I-9999"}, nil); err == nil {
		t.Fatal("dangling add must be refused")
	}
	got, _, err = LinkReference(repoDir, projDir, r.ID, nil, []string{i.ID, "F-7777"})
	if err != nil || !slices.Equal(got.Informs, []string{f.ID}) {
		t.Fatalf("remove: %v %v", got.Informs, err)
	}
	if p := createdPayload(t, repoDir, r.ID, "reference.unlinked"); !strings.Contains(p["informs"], i.ID) {
		t.Fatalf("unlinked payload = %v", p)
	}
	st, _ := store.LoadReference(repoDir, r.ID)
	if !slices.Equal(st.Informs, []string{f.ID}) {
		t.Fatalf("persisted = %v", st.Informs)
	}
}

// A retired reference is refused.
func TestLinkReferenceRefusesRetired(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "uma ideia", nil, nil, "")
	r, _ := CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", nil, nil, nil, false)
	if _, err := RetireReference(repoDir, projDir, r.ID, "dup"); err != nil {
		t.Fatal(err)
	}
	_, _, err := LinkReference(repoDir, projDir, r.ID, []string{i.ID}, nil)
	if err == nil || !strings.Contains(err.Error(), "not current") {
		t.Fatalf("err = %v, want not current", err)
	}
}

// F-0389 crit. 4: promote adds the new feature to live references informing the idea.
func TestPromoteIdeaCarriesReferenceLinks(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "Exportar CSV", nil, nil, "")
	live, _ := CreateExternalReference(repoDir, projDir, "Viva", "http://a", "c", []string{i.ID}, nil, nil, false)
	dead, _ := CreateExternalReference(repoDir, projDir, "Morta", "http://b", "c", []string{i.ID}, nil, nil, false)
	if _, err := RetireReference(repoDir, projDir, dead.ID, "dup"); err != nil {
		t.Fatal(err)
	}
	_, f, err := PromoteIdea(repoDir, projDir, i.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.LoadReference(repoDir, live.ID)
	if !slices.Equal(got.Informs, []string{i.ID, f.ID}) {
		t.Fatalf("live informs = %v, want [%s %s]", got.Informs, i.ID, f.ID)
	}
	gd, _ := store.LoadReference(repoDir, dead.ID)
	if !slices.Equal(gd.Informs, []string{i.ID}) {
		t.Fatalf("retired informs = %v, want untouched", gd.Informs)
	}
	if p := createdPayload(t, repoDir, live.ID, "reference.linked"); p["via"] != "promote" || !strings.Contains(p["informs"], f.ID) {
		t.Fatalf("payload = %v", p)
	}
}

func linkEventCount(t *testing.T, repoDir, id string) int {
	t.Helper()
	evs, err := store.ReadAllEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range evs {
		if ev.EntityID == id && (ev.Type == "reference.linked" || ev.Type == "reference.unlinked") {
			n++
		}
	}
	return n
}

// Critic 1: a link that changes nothing commits nothing and emits no event,
// and reports the removed ids that were absent (critic 5).
func TestLinkReferenceNoOpCommitsNothing(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "uma ideia", nil, nil, "")
	r, _ := CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", []string{i.ID}, nil, nil, false)
	before, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, absent, err := LinkReference(repoDir, projDir, r.ID, []string{i.ID}, nil); err != nil || len(absent) != 0 {
		t.Fatalf("existing add: absent=%v err=%v", absent, err)
	}
	_, absent, err := LinkReference(repoDir, projDir, r.ID, nil, []string{"F-0042", "F-0042"})
	if err != nil || !slices.Equal(absent, []string{"F-0042"}) {
		t.Fatalf("absent unlink: absent=%v err=%v, want [F-0042] once", absent, err)
	}
	after, _ := gitback.HeadSHA(repoDir)
	if before != after {
		t.Fatalf("HEAD moved on a no-op: %s -> %s", before, after)
	}
	if n := linkEventCount(t, repoDir, r.ID); n != 0 {
		t.Fatalf("%d link events on a no-op", n)
	}
}

// Critic 4: an id in both lists is refused; the remove list is deduped.
func TestLinkReferenceBothListsAndDedupe(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "uma ideia", nil, nil, "")
	r, _ := CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", []string{i.ID}, nil, nil, false)
	_, _, err := LinkReference(repoDir, projDir, r.ID, []string{i.ID}, []string{i.ID})
	if err == nil || !strings.Contains(err.Error(), "--informs and --unlink both name "+i.ID) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := LinkReference(repoDir, projDir, r.ID, nil, []string{i.ID, i.ID}); err != nil {
		t.Fatal(err)
	}
	if p := createdPayload(t, repoDir, r.ID, "reference.unlinked"); p["informs"] != i.ID {
		t.Fatalf("payload = %v, want %s once", p, i.ID)
	}
}

// Critic 3: a dropped idea is a dead link; active and promoted are accepted.
func TestInformsIdeaStates(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	active, _ := CreateIdea(repoDir, projDir, "ativa", nil, nil, "")
	dropped, _ := CreateIdea(repoDir, projDir, "descartada", nil, nil, "")
	promoted, _ := CreateIdea(repoDir, projDir, "promovida", nil, nil, "")
	if _, err := DropIdea(repoDir, projDir, dropped.ID, "no"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PromoteIdea(repoDir, projDir, promoted.ID, "", "", false); err != nil {
		t.Fatal(err)
	}
	for n, id := range []string{active.ID, promoted.ID} {
		if _, err := CreateExternalReference(repoDir, projDir, "ok", "http://ok"+string(rune('a'+n)), "c", []string{id}, nil, nil, false); err != nil {
			t.Fatalf("%s must be accepted: %v", id, err)
		}
	}
	_, err := CreateExternalReference(repoDir, projDir, "no", "http://no", "c", []string{dropped.ID}, nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "--informs: idea "+dropped.ID+" is dropped") {
		t.Fatalf("dropped err = %v", err)
	}
	r, _ := CreateExternalReference(repoDir, projDir, "sem", "http://sem", "c", nil, nil, nil, false)
	if _, _, err := LinkReference(repoDir, projDir, r.ID, []string{dropped.ID}, nil); err == nil {
		t.Fatal("link to a dropped idea must be refused")
	}
}

// Critic 2: a corrupt reference makes the promote fail whole.
func TestPromoteIdeaAtomicWithCorruptReference(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, _ := CreateIdea(repoDir, projDir, "Exportar CSV", nil, nil, "")
	r, _ := CreateExternalReference(repoDir, projDir, "Viva", "http://a", "c", []string{i.ID}, nil, nil, false)
	bad := filepath.Join(repoDir, "entities", "references", "R-0099.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Committed, or the mutation's reset-to-HEAD would sweep it away.
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "corrupt"}} {
		if out, err := exec.Command("git", append([]string{"-C", repoDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	before, _ := gitback.HeadSHA(repoDir)
	if _, _, err := PromoteIdea(repoDir, projDir, i.ID, "", "", false); err == nil {
		t.Fatal("promote must fail on a corrupt reference")
	}
	if after, _ := gitback.HeadSHA(repoDir); after != before {
		t.Fatalf("HEAD moved: %s -> %s", before, after)
	}
	got, err := store.LoadIdea(repoDir, i.ID)
	if err != nil || got.State != domain.IdeaActive {
		t.Fatalf("idea = %+v, %v; want active", got, err)
	}
	if feats, _ := store.LoadAllFeatures(repoDir); len(feats) != 0 {
		t.Fatalf("a feature was created: %+v", feats)
	}
	if ref, _ := store.LoadReference(repoDir, r.ID); !slices.Equal(ref.Informs, []string{i.ID}) {
		t.Fatalf("reference changed: %v", ref.Informs)
	}
}

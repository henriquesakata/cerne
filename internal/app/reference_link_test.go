package app

import (
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
	got, err := LinkReference(repoDir, projDir, r.ID, []string{i.ID, "  "}, nil)
	if err != nil {
		t.Fatalf("LinkReference: %v", err)
	}
	if !slices.Equal(got.Informs, []string{i.ID}) {
		t.Fatalf("duplicated: %v", got.Informs)
	}
	f, _ := CreateFeature(repoDir, projDir, "Uma feature", "", "", nil, nil)
	got, err = LinkReference(repoDir, projDir, r.ID, []string{f.ID}, nil)
	if err != nil || !slices.Equal(got.Informs, []string{i.ID, f.ID}) {
		t.Fatalf("add: %v %v", got.Informs, err)
	}
	if p := createdPayload(t, repoDir, r.ID, "reference.linked"); !strings.Contains(p["informs"], f.ID) {
		t.Fatalf("linked payload = %v", p)
	}
	if _, err := LinkReference(repoDir, projDir, r.ID, []string{"I-9999"}, nil); err == nil {
		t.Fatal("dangling add must be refused")
	}
	got, err = LinkReference(repoDir, projDir, r.ID, nil, []string{i.ID, "F-7777"})
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
	_, err := LinkReference(repoDir, projDir, r.ID, []string{i.ID}, nil)
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

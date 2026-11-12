package app

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

const stepsBody = "# Runbook\n\n## S1. Primeiro\n\nTexto 1.\n\n## S2. Segundo\n\nTexto 2.\n"

func commitCount(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// F-0345 crit. 2: a list of steps writes one item per step, in ONE commit.
func TestAddErrataStepsWritesOneItemPerStepInOneCommit(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb, err := CreateRunbook(repoDir, projDir, "Rb", stepsBody)
	if err != nil {
		t.Fatal(err)
	}
	before := commitCount(t, repoDir)
	ids, err := AddErrataSteps(repoDir, projDir, rb.ID, "Texto da errata.", "ator", []string{"S1", "S2"}, false)
	if err != nil {
		t.Fatalf("AddErrataSteps: %v", err)
	}
	if len(ids) != 2 || ids[0] != "E1" || ids[1] != "E2" {
		t.Fatalf("ids = %v, want [E1 E2]", ids)
	}
	if got := commitCount(t, repoDir) - before; got != 1 {
		t.Fatalf("commits = %d, want exactly 1 for 2 items", got)
	}
	doc, _ := store.LoadErrata(repoDir, rb.ID)
	items := store.ParseErrataItems(doc)
	if len(items) != 2 || items[0].Target != "S1" || items[1].Target != "S2" {
		t.Fatalf("items = %+v", items)
	}
	if !strings.Contains(items[0].Text, "Texto da errata.") || !strings.HasSuffix(items[0].Text, "Parte de: E2") ||
		!strings.HasSuffix(items[1].Text, "Parte de: E1") {
		t.Fatalf("same text and a Parte de: line per item:\n%q\n%q", items[0].Text, items[1].Text)
	}
}

// F-0345 crit. 3: a step the live body lacks refuses the whole call, writing nothing.
func TestAddErrataStepsRefusesMissingStepWithoutWriting(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb, err := CreateRunbook(repoDir, projDir, "Rb", stepsBody)
	if err != nil {
		t.Fatal(err)
	}
	before := commitCount(t, repoDir)
	_, err = AddErrataSteps(repoDir, projDir, rb.ID, "x", "ator", []string{"S1", "S9"}, false)
	if err == nil || !strings.Contains(err.Error(), "S9") {
		t.Fatalf("err = %v, want one naming S9", err)
	}
	if commitCount(t, repoDir) != before {
		t.Fatal("a refused errata must not commit")
	}
	if doc, _ := store.LoadErrata(repoDir, rb.ID); len(store.ParseErrataItems(doc)) != 0 {
		t.Fatalf("a refused errata must not write:\n%s", doc)
	}
}

func errataEvents(t *testing.T, repoDir, id string) int {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == "errata.added" {
			n++
		}
	}
	return n
}

// F-0345 crit. 2: numbering continues after the items already there, each item
// names the other two, one commit, one errata.added event per item.
func TestAddErrataStepsNumbersAfterExistingItems(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb, err := CreateRunbook(repoDir, projDir, "Rb", stepsBody+"\n## S3. Terceiro\n\nTexto 3.\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddErrataSteps(repoDir, projDir, rb.ID, "Antiga.", "ator", []string{"S1", "S2"}, false); err != nil {
		t.Fatal(err)
	}
	commits, events := commitCount(t, repoDir), errataEvents(t, repoDir, rb.ID)
	ids, err := AddErrataSteps(repoDir, projDir, rb.ID, "Nova.", "ator", []string{"S1", "S2", "S3"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "E3,E4,E5" {
		t.Fatalf("ids = %v, want E3,E4,E5", ids)
	}
	if got := commitCount(t, repoDir) - commits; got != 1 {
		t.Fatalf("commits = %d, want 1", got)
	}
	if got := errataEvents(t, repoDir, rb.ID) - events; got != 3 {
		t.Fatalf("errata.added events = %d, want 3 (one per item)", got)
	}
	doc, _ := store.LoadErrata(repoDir, rb.ID)
	items := store.ParseErrataItems(doc)
	for i, want := range map[int]string{2: "Parte de: E4, E5", 3: "Parte de: E3, E5", 4: "Parte de: E3, E4"} {
		if !strings.HasSuffix(items[i].Text, want) {
			t.Errorf("%s text = %q, want suffix %q", items[i].ID, items[i].Text, want)
		}
	}
}

// F-0345: a repeated step is one item, not two pointing at each other; order is kept.
func TestAddErrataStepsDedupesSteps(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb, err := CreateRunbook(repoDir, projDir, "Rb", stepsBody)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := AddErrataSteps(repoDir, projDir, rb.ID, "x", "ator", []string{"S2", "S1", "S2"}, false)
	if err != nil || len(ids) != 2 {
		t.Fatalf("ids = %v err = %v, want 2 items", ids, err)
	}
	doc, _ := store.LoadErrata(repoDir, rb.ID)
	if items := store.ParseErrataItems(doc); items[0].Target != "S2" || items[1].Target != "S1" {
		t.Fatalf("order must be kept: %+v", items)
	}
}

// F-0345: `--altera -` is the no-step marker, not a step: refused.
func TestAddErrataStepsRefusesNoStepMarker(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb, err := CreateRunbook(repoDir, projDir, "Rb", stepsBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddErrataSteps(repoDir, projDir, rb.ID, "x", "ator", []string{store.NoStep}, false); err == nil || !strings.Contains(err.Error(), "not a step") {
		t.Fatal("--altera - must be refused")
	}
}

// F-0345: --sem-passo writes the deliberate marker.
func TestAddErrataStepsStepless(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb, err := CreateRunbook(repoDir, projDir, "Rb", stepsBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddErrataSteps(repoDir, projDir, rb.ID, "Nota geral.", "ator", nil, true); err != nil {
		t.Fatal(err)
	}
	doc, _ := store.LoadErrata(repoDir, rb.ID)
	items := store.ParseErrataItems(doc)
	if len(items) != 1 || !items[0].Stepless || items[0].Target != "" || items[0].Text != "Nota geral." {
		t.Fatalf("items = %+v", items)
	}
}

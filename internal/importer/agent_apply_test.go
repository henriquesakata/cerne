package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

func TestApplyPlanCreatesEachKind(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	ref := validItem("reference")
	ref.Informs = []string{"F-0001"}
	note := validItem("note")
	note.Informs = []string{"F-0001"}
	items := []PlanItem{validItem("feature"), validItem("decision"), ref, validItem("runbook"), note}

	sum, err := ApplyPlan(repo, items, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Features != 1 || sum.Decisions != 1 || sum.References != 1 || sum.Runbooks != 1 || sum.Notes != 1 {
		t.Fatalf("summary = %+v", sum)
	}

	// reference keeps url + external marker + informs
	r, err := store.LoadReference(repo, "R-0001")
	if err != nil || r.URL != "https://example.com" || !r.External || len(r.Informs) != 1 {
		t.Fatalf("reference = %+v err=%v", r, err)
	}
	// note keeps informs
	n, err := store.LoadNote(repo, "N-0001")
	if err != nil || len(n.Informs) != 1 || n.Informs[0] != "F-0001" {
		t.Fatalf("note = %+v err=%v", n, err)
	}
	// feature persisted with its proposed state
	f, err := store.LoadFeature(repo, "F-0001")
	if err != nil || f.Title != "T" || string(f.State) != "implemented" {
		t.Fatalf("feature = %+v err=%v", f, err)
	}

	// body written with cerne_id + imported_from frontmatter (idempotency anchor)
	body, err := os.ReadFile(filepath.Join(repo, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "cerne_id: F-0001") || !strings.Contains(string(body), "imported_from: feature.md") {
		t.Fatalf("feature content missing frontmatter:\n%s", body)
	}

	// one <kind>.imported event per item
	evs, err := store.ReadEvents(repo)
	if err != nil {
		t.Fatal(err)
	}
	imported := 0
	for _, e := range evs {
		if strings.HasSuffix(e.Type, ".imported") {
			imported++
		}
	}
	if imported != 5 {
		t.Fatalf("imported events = %d, want 5", imported)
	}
}

func TestApplyPlanUnknownKind(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	_, err := ApplyPlan(repo, []PlanItem{{Kind: "widget", Title: "X", State: "x", Body: "b", Source: "s"}}, "t")
	if err == nil {
		t.Fatal("ApplyPlan(unknown kind) = nil err, want error")
	}
}

package lint

import (
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestLintScansErrataOfImplementedFeature(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// F-0002 superseded → a ref-to-superseded needle.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0002", Title: "Velho", State: domain.StateSuperseded, SupersededBy: "F-0003", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0002: %v", err)
	}
	// F-0001 IMPLEMENTED (its spec is NOT scanned) whose errata mentions F-0002.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	if err := store.AppendErrata(dir, "F-0001", "esta feature ainda menciona F-0002", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, f := range refToSuperseded(findings) {
		if f.File == "content/errata/F-0001.md" && contains(f.Detail, "F-0002") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a ref-to-superseded finding on the implemented feature's errata, got: %+v", refToSuperseded(findings))
	}
}

func TestLintCleanWhenErrataMentionsLiveOnly(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0003", Title: "Novo", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0003: %v", err)
	}
	if err := store.AppendErrata(dir, "F-0001", "ver F-0003 para o fluxo atual", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("errata mentioning only live ids must produce no finding, got: %+v", refs)
	}
}

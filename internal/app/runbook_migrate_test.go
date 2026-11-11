package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// writeRefWithDoc creates a reference entity plus its content doc carrying the
// given imported_from, as `cerne import` would have left it.
func writeRefWithDoc(t *testing.T, dir, id, title, importedFrom, body string) {
	t.Helper()
	if err := store.SaveReference(dir, store.Reference{ID: id, Title: title, State: domain.ReferenceCurrent}); err != nil {
		t.Fatalf("SaveReference %s: %v", id, err)
	}
	doc := "---\ncerne_id: " + id + "\nimported_from: " + importedFrom + "\n---\n\n# " + title + "\n\n" + body + "\n"
	p := ReferenceDocPath(dir, id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateRunbooksFromReferences(t *testing.T) {
	dir := t.TempDir()
	writeRefWithDoc(t, dir, "R-0001", "Deploy", "docs/runbooks/deploy.md", "Steps to deploy.")
	writeRefWithDoc(t, dir, "R-0002", "Smoke", "docs/runbooks/smoke.md", "Smoke checks.")
	writeRefWithDoc(t, dir, "R-0003", "ADR ref", "docs/architecture/adr.md", "Not a runbook.")

	// a feature doc cross-linking R-0001 both as a path and as a bare mention
	featDoc := filepath.Join(dir, "content", "features", "F-0010", "result.md")
	if err := os.MkdirAll(filepath.Dir(featDoc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(featDoc, []byte("See [deploy](../../references/R-0001.md) and R-0001 for ops.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := MigrateRunbooksFromReferences(dir)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("migrated %d, want 2 (%+v)", len(got), got)
	}

	// Crit. 1 — runbooks created with the right ids/titles
	rbs, err := store.LoadAllRunbooks(dir)
	if err != nil {
		t.Fatalf("LoadAllRunbooks: %v", err)
	}
	if len(rbs) != 2 || rbs[0].ID != "RB-0001" || rbs[0].Title != "Deploy" || rbs[0].State != domain.RunbookActive {
		t.Fatalf("runbooks wrong: %+v", rbs)
	}

	// Crit. 1 — old runbook refs gone; Crit. 3 — non-runbook ref kept
	if _, err := store.LoadReference(dir, "R-0001"); err == nil {
		t.Fatal("R-0001 still present, want deleted")
	}
	if _, err := store.LoadReference(dir, "R-0003"); err != nil {
		t.Fatalf("R-0003 (non-runbook) should remain: %v", err)
	}

	// Crit. 1 — content doc moved with rewritten cerne_id, old removed
	if _, err := os.Stat(ReferenceDocPath(dir, "R-0001")); !os.IsNotExist(err) {
		t.Fatalf("old ref doc still present (err=%v)", err)
	}
	rbDoc, err := os.ReadFile(RunbookDocPath(dir, "RB-0001"))
	if err != nil {
		t.Fatalf("runbook doc: %v", err)
	}
	if !strings.Contains(string(rbDoc), "cerne_id: RB-0001") || strings.Contains(string(rbDoc), "cerne_id: R-0001") {
		t.Fatalf("cerne_id not rewritten: %s", rbDoc)
	}

	// Crit. 2 — cross-links rewritten (path + bare id)
	feat, err := os.ReadFile(featDoc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(feat), "../../runbooks/RB-0001.md") {
		t.Fatalf("link path not rewritten: %s", feat)
	}
	if strings.Contains(string(feat), "R-0001") {
		t.Fatalf("bare id not rewritten: %s", feat)
	}

	// Crit. 1 — one runbook.migrated event per migration
	evs, err := store.ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	migrated := 0
	for _, e := range evs {
		if e.Type == "runbook.migrated" {
			migrated++
		}
	}
	if migrated != 2 {
		t.Fatalf("runbook.migrated events = %d, want 2", migrated)
	}
}

// Crit. 2 — moving a doc between sibling dirs must not dangle its relative links:
// a moved doc's same-dir links to non-migrated refs become ../references/..., its
// links to other migrated refs stay same-dir (runbooks/), and a non-moved ref's
// same-dir link to a migrated one becomes ../runbooks/...
func TestMigrateRunbooksRewritesRelativeLinks(t *testing.T) {
	dir := t.TempDir()
	writeRefWithDoc(t, dir, "R-0001", "A", "docs/runbooks/a.md", "See [arch](R-0009.md) and [b](R-0002.md).")
	writeRefWithDoc(t, dir, "R-0002", "B", "docs/runbooks/b.md", "Runbook B.")
	writeRefWithDoc(t, dir, "R-0009", "Arch", "docs/architecture/x.md", "Not a runbook.")
	writeRefWithDoc(t, dir, "R-0010", "Other", "docs/other/y.md", "Related: [a](R-0001.md).")

	if _, err := MigrateRunbooksFromReferences(dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rb1, err := os.ReadFile(RunbookDocPath(dir, "RB-0001"))
	if err != nil {
		t.Fatalf("RB-0001 doc: %v", err)
	}
	if s := string(rb1); !strings.Contains(s, "(../references/R-0009.md)") {
		t.Fatalf("moved doc's same-dir link to a non-migrated ref not re-relativized:\n%s", s)
	}
	if s := string(rb1); !strings.Contains(s, "(RB-0002.md)") {
		t.Fatalf("moved doc's link to another migrated runbook wrong:\n%s", s)
	}

	other, err := os.ReadFile(ReferenceDocPath(dir, "R-0010"))
	if err != nil {
		t.Fatalf("R-0010 doc: %v", err)
	}
	if !strings.Contains(string(other), "(../runbooks/RB-0001.md)") {
		t.Fatalf("non-moved ref's link to a migrated ref not retargeted:\n%s", other)
	}
}

// Crit. 4 — a second run finds nothing to migrate.
func TestMigrateRunbooksIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeRefWithDoc(t, dir, "R-0001", "Deploy", "docs/runbooks/deploy.md", "Steps.")
	if _, err := MigrateRunbooksFromReferences(dir); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	got, err := MigrateRunbooksFromReferences(dir)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("second run migrated %d, want 0", len(got))
	}
}

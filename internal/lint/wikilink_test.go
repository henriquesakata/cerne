package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestWikiLinkFindings pins F-0167 C2/C3 at the lint layer: an id-form
// wiki-link whose id is not a known entity is a broken-link finding with a
// wiki detail; a resolvable id (any kind, case-insensitive, historical
// included) is silent; non-id [[...]] text and code spans are never findings.
func TestWikiLinkFindings(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	f := store.Feature{ID: "F-0001", Title: "Carteira", State: domain.StateCandidate,
		Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	n := store.Note{ID: "N-0001", Title: "Nota viva", State: domain.NoteActive,
		Facets: []string{"process"}, Components: []string{"cli"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveNote(dir, n); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	h := store.Historical{ID: "H-0001", Name: "Bills", SupersededBy: "F-0001",
		Reason: "absorvida", State: domain.StateSuperseded, CreatedAt: now}
	if err := store.SaveHistorical(dir, h); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}

	writeDoc(t, dir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Carteira\n\n"+
			"Cita [[N-0001]] e [[n-0001]] (caixa), o histórico [[H-0001]],\n"+
			"o quebrado [[N-9999]], um [[título qualquer]] e o span `[[X-0001]]`.\n")

	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var wiki []Finding
	for _, fd := range got {
		if fd.Rule == RuleBrokenLink && strings.Contains(fd.Detail, "wiki-link") {
			wiki = append(wiki, fd)
		}
	}
	if len(wiki) != 1 {
		t.Fatalf("wiki findings = %+v, want exactly one for [[N-9999]]", wiki)
	}
	want := Finding{Rule: RuleBrokenLink, Entity: "F-0001", File: "content/features/F-0001/spec.md",
		Line: 8, Detail: "broken wiki-link [[N-9999]] — no such id; the last N- id is N-0001: point the link to an existing id or remove it"}
	if wiki[0] != want {
		t.Fatalf("finding = %+v, want %+v", wiki[0], want)
	}
}

// TestWikiLinkFindingsEpicTask pins criterion 5 of F-0203: [[E-NNNN]] and
// [[T-NNNN]] resolve like any id-form wiki-link (E-/T- are features), and an
// unknown E-/T- id is a broken-link finding.
func TestWikiLinkFindingsEpicTask(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	for _, id := range []string{"F-0001", "E-0001", "T-0001"} {
		f := store.Feature{ID: id, Title: "Entidade " + id, State: domain.StateCandidate,
			CreatedAt: now, UpdatedAt: now}
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature(%s): %v", id, err)
		}
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Spec\n\n"+
			"Cita [[E-0001]] e [[T-0001]] (conhecidos) e os quebrados [[E-9999]] e [[T-9999]].\n")

	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var details []string
	for _, fd := range got {
		if fd.Rule == RuleBrokenLink && strings.Contains(fd.Detail, "wiki-link") {
			details = append(details, fd.Detail)
		}
	}
	want := []string{"broken wiki-link [[E-9999]] — no such id; the last E- id is E-0001: point the link to an existing id or remove it", "broken wiki-link [[T-9999]] — no such id; the last T- id is T-0001: point the link to an existing id or remove it"}
	if len(details) != 2 || details[0] != want[0] || details[1] != want[1] {
		t.Fatalf("wiki findings = %v, want %v", details, want)
	}
}

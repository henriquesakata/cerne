package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/dectmpl"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/plantmpl"
	"github.com/henriquesakata/cerne/internal/restmpl"
	"github.com/henriquesakata/cerne/internal/runtmpl"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// docWith renders a real lifecycle doc body with the given sections as level-2
// headings (each with content), omitting `omit`. extraFront, when set, is added
// to the frontmatter block (e.g. "imported_from: docs/x.md").
func docWith(sections []string, omit, extraFront string) string {
	fm := "cerne_id: X\n"
	if extraFront != "" {
		fm += extraFront + "\n"
	}
	var b strings.Builder
	b.WriteString("---\n" + fm + "---\n\n# Título\n\nResumo real do documento.\n\n")
	for _, s := range sections {
		if s == omit {
			continue
		}
		b.WriteString("## " + s + "\n\nconteúdo.\n\n")
	}
	return b.String()
}

// findFinding returns the first finding for file whose detail mentions section.
func findFinding(findings []Finding, file, section string) *Finding {
	for i := range findings {
		if findings[i].File == file && strings.Contains(findings[i].Detail, section) {
			return &findings[i]
		}
	}
	return nil
}

func TestIncompleteDocFindings(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	features := []store.Feature{
		{ID: "F-0001", Title: "A", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "B", State: domain.StateImplemented},
		{ID: "F-0003", Title: "C", State: domain.StateCandidate},
		{ID: "F-0004", Title: "D", State: domain.StateSuperseded},
		{ID: "F-0005", Title: "E", State: domain.StateInDevelopment},
		{ID: "F-0006", Title: "F", State: domain.StateApproved},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	// F-0001: complete spec; plan missing "Riscos e rollback" → one finding.
	writeDoc(t, dir, "content/features/F-0001/spec.md", docWith(spectmpl.Sections, "", ""))
	writeDoc(t, dir, "content/features/F-0001/plan.md", docWith(plantmpl.Sections, "Riscos e rollback", ""))
	// F-0002: implemented with an INCOMPLETE result → not flagged (settled history,
	// like the text-scan rules; only live work is enforced).
	writeDoc(t, dir, "content/features/F-0002/spec.md", docWith(spectmpl.Sections, "", ""))
	writeDoc(t, dir, "content/features/F-0002/plan.md", docWith(plantmpl.Sections, "", ""))
	writeDoc(t, dir, "content/features/F-0002/result.md", docWith(restmpl.Sections, "Aceitação", ""))
	// F-0003: stub spec (no real content) → skipped (gates' job).
	writeDoc(t, dir, "content/features/F-0003/spec.md", "---\ncerne_id: F-0003\n---\n\n# C\n\n<!-- COMO USAR -->\n")
	// F-0004: incomplete spec but terminal → skipped.
	writeDoc(t, dir, "content/features/F-0004/spec.md", docWith(spectmpl.Sections, "Verificação", ""))
	// F-0005: imported spec missing a section → grandfathered (no finding).
	writeDoc(t, dir, "content/features/F-0005/spec.md", docWith(spectmpl.Sections, "Desenho", "imported_from: docs/x.md"))
	// F-0006: live spec without the agent-execution section → finding (F-0266).
	writeDoc(t, dir, "content/features/F-0006/spec.md", docWith(spectmpl.Sections, "Execução recomendada (agente)", ""))

	decisions := []store.Decision{
		{ID: "D-0001", Title: "Dec", State: domain.DecisionActive},
		{ID: "D-0002", Title: "Old", State: domain.DecisionSuperseded},
	}
	for _, d := range decisions {
		d.CreatedAt, d.UpdatedAt = now, now
		if err := store.SaveDecision(dir, d); err != nil {
			t.Fatalf("SaveDecision: %v", err)
		}
	}
	// D-0001 active missing "Consequências" → finding; D-0002 terminal → skipped.
	writeDoc(t, dir, "content/decisions/D-0001.md", docWith(dectmpl.Sections, "Consequências", ""))
	writeDoc(t, dir, "content/decisions/D-0002.md", docWith(dectmpl.Sections, "Decisão", ""))

	runbooks := []store.Runbook{
		{ID: "RB-0001", Title: "RB", State: domain.RunbookActive},
	}
	for _, rb := range runbooks {
		rb.CreatedAt, rb.UpdatedAt = now, now
		if err := store.SaveRunbook(dir, rb); err != nil {
			t.Fatalf("SaveRunbook: %v", err)
		}
	}
	// RB-0001 complete → no finding.
	writeDoc(t, dir, "content/runbooks/RB-0001.md", docWith(runtmpl.Sections, "", ""))

	got, err := incompleteDocFindings(dir, features, decisions, runbooks)
	if err != nil {
		t.Fatalf("incompleteDocFindings: %v", err)
	}

	// Exactly three: F-0001 plan + D-0001 + F-0006 spec.
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(got), got)
	}
	for _, f := range got {
		if f.Rule != RuleIncompleteDoc {
			t.Errorf("finding rule = %q, want %q (%+v)", f.Rule, RuleIncompleteDoc, f)
		}
	}
	if findFinding(got, "content/features/F-0001/plan.md", "Riscos e rollback") == nil {
		t.Errorf("missing expected finding: F-0001 plan missing 'Riscos e rollback': %+v", got)
	}
	if findFinding(got, "content/decisions/D-0001.md", "Consequências") == nil {
		t.Errorf("missing expected finding: D-0001 missing 'Consequências': %+v", got)
	}
	if findFinding(got, "content/features/F-0006/spec.md", "Execução recomendada (agente)") == nil {
		t.Errorf("missing expected finding: F-0006 spec missing the agent-execution section: %+v", got)
	}
}

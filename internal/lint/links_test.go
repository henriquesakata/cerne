package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// linksLintBackend: candidate F-0003 whose spec says nothing of D-0002, a
// superseded decision (by D-0003) and an accepted link F-0003 → D-0002.
func linksLintBackend(t *testing.T, spec string, status string) string {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC()
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0003", Title: "Conta digital", State: domain.StateCandidate, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []store.Decision{
		{ID: "D-0002", Title: "Usar boleto legado", State: domain.DecisionSuperseded, SupersededBy: "D-0003"},
		{ID: "D-0003", Title: "Usar boleto novo", State: domain.DecisionActive, Facets: []string{"infra"}},
	} {
		d.CreatedAt, d.UpdatedAt = now, now
		if err := store.SaveDecision(dir, d); err != nil {
			t.Fatal(err)
		}
	}
	writeDoc(t, dir, "content/features/F-0003/spec.md", spec)
	err := store.SaveLinkSuggestions(dir, []store.LinkSuggestion{{N: 4, From: "F-0003", To: "D-0002", Excerpt: "x", Status: status}})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func refFindingsFor(t *testing.T, dir, entity string) []Finding {
	t.Helper()
	all, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []Finding
	for _, f := range all {
		if f.Rule == RuleRefToSuperseded && f.Entity == entity {
			out = append(out, f)
		}
	}
	return out
}

// TestRefToSupersededReadsAcceptedLinks: an accepted link to a superseded
// decision is flagged on its source, pointing at the successor and at the
// command that takes the relation back; an open or rejected one is not, and a
// mention in the text already flagging the same decision is not doubled.
func TestRefToSupersededReadsAcceptedLinks(t *testing.T) { // F-0502 crit. 8
	got := refFindingsFor(t, linksLintBackend(t, "# Conta\n\nPlain words.\n", store.LinkAccepted), "F-0003")
	if len(got) != 1 {
		t.Fatalf("findings = %+v, want 1", got)
	}
	f := got[0]
	if f.File != "links/suggestions.json" || f.Fix != "cerne links reject 4" ||
		!strings.Contains(f.Detail, "accepted link to D-0002") || !strings.Contains(f.Detail, "superseded") || !strings.Contains(f.Detail, "use D-0003") {
		t.Errorf("finding = %+v", f)
	}
	for _, st := range []string{store.LinkOpen, store.LinkRejected} {
		if got := refFindingsFor(t, linksLintBackend(t, "# Conta\n\nPlain words.\n", st), "F-0003"); len(got) != 0 {
			t.Errorf("%s link flagged: %+v", st, got)
		}
	}
	both := refFindingsFor(t, linksLintBackend(t, "# Conta\n\nSee D-0002 for the old way.\n", store.LinkAccepted), "F-0003")
	if len(both) != 1 || both[0].File == "links/suggestions.json" {
		t.Errorf("text mention + accepted link = %+v, want the one finding of the text", both)
	}
}

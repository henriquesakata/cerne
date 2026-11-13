package lint

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func agedPending(id, from string, state domain.PendingState, created time.Time) store.Pending {
	return store.Pending{ID: id, Title: "Débito " + id, From: from, State: state, CreatedAt: created, UpdatedAt: created}
}

// Crit. 11 and 12: an open pending older than StalePendingAfter is flagged with
// its id, origin and age; one just under it is not. The boundary is the named
// constant, never a literal, so moving the constant moves the finding.
func TestStalePendingAfterThreshold(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	pendings := []store.Pending{
		agedPending("P-0001", "F-0007", domain.PendingOpen, now.Add(-StalePendingAfter-time.Hour)),
		agedPending("P-0002", "F-0007", domain.PendingOpen, now.Add(-StalePendingAfter+time.Hour)),
	}
	got := stalePendingFindings(pendings, now)
	if len(got) != 1 || got[0].Rule != RuleStalePending {
		t.Fatalf("findings = %+v, want exactly one stale-pending", got)
	}
	days := int((StalePendingAfter + time.Hour).Hours() / 24)
	for _, want := range []string{"P-0001", "F-0007", fmt.Sprintf("%dd", days)} {
		if !strings.Contains(got[0].Detail, want) {
			t.Fatalf("detail %q does not cite %q", got[0].Detail, want)
		}
	}
	if !Advisory(RuleStalePending) || Advisory(RuleStaleWork) {
		t.Fatal("stale-pending must be the advisory (warning) rule; stale-work stays blocking")
	}
}

// Crit. 13: resolved, dropped and promoted pendings never produce a finding.
func TestStalePendingIgnoresClosed(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * StalePendingAfter)
	var pendings []store.Pending
	for i, s := range []domain.PendingState{domain.PendingResolved, domain.PendingDropped, domain.PendingPromoted} {
		pendings = append(pendings, agedPending(fmt.Sprintf("P-%04d", i+1), "F-0001", s, old))
	}
	if got := stalePendingFindings(pendings, now); len(got) != 0 {
		t.Fatalf("closed pendings produced findings: %+v", got)
	}
}

// Crit. 11 (F-0071/F-0306): the finding belongs to the ORIGIN, so a
// --feature/--parent scope on the origin keeps it and a scope on the P- id
// does not; RunConfig wires the rule and P- ids resolve as wiki-links.
func TestStalePendingAttributedToOrigin(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Origem", State: domain.StateCandidate, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePending(dir, agedPending("P-0001", "F-0001", domain.PendingOpen, now.Add(-StalePendingAfter-time.Hour))); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md", "---\ncerne_id: F-0001\n---\n\n# Origem\n\nDeixou [[P-0001]].\n")
	got, err := RunConfig(dir, 14, 8, 0)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	var stale []Finding
	for _, fnd := range got {
		if fnd.Rule == RuleStalePending {
			stale = append(stale, fnd)
		}
		if fnd.Rule == RuleBrokenLink && strings.Contains(fnd.Detail, "P-0001") {
			t.Fatalf("[[P-0001]] must resolve as a wiki-link: %+v", fnd)
		}
	}
	if len(stale) != 1 || stale[0].Entity != "F-0001" || stale[0].File != "entities/pendings/P-0001.json" {
		t.Fatalf("stale-pending = %+v, want one on entity F-0001, file entities/pendings/P-0001.json", stale)
	}
	if in, _ := Scope(stale, map[string]bool{"F-0001": true}); len(in) != 1 {
		t.Fatal("scope on the origin must keep the finding")
	}
	if in, _ := Scope(stale, map[string]bool{"P-0001": true}); len(in) != 0 {
		t.Fatal("the finding must never be attributed to the P- id")
	}
}

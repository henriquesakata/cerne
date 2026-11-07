package lint

import (
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// errataAckFixture: F-0001 (implemented) owns the errata; D-0002 and F-0002
// are superseded by third parties (D-0005, F-0003), so the own-predecessor
// tolerance (F-0094) never applies. F-0002 exists for the title needle, which
// decisions do not have.
func errataAckFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	at := time.Unix(0, 0).UTC()
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented, CreatedAt: at},
		{ID: "F-0002", Title: "Fluxo legado", State: domain.StateSuperseded, SupersededBy: "F-0003", CreatedAt: at},
		{ID: "F-0003", Title: "Fluxo novo", State: domain.StateImplemented, CreatedAt: at},
	} {
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	for _, d := range []store.Decision{
		{ID: "D-0002", Title: "Regra velha", State: domain.DecisionSuperseded, SupersededBy: "D-0005", CreatedAt: at, UpdatedAt: at},
		{ID: "D-0005", Title: "Regra nova", State: domain.DecisionActive, CreatedAt: at, UpdatedAt: at},
	} {
		if err := store.SaveDecision(dir, d); err != nil {
			t.Fatalf("SaveDecision %s: %v", d.ID, err)
		}
	}
	return dir
}

// refsIn returns the ref-to-superseded findings Run reports on file.
func refsIn(t *testing.T, dir, file string) []Finding {
	t.Helper()
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var out []Finding
	for _, f := range refToSuperseded(findings) {
		if f.File == file {
			out = append(out, f)
		}
	}
	return out
}

const ownErrata = "content/errata/F-0001.md"

// ackEntry is an errata entry acknowledging ids as history.
func ackEntry(ids string) string {
	return "<!-- lint:ok-superseded " + ids + " --> história\n"
}

// TestLintErrataLaterMarkerAcknowledges pins F-0329 crit. 1: a marker in a later
// entry acknowledges every mention of the id in the entries before it, by id or
// by title. The finding exists before the marker entry is appended and is gone
// after.
func TestLintErrataLaterMarkerAcknowledges(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pre, post []string
	}{
		{"id", []string{"siga D-0002\n"}, []string{ackEntry("D-0002")}},
		{"title", []string{"siga o Fluxo legado\n"}, []string{ackEntry("F-0002")}},
		{"two earlier entries", []string{"siga D-0002\n", "ainda D-0002\n"}, []string{ackEntry("D-0002")}},
		{"id list, mixed case", []string{"siga D-0002\n"}, []string{ackEntry("D-0003, d-0002")}},
		{"unrelated later entry", []string{"siga D-0002\n"}, []string{ackEntry("D-0002"), "outro assunto\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := errataAckFixture(t)
			addErrata(t, dir, tc.pre...)
			if got := refsIn(t, dir, ownErrata); len(got) != 1 {
				t.Fatalf("before the marker entry: want 1 finding, got %+v", got)
			}
			addErrata(t, dir, tc.post...)
			if got := refsIn(t, dir, ownErrata); len(got) != 0 {
				t.Fatalf("after the marker entry: want no finding, got %+v", got)
			}
		})
	}
}

// TestLintErrataEarlierMarkerDoesNotCoverLater pins F-0329 crit. 2: the
// acknowledgment looks back, never forward — a mention appended after the
// marker entry, or on another line of the marker's own entry, is still a debt.
func TestLintErrataEarlierMarkerDoesNotCoverLater(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []string
	}{
		{"marker before", []string{ackEntry("D-0002"), "siga D-0002\n"}},
		{"marker between two mentions", []string{"siga D-0002\n", ackEntry("D-0002"), "ainda siga D-0002\n"}},
		{"same entry, mention above marker", []string{"siga D-0002\n" + ackEntry("D-0002")}},
		{"same entry, mention below marker", []string{ackEntry("D-0002") + "siga D-0002\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := errataAckFixture(t)
			addErrata(t, dir, tc.entries...)
			got := refsIn(t, dir, ownErrata)
			if len(got) != 1 || !strings.Contains(got[0].Detail, "D-0002") {
				t.Fatalf("want 1 finding about D-0002, got %+v", got)
			}
		})
	}
}

// TestLintErrataMarkerForOtherID pins F-0329 crit. 3: a later marker naming
// another id — including a longer id sharing the prefix — acknowledges nothing.
func TestLintErrataMarkerForOtherID(t *testing.T) {
	for _, tc := range []struct{ name, marker string }{
		{"other id", ackEntry("D-0003")},
		{"longer id", ackEntry("D-00020")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := errataAckFixture(t)
			addErrata(t, dir, "siga D-0002\n", tc.marker)
			got := refsIn(t, dir, ownErrata)
			if len(got) != 1 || !strings.Contains(got[0].Detail, "D-0002") {
				t.Fatalf("want 1 finding about D-0002, got %+v", got)
			}
		})
	}
}

// TestLintMarkerOutsideErrataUnchanged pins F-0329 crit. 4: outside an errata
// the marker still counts only on its own line (F-0089) — even when the doc
// carries a heading shaped like an errata entry.
func TestLintMarkerOutsideErrataUnchanged(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"marked line after", "siga D-0002\n<!-- lint:ok-superseded D-0002 -->\n"},
		{"errata-shaped heading", "siga D-0002\n\n## errata 2026-01-01 — ator\n\n<!-- lint:ok-superseded D-0002 -->\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := errataAckFixture(t)
			writeLiveFeatureDoc(t, dir, "F-0004", tc.body)
			if got := refsIn(t, dir, "content/features/F-0004/spec.md"); len(got) != 1 {
				t.Fatalf("want 1 finding, got %+v", got)
			}
		})
	}
}

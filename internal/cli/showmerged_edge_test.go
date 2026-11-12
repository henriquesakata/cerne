package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedMergedItem writes an erratum already marked merged and, when evRev is set,
// the errata.merged event that folded it, as a hand-edited or older backend has them.
func seedMergedItem(t *testing.T, repoDir, meta string, hdr time.Time, evRev, evItems string, evAt time.Time) {
	t.Helper()
	if err := store.AppendErrata(repoDir, "RB-0001", "Altera: S6.6\n"+meta+"\n\nTexto fundido.", "agent", hdr); err != nil {
		t.Fatal(err)
	}
	if evRev != "" {
		ev := domain.NewEvent("RB-0001", "errata.merged", map[string]string{"revision": evRev, "items": evItems}, "agent")
		ev.At = evAt
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
}

type mergedShow struct {
	Merged []map[string]any `json:"merged_errata"`
}

func showMerged(t *testing.T, projDir string) (string, mergedShow) {
	t.Helper()
	var js mergedShow
	if err := json.Unmarshal([]byte(rb(t, projDir, "show", "RB-0001", "--json")), &js); err != nil {
		t.Fatal(err)
	}
	return rb(t, projDir, "show", "RB-0001"), js
}

// F-0351 rodada 1, A1: no event, no date; no revision anywhere, no revision.
func TestShowMergedErrataWithoutEventOrRevision(t *testing.T) {
	projDir, repoDir := runbookFixture(t)
	seedMergedItem(t, repoDir, "Fundida: revisão 2", time.Now().UTC(), "", "", time.Time{})
	text, js := showMerged(t, projDir)
	if !strings.Contains(text, "E1 merged into revision 2\n") || strings.Contains(text, "unknown") {
		t.Fatalf("no event: line without parenthesis:\n%s", text)
	}
	if _, has := js.Merged[0]["date"]; has || js.Merged[0]["revision"] != float64(2) {
		t.Fatalf("no event: json must omit date: %v", js.Merged)
	}

	projDir, repoDir = runbookFixture(t)
	seedMergedItem(t, repoDir, "Fundida: sim", time.Now().UTC(), "", "", time.Time{})
	text, js = showMerged(t, projDir)
	if !strings.Contains(text, "E1 merged\n") {
		t.Fatalf("no revision: bare line:\n%s", text)
	}
	if _, has := js.Merged[0]["revision"]; has {
		t.Fatalf("no revision: json must omit it: %v", js.Merged)
	}
}

// A1(a)+A2: a revision the item does not carry comes from the event, and the
// date is the event's `at`, not today nor the erratum's header date.
func TestShowMergedErrataTakesRevisionAndDateFromEvent(t *testing.T) {
	projDir, repoDir := runbookFixture(t)
	hdr := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	at := time.Date(2026, 3, 4, 23, 30, 0, 0, time.UTC)
	seedMergedItem(t, repoDir, "Fundida: sim", hdr, "7", "E1", at)
	text, js := showMerged(t, projDir)
	if !strings.Contains(text, "E1 merged into revision 7 (2026-03-04)\n") {
		t.Fatalf("revision and date from the event:\n%s", text)
	}
	if js.Merged[0]["date"] != "2026-03-04" || js.Merged[0]["revision"] != float64(7) {
		t.Fatalf("json: %v", js.Merged)
	}
}

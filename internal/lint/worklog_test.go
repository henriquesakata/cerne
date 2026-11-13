package lint

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0397: the work log is a literal record of what was said, not a document:
// lint reads nothing from content/worklogs/, whatever the text cites.
func TestLintSkipsWorkLogs(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\nsem links\n")
	if err := store.SaveLogEntry(dir, "W-0001", store.LogEntry{Seq: 1, UTC: "2026-11-13T15:33:00Z", Actor: "owner", RecordedBy: "a@h",
		Text: "veja [[N-9999]], [x](./nada.md) e https://exemplo.com/artigo"}); err != nil {
		t.Fatal(err)
	}
	got, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		if strings.Contains(f.File, "worklogs") || strings.Contains(f.Detail, "N-9999") || strings.Contains(f.Detail, "nada.md") {
			t.Fatalf("lint read the work log: %+v", f)
		}
	}
}

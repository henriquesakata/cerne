package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/index"
)

// now is the clock of the index-age note; tests swap it.
var now = time.Now

// indexStaleAfter is the age past which search and ask warn (F-0394). Fixed:
// it is the limit the hub's session start already uses.
const indexStaleAfter = 24 * time.Hour

// indexSyncedAt reads when the index was last synced, nil when unknown. A read
// failure counts as unknown: search must not fail over a missing note.
func indexSyncedAt(dbPath string) *time.Time {
	t, ok, err := index.SyncedAt(dbPath)
	if err != nil || !ok {
		return nil
	}
	return &t
}

// noteIndexAge prints, on stderr and before any result, that the index may
// miss what other machines wrote. A local-path backend has no other machine,
// so it stays silent there.
func noteIndexAge(w io.Writer, backend string, synced *time.Time) {
	if gitback.IsLocalPath(backend) {
		return
	}
	when := "last synced: unknown"
	if synced != nil {
		age := now().Sub(*synced)
		if age <= indexStaleAfter {
			return
		}
		when = fmt.Sprintf("last synced %dh ago", int(age.Hours()))
	}
	fmt.Fprintf(w, "note: search index %s; entities written on other machines may be missing (run cerne pull)\n", when)
}

// syncedAtJSON is the index_synced_at payload value: RFC 3339 or null.
func syncedAtJSON(synced *time.Time) *string {
	if synced == nil {
		return nil
	}
	s := synced.UTC().Format(time.RFC3339)
	return &s
}

package index

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
)

// ErrBusy is returned by PhraseNotes when the index is locked by a writer
// (a concurrent rebuild) past the short busy timeout.
var ErrBusy = errors.New("index busy")

// NoteHit is one active note whose title or body matched a phrase query. The
// body is returned so the caller can confirm the match by substring.
type NoteHit struct {
	ID, Title, Body string
}

// PhraseNotes returns the active notes whose title/body contain any of the
// phrases (F-0366). Each phrase is quoted in the FTS MATCH, so path
// punctuation is never query syntax. The index is opened read-only with a short
// busy timeout: the work-gate hook must never wait on a concurrent rebuild. A
// missing index yields nil, nil.
func PhraseNotes(ctx context.Context, dbPath string, phrases []string) ([]NoteHit, error) {
	if len(phrases) == 0 {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	quoted := make([]string, len(phrases))
	for i, p := range phrases {
		quoted[i] = quotePhrase(p)
	}
	u := url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&_pragma=busy_timeout(50)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT entity_id, title, body FROM docs WHERE docs MATCH ? AND kind = 'note' AND state = 'active' LIMIT 500`,
		"{title body} : ("+strings.Join(quoted, " OR ")+")")
	if err != nil {
		return nil, wrapBusy(err)
	}
	defer rows.Close()
	var hits []NoteHit
	for rows.Next() {
		var h NoteHit
		if err := rows.Scan(&h.ID, &h.Title, &h.Body); err != nil {
			return nil, wrapBusy(err)
		}
		hits = append(hits, h)
	}
	return hits, wrapBusy(rows.Err())
}

func wrapBusy(err error) error {
	if err == nil {
		return nil
	}
	if m := strings.ToLower(err.Error()); strings.Contains(m, "locked") || strings.Contains(m, "busy") {
		return ErrBusy
	}
	return err
}

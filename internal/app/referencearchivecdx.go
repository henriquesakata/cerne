package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// Captures lists the page's copies with a 200, one per day, from the CDX API:
// [["timestamp","original","statuscode"],["20191122194934","https://…","200"],…].
// A page with no copy is an empty list; a body past 4 MB is an error.
func (c WaybackClient) Captures(ctx context.Context, target string) ([]Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, waybackCapturesTimeout)
	defer cancel()
	q := url.Values{"url": {target}, "output": {"json"}, "fl": {"timestamp,original,statuscode"}, "filter": {"statuscode:200"}, "collapse": {"timestamp:8"}}
	resp, err := c.get(ctx, c.CDXURL+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpStatusError(resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var rows [][]string
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
	}
	var out []Snapshot
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		at, err := time.Parse(waybackTimestamp, row[0])
		if err != nil { // the header row, or a malformed one
			continue
		}
		out = append(out, Snapshot{URL: "https://web.archive.org/web/" + row[0] + "/" + row[1], CapturedAt: at})
	}
	return out, nil
}

func (n ArchiveNet) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

func distance(a, b time.Time) time.Duration {
	if d := a.Sub(b); d >= 0 {
		return d
	}
	return b.Sub(a)
}

// chooseCopy picks the copy a reference records (crit. 10): the capture nearest
// the registration day, taken as an existing copy when it is nearer than now;
// otherwise a new copy, and when the service refuses it, that nearest capture.
// A failed capture list only means there is no capture to choose.
func chooseCopy(ctx context.Context, r store.Reference, n ArchiveNet) store.ReferenceArchive {
	caps, _ := n.Client.Captures(ctx, r.URL)
	var near *Snapshot
	for i, c := range caps {
		if near == nil || distance(c.CapturedAt, r.CreatedAt) < distance(near.CapturedAt, r.CreatedAt) {
			near = &caps[i]
		}
	}
	existing := func(s Snapshot) store.ReferenceArchive {
		return store.ReferenceArchive{URL: s.URL, CapturedAt: s.CapturedAt.UTC(), Origin: "existing", Status: archiveArchived}
	}
	now := n.now()
	if near != nil && distance(near.CapturedAt, r.CreatedAt) < distance(now, r.CreatedAt) {
		return existing(*near)
	}
	s, err := n.Client.Save(ctx, r.URL)
	switch {
	case err == nil && s.CapturedAt.Before(now.Add(-time.Minute)):
		return existing(s) // the service reused a copy
	case err == nil:
		return store.ReferenceArchive{URL: s.URL, CapturedAt: s.CapturedAt.UTC(), Origin: "saved", Status: archiveArchived}
	case near != nil:
		return existing(*near)
	}
	return store.ReferenceArchive{Status: archivePending, Reason: "save failed: " + saveFailure(err)}
}

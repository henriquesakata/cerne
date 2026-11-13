package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Work log (F-0397): the decisions given during a work, append-only, one file
// per entry at content/worklogs/<W-id>/<seq>.json. UTC is stored; readers
// render their own zone.

// ErrLogEntryExists is returned when an entry number is already taken.
var ErrLogEntryExists = errors.New("work log entry already exists")

// LogEntry is one recorded decision. Actor is who the decision came from as
// declared by the writer; RecordedBy is the session identity that wrote it.
type LogEntry struct {
	Seq        int    `json:"seq"`
	UTC        string `json:"utc"` // RFC 3339, UTC
	Actor      string `json:"actor"`
	RecordedBy string `json:"recorded_by"`
	Text       string `json:"text"`
	Corrects   int    `json:"corrects,omitempty"`
}

func logDir(repoDir, work string) string {
	return filepath.Join(repoDir, "content", "worklogs", work)
}

// NextLogSeq is the first free entry number of work (1 when it has no log);
// an unreadable entry is an error, not a free number.
func NextLogSeq(repoDir, work string) (int, error) {
	es, err := LoadLog(repoDir, work)
	if err != nil {
		return 0, err
	}
	if n := len(es); n > 0 {
		return es[n-1].Seq + 1, nil
	}
	return 1, nil
}

// SaveLogEntry writes e; ErrLogEntryExists when its number is taken.
func SaveLogEntry(repoDir, work string, e LogEntry) error {
	dir := logDir(repoDir, work)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, strconv.Itoa(e.Seq)+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s #%d", ErrLogEntryExists, work, e.Seq)
	}
	if err != nil {
		return err
	}
	_, werr := f.Write(append(data, '\n'))
	return errors.Join(werr, f.Close())
}

// LoadLog returns work's entries in numeric order of Seq; empty when none.
func LoadLog(repoDir, work string) ([]LogEntry, error) {
	files, err := os.ReadDir(logDir(repoDir, work))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(logDir(repoDir, work), f.Name()))
		if err != nil {
			return nil, err
		}
		var e LogEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("work log %s/%s: %w", work, f.Name(), err)
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b LogEntry) int { return a.Seq - b.Seq })
	return out, nil
}

package store

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"
)

var handoffHeaderRe = regexp.MustCompile(`^## handoff (\d{4}-\d{2}-\d{2}) — (.*)$`)

// HandoffEntry is one session handoff of a work (F-0339): what the session
// leaving wrote for the one arriving.
type HandoffEntry struct {
	Date   string `json:"date"` // YYYY-MM-DD (UTC)
	Author string `json:"author"`
	Text   string `json:"text"`
}

// String renders the entry as it is stored: header, blank line, text.
func (h HandoffEntry) String() string {
	return "## handoff " + h.Date + " — " + h.Author + "\n\n" + h.Text
}

// AppendHandoff appends one dated handoff entry to work id's
// content/works/<id>/handoff.md — append-only, like the errata.
func AppendHandoff(repoDir, id, text, author string, at time.Time) error {
	return appendEntry(handoffPath(repoDir, id), id, "handoff", text, author, at.UTC())
}

// LoadHandoffs reads work id's handoff entries, oldest first. A missing file is
// "no handoffs", not an error.
func LoadHandoffs(repoDir, id string) ([]HandoffEntry, error) {
	data, err := os.ReadFile(handoffPath(repoDir, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []HandoffEntry
	var body []string
	flush := func() {
		if n := len(out); n > 0 {
			out[n-1].Text = strings.TrimSpace(strings.Join(body, "\n"))
		}
		body = nil
	}
	for _, l := range strings.Split(string(data), "\n") {
		if m := handoffHeaderRe.FindStringSubmatch(strings.TrimRight(l, "\r")); m != nil {
			flush()
			out = append(out, HandoffEntry{Date: m[1], Author: strings.TrimSpace(m[2])})
			continue
		}
		body = append(body, l)
	}
	flush()
	return out, nil
}

package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Card notices (F-0504): when a human moves a card, every session that worked
// on it hears so at its next contact point, with no `cerne feature wait`
// running. One file per session, its own format beside the channel: the
// release binary the hooks run never reads it (N-0134).
//
// ponytail: the file of a session that never comes back stays; a cleanup
// sweep on write when the notices dir grows.

const noticesDir = "notices"

// notice is one line of <cache>/notices/<session>.ndjson.
type notice struct {
	Entity string    `json:"entity"`
	Line   string    `json:"line"`
	At     time.Time `json:"at"`
}

func noticePath(cacheDir, sess string) string {
	return filepath.Join(cacheDir, noticesDir, sess+".ndjson")
}

// appendNotice adds n to session sess's file. One Write of the whole line
// keeps concurrent appends from interleaving.
func appendNotice(cacheDir, sess string, n notice) error {
	if err := safeSessionID(sess); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, noticesDir), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(n)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(noticePath(cacheDir, sess), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// TakeNotices is session sess's pending notices as one block, deleted as they
// are read, or "" — none, an unsafe id, any error (a hook is fail-open,
// N-0021). The file is renamed away first, so two contact points at once show
// each notice once, and a notice written meanwhile lands in a new file for the
// next contact. A notice older than the channel TTL is dropped unseen.
func TakeNotices(cacheDir, sess string, now time.Time) string {
	if safeSessionID(sess) != nil {
		return ""
	}
	src := noticePath(cacheDir, sess)
	if _, err := os.Stat(src); err != nil {
		return "" // hot path: nothing pending
	}
	tmp, err := os.CreateTemp(filepath.Dir(src), sess+".taking-*")
	if err != nil {
		return ""
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if os.Rename(src, tmp.Name()) != nil {
		return "" // another contact point took it
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return ""
	}
	var b strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		var n notice
		if json.Unmarshal(sc.Bytes(), &n) != nil || now.Sub(n.At) > channelTTL {
			continue
		}
		b.WriteString("- " + n.Line + "\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "cerne: card state changed since this session last looked:\n" + b.String()
}

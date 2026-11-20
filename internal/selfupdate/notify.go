package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const checkEvery = 24 * time.Hour

type checkFile struct {
	Base      string    `json:"base"`
	Tag       string    `json:"tag"` // empty: the check found nothing to announce
	CheckedAt time.Time `json:"checked_at"`
}

// NoticeDir is where the release check and the notice markers live, shared by
// every project of the machine: <user cache>/cerne, or CERNE_CACHE_DIR.
func NoticeDir() (string, error) {
	if d := os.Getenv("CERNE_CACHE_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "cerne"), nil
}

// LatestCached is the latest release tag for the session-start notice (F-0479):
// it asks the network only when dir's update-check.json is missing, older than
// 24 hours or about another base, within timeout. A failure and a prerelease
// are remembered like an answer (an entry without a tag), so an offline
// machine costs one request a day, not one per session; when even the file
// cannot be written the process remembers it in memory. A remembered failure
// is an error, a prerelease is never announced.
func LatestCached(base, dir string, now time.Time, timeout time.Duration) (string, error) {
	path := filepath.Join(dir, "update-check.json")
	fresh := func(c checkFile) bool {
		return c.Base == base && now.Sub(c.CheckedAt) < checkEvery && now.After(c.CheckedAt.Add(-checkEvery))
	}
	answer := func(c checkFile) (string, error) {
		if c.Tag == "" {
			return "", fmt.Errorf("%w: the last check, %s, found no release to announce", ErrNoRelease, c.CheckedAt.Format(time.RFC3339))
		}
		return c.Tag, nil
	}
	key := dir + "\x00" + base
	if v, ok := checked.Load(key); ok && fresh(v.(checkFile)) {
		return answer(v.(checkFile))
	}
	if b, err := os.ReadFile(path); err == nil {
		var c checkFile
		if json.Unmarshal(b, &c) == nil && fresh(c) {
			return answer(c)
		}
	}
	c := checkFile{Base: base, CheckedAt: now}
	hc := *client
	hc.Timeout = timeout
	tag, err := latestWith(&hc, base)
	if err == nil {
		if m := semverRe.FindStringSubmatch(tag); m != nil && m[4] == "" {
			c.Tag = tag
		}
	}
	if !keep(dir, path, c) {
		checked.Store(key, c)
	}
	if err != nil {
		return "", err
	}
	return answer(c)
}

// checked is the per-process fallback when the cache file cannot be written.
var checked sync.Map

// keep writes the entry through a temp file and a rename (N-0129).
func keep(dir, path string, c checkFile) bool {
	b, _ := json.Marshal(c)
	if os.MkdirAll(dir, 0o755) != nil {
		return false
	}
	f, err := os.CreateTemp(dir, "update-check-*.tmp")
	if err != nil {
		return false
	}
	_, werr := f.Write(b)
	if cerr := f.Close(); werr != nil || cerr != nil || os.Rename(f.Name(), path) != nil {
		os.Remove(f.Name())
		return false
	}
	return true
}

// ClaimNotice is true for the first caller of tag on this machine: the marker
// is created with O_EXCL, atomic across processes. A directory that cannot be
// written claims nothing, so nothing is announced.
func ClaimNotice(dir, tag string) bool {
	if !tagRe.MatchString(tag) || os.MkdirAll(dir, 0o755) != nil {
		return false
	}
	f, err := os.OpenFile(filepath.Join(dir, "update-notified-"+tag), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

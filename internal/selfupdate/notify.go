package selfupdate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const checkEvery = 24 * time.Hour

type checkFile struct {
	Tag       string    `json:"tag"`
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
// it asks the network only when dir's update-check.json is missing or older
// than 24 hours, within timeout, and a failure leaves the file alone. A
// prerelease is an error: it is not announced.
func LatestCached(base, dir string, now time.Time, timeout time.Duration) (string, error) {
	path := filepath.Join(dir, "update-check.json")
	if b, err := os.ReadFile(path); err == nil {
		var c checkFile
		if json.Unmarshal(b, &c) == nil && c.Tag != "" && now.Sub(c.CheckedAt) < checkEvery && now.After(c.CheckedAt.Add(-checkEvery)) {
			return c.Tag, nil
		}
	}
	c := *client
	c.Timeout = timeout
	tag, err := latestWith(&c, base)
	if err != nil {
		return "", err
	}
	if semverRe.FindStringSubmatch(tag)[4] != "" {
		return "", errors.New("latest release is a prerelease")
	}
	b, _ := json.Marshal(checkFile{Tag: tag, CheckedAt: now})
	if os.MkdirAll(dir, 0o755) == nil { // N-0129: temp file + rename
		if f, err := os.CreateTemp(dir, "update-check-*.tmp"); err == nil {
			_, werr := f.Write(b)
			if cerr := f.Close(); werr != nil || cerr != nil || os.Rename(f.Name(), path) != nil {
				os.Remove(f.Name())
			}
		}
	}
	return tag, nil // the answer is good even when it could not be kept
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

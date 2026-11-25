// Package selfupdate replaces the cerne binary with the latest release, the
// way install.sh and install.ps1 do it (F-0476): resolve the tag from the
// /releases/latest redirect, download the asset and SHA256SUMS, verify, then
// stage next to the executable and rename. Its callers are `cerne update` and
// the cerne ui panel's release check and Update button (F-0480, behind
// ui.update_check): the binary's only version checks (F-0476 crit. 11).
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the repository releases are read from; CERNE_BASE_URL
// overrides it, as in the install scripts (mirrors, tests).
const DefaultBaseURL = "https://github.com/henriquesakata/cerne"

// Method is how the running binary was installed, which decides who may
// replace it.
type Method int

const (
	Script    Method = iota // install script or a release file put in place by hand: cerne replaces it
	Homebrew                // under a Cellar: Homebrew replaces it, so its records stay right
	GoInstall               // in a GOBIN/GOPATH bin: `go install` replaces it
	Dev                     // a local build: nothing to replace it with
	SystemPackage           // /usr/bin/cerne from pacman and kin: the package manager replaces it
)

func (m Method) String() string {
	return [...]string{"script", "homebrew", "go install", "dev", "system package"}[m]
}

var (
	ErrNoRelease = errors.New("no release found")
	ErrChecksum  = errors.New("checksum does not match the release's SHA256SUMS")
)

// releaseTagRe is a release tag as the release workflow stamps it; a
// `git describe` string (commits after a tag, -dirty) is not one.
var (
	releaseTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)
	describeRe   = regexp.MustCompile(`-\d+-g[0-9a-f]+(-dirty)?$|-dirty$`)
)

// Detect names the install method from the executable path as invoked, its
// symlink-resolved form, the build version and the Go bin folders. Any
// version that is not a release tag ("dev", a describe string) is a local
// build, never replaced by a release.
func Detect(exe, resolved, version string, gobins []string) Method {
	if !releaseTagRe.MatchString(version) || describeRe.MatchString(version) {
		return Dev
	}
	if filepath.ToSlash(resolved) == "/usr/bin/cerne" {
		return SystemPackage
	}
	if strings.Contains(filepath.ToSlash(resolved), "/Cellar/cerne/") {
		return Homebrew
	}
	for _, p := range []string{exe, resolved} {
		for _, b := range gobins {
			// Case-insensitive: Windows and macOS folders are, and a false
			// match only keeps the binary, the safe side.
			if b != "" && strings.EqualFold(filepath.Clean(filepath.Dir(p)), filepath.Clean(b)) {
				return GoInstall
			}
		}
	}
	return Script
}

// GoBins lists where `go install` writes or wrote: GOBIN and every GOPATH
// entry's bin (~/go/bin when GOPATH is unset). Read from the environment,
// never by running go.
func GoBins() []string {
	var out []string
	if b := os.Getenv("GOBIN"); b != "" {
		out = append(out, b)
	}
	gopath := filepath.SplitList(os.Getenv("GOPATH"))
	for _, p := range gopath {
		if p != "" {
			out = append(out, filepath.Join(p, "bin"))
		}
	}
	if len(gopath) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			out = append(out, filepath.Join(home, "go", "bin"))
		}
	}
	return out
}

// BrewLink is the <prefix>/bin/cerne link Homebrew points at the installed
// keg, given the resolved path inside the Cellar.
func BrewLink(resolved string) string {
	s := filepath.ToSlash(resolved)
	prefix := s[:strings.Index(s, "/Cellar/")]
	return filepath.Join(filepath.FromSlash(prefix), "bin", "cerne")
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

// Newer reports whether release tag latest is newer than current. An
// unparseable current (a describe string, "dev") counts as older.
// ponytail: prereleases compare as plain strings (rc.10 < rc.9); fine while
// tags stay rc.1..rc.9.
func Newer(latest, current string) bool {
	l, c := semverRe.FindStringSubmatch(latest), semverRe.FindStringSubmatch(current)
	if l == nil {
		return false
	}
	if c == nil {
		return true
	}
	for i := 1; i <= 3; i++ {
		ln, _ := strconv.Atoi(l[i])
		cn, _ := strconv.Atoi(c[i])
		if ln != cn {
			return ln > cn
		}
	}
	switch {
	case l[4] == c[4]:
		return false
	case l[4] == "": // a release is newer than its prereleases
		return true
	case c[4] == "":
		return false
	}
	return l[4] > c[4]
}

var tagRe = regexp.MustCompile(`^v[0-9][A-Za-z0-9._-]*$`)

// client keeps an https origin on https across redirects, like install.sh's
// --proto-redir; an http:// CERNE_BASE_URL is the user's explicit choice.
var client = &http.Client{
	Timeout: 5 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("refusing a redirect from https to %s", req.URL.Scheme)
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// Latest resolves the latest release tag from the /releases/latest redirect,
// which ends at /releases/tag/<tag>: no API call, so no anonymous rate limit.
func Latest(base string) (string, error) {
	resp, err := client.Head(strings.TrimRight(base, "/") + "/releases/latest")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %s/releases/latest answered %s", ErrNoRelease, base, resp.Status)
	}
	tag := pathBase(resp.Request.URL.Path)
	if !tagRe.MatchString(tag) {
		return "", fmt.Errorf("%w at %s/releases/latest", ErrNoRelease, base)
	}
	return tag, nil
}

func pathBase(p string) string { return p[strings.LastIndex(p, "/")+1:] }

// AssetName is the release file for a platform; Windows always gets the amd64
// build, as install.ps1 does (Windows on ARM runs it emulated).
func AssetName(tag, goos, goarch string) string {
	if goos == "windows" {
		return "cerne_" + tag + "_windows_amd64.exe"
	}
	return "cerne_" + tag + "_" + goos + "_" + goarch
}

// Fetch downloads the release asset for the platform and verifies it against
// the release's SHA256SUMS; nothing is written to disk.
func Fetch(base, tag, goos, goarch string) ([]byte, error) {
	name := AssetName(tag, goos, goarch)
	dl := strings.TrimRight(base, "/") + "/releases/download/" + tag + "/"
	data, err := get(dl + name)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	sums, err := get(dl + "SHA256SUMS")
	if err != nil {
		return nil, fmt.Errorf("downloading SHA256SUMS: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && strings.TrimPrefix(f[1], "*") == name {
			want = f[0]
		}
	}
	if want == "" {
		return nil, fmt.Errorf("%w: SHA256SUMS of %s has no entry for %s", ErrChecksum, tag, name)
	}
	h := sha256.Sum256(data)
	if got := hex.EncodeToString(h[:]); !strings.EqualFold(got, want) {
		return nil, fmt.Errorf("%w: %s (expected %s, got %s)", ErrChecksum, name, want, got)
	}
	return data, nil
}

func get(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// writeSynced writes data and flushes it to disk, so a crash right after the
// rename never leaves a truncated cerne.
func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Writable reports whether a file can be created in dir, by creating and
// removing one: checked before any download.
func Writable(dir string) error {
	f, err := os.CreateTemp(dir, ".cerne.update.probe.*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

// Replace puts data at target: staged in the same folder, then renamed, so an
// interrupted update never leaves a truncated or missing cerne. On Windows a
// running executable cannot be overwritten, only renamed, so target moves
// aside first (under a name of its own: an older one may still run) and comes
// back if the swap fails; copies left by earlier updates go now, unless still
// running.
func Replace(target string, data []byte, goos string) error {
	dir := filepath.Dir(target)
	staged := filepath.Join(dir, fmt.Sprintf(".cerne.update.%d", os.Getpid()))
	if goos == "windows" {
		staged += ".exe"
	}
	if err := writeSynced(staged, data); err != nil {
		os.Remove(staged)
		return err
	}
	defer os.Remove(staged) // a no-op after a successful rename
	// The create mode is cut by the umask.
	if err := os.Chmod(staged, 0o755); err != nil {
		return err
	}
	if goos != "windows" {
		return os.Rename(staged, target)
	}
	if olds, _ := filepath.Glob(target + ".*.old"); len(olds) > 0 {
		for _, o := range olds {
			os.Remove(o)
		}
	}
	old := fmt.Sprintf("%s.%d.old", target, os.Getpid())
	if err := os.Rename(target, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		os.Rename(old, target)
		return err
	}
	return nil
}

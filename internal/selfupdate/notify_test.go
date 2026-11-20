package selfupdate

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// releaseServer answers /releases/latest by redirecting to the tag's page.
func releaseServer(t *testing.T, tag *atomic.Value, hits *int32, delay time.Duration) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		time.Sleep(delay)
		http.Redirect(w, r, "/releases/tag/"+tag.Load().(string), http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/", func(w http.ResponseWriter, r *http.Request) {})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// F-0479 crit. 1: one request a day per machine; a slow server costs at most
// the timeout and leaves no cache behind.
func TestLatestCachedAsksOnceADay(t *testing.T) {
	var tag atomic.Value
	tag.Store("v1.2.0")
	var hits int32
	s := releaseServer(t, &tag, &hits, 0)
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 2; i++ {
		got, err := LatestCached(s.URL, dir, now.Add(time.Duration(i)*time.Hour), time.Second)
		if err != nil || got != "v1.2.0" {
			t.Fatalf("call %d = %q, %v", i, got, err)
		}
	}
	if hits != 1 {
		t.Fatalf("hits after two calls within 24h = %d, want 1", hits)
	}
	tag.Store("v1.3.0")
	got, err := LatestCached(s.URL, dir, now.Add(25*time.Hour), time.Second)
	if err != nil || got != "v1.3.0" || hits != 2 {
		t.Fatalf("after 25h = %q, %v, hits %d; want v1.3.0 and 2 hits", got, err, hits)
	}
}

func TestLatestCachedSlowServerFailsWithinTimeout(t *testing.T) {
	var tag atomic.Value
	tag.Store("v1.2.0")
	var hits int32
	s := releaseServer(t, &tag, &hits, 2*time.Second)
	dir := t.TempDir()
	start := time.Now()
	if _, err := LatestCached(s.URL, dir, time.Now(), 200*time.Millisecond); err == nil {
		t.Fatal("want an error from the slow server")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v, want about the 200ms timeout", d)
	}
	// The failure is remembered for a day, so the next session does not ask again.
	if _, err := LatestCached(s.URL, dir, time.Now().Add(time.Hour), 200*time.Millisecond); err == nil {
		t.Fatal("want the remembered failure")
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("requests after two sessions = %d, want 1", n)
	}
}

// F-0479: offline, a 5xx, a prerelease and an unwritable cache each cost one
// request a day, not one per session.
func TestLatestCachedRemembersFailures(t *testing.T) {
	var hits int32
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(fail.Close)
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := LatestCached(fail.URL, dir, now.Add(time.Duration(i)*time.Minute), time.Second); err == nil {
			t.Fatal("want an error")
		}
	}
	if hits != 1 {
		t.Fatalf("failing server: %d requests, want 1", hits)
	}
	if _, err := LatestCached(fail.URL, dir, now.Add(25*time.Hour), time.Second); err == nil || hits != 2 {
		t.Fatalf("after 25h: err %v, hits %d, want a new request", err, hits)
	}

	var tag atomic.Value
	tag.Store("v2.0.0-rc.1")
	var pre int32
	ps := releaseServer(t, &tag, &pre, 0)
	pdir := t.TempDir()
	for i := 0; i < 2; i++ {
		LatestCached(ps.URL, pdir, now, time.Second)
	}
	if pre != 1 {
		t.Fatalf("prerelease: %d requests, want 1", pre)
	}

	// A cache dir that cannot be written: the process still asks once.
	var ro int32
	tag.Store("v1.2.0")
	rs := releaseServer(t, &tag, &ro, 0)
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	bad := filepath.Join(f, "sub")
	for i := 0; i < 2; i++ {
		LatestCached(rs.URL, bad, now, time.Second)
	}
	if ro != 1 {
		t.Fatalf("unwritable cache: %d requests, want 1 in the process", ro)
	}
}

// A cached answer belongs to the base it was asked of.
func TestLatestCachedKeysByBase(t *testing.T) {
	var tag atomic.Value
	tag.Store("v1.2.0")
	var a, b int32
	sa, sb := releaseServer(t, &tag, &a, 0), releaseServer(t, &tag, &b, 0)
	dir := t.TempDir()
	now := time.Now()
	LatestCached(sa.URL, dir, now, time.Second)
	LatestCached(sb.URL, dir, now, time.Second)
	if a != 1 || b != 1 {
		t.Fatalf("requests a=%d b=%d, want 1 each", a, b)
	}
}

// A tag the redirect gives that passes tagRe but is not a version is an error,
// not a panic.
func TestLatestCachedNonSemverTag(t *testing.T) {
	var tag atomic.Value
	tag.Store("v1.2")
	var hits int32
	s := releaseServer(t, &tag, &hits, 0)
	if got, err := LatestCached(s.URL, t.TempDir(), time.Now(), time.Second); err == nil {
		t.Fatalf("got %q, want an error", got)
	}
}

// A prerelease is not a release to announce (spec: out of scope).
func TestLatestCachedIgnoresPrerelease(t *testing.T) {
	var tag atomic.Value
	tag.Store("v2.0.0-rc.1")
	var hits int32
	s := releaseServer(t, &tag, &hits, 0)
	if got, err := LatestCached(s.URL, t.TempDir(), time.Now(), time.Second); err == nil {
		t.Fatalf("prerelease announced as %q", got)
	}
}

func TestClaimNoticeOncePerTag(t *testing.T) {
	dir := t.TempDir()
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ClaimNotice(dir, "v1.2.0") {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("goroutines that won = %d, want 1", wins)
	}
	if !ClaimNotice(dir, "v1.3.0") || ClaimNotice(dir, "v1.3.0") {
		t.Fatal("a newer tag must announce once")
	}
}

func TestClaimNoticeReadOnlyDirFailsOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if ClaimNotice(dir, "v1.2.0") {
		t.Fatal("an unwritable dir must not claim")
	}
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	if ClaimNotice(filepath.Join(f, "sub"), "v1.2.0") {
		t.Fatal("a dir that cannot be made must not claim")
	}
}

// Separate processes race on the same directory: exactly one wins.
func TestClaimNoticeAcrossProcesses(t *testing.T) {
	if d := os.Getenv("CERNE_TEST_CLAIM_DIR"); d != "" {
		if ClaimNotice(d, "v9.9.9") {
			os.Stdout.WriteString("WON")
		}
		return
	}
	dir := t.TempDir()
	exe, _ := os.Executable()
	var wg sync.WaitGroup
	var wins int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := exec.Command(exe, "-test.run=^TestClaimNoticeAcrossProcesses$")
			c.Env = append(os.Environ(), "CERNE_TEST_CLAIM_DIR="+dir)
			out, _ := c.Output()
			if strings.Contains(string(out), "WON") {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("processes that won = %d, want 1", wins)
	}
}

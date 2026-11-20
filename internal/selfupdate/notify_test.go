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
	if _, err := os.Stat(filepath.Join(dir, "update-check.json")); err == nil {
		t.Fatal("a failed check wrote the cache")
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
	if ClaimNotice(filepath.Join(t.TempDir(), "missing", "deeper"), "v1.2.0") {
		// MkdirAll creates it: the claim works.
		return
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

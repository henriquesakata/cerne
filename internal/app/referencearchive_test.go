package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/version"
)

const pageURL = "https://docs.publicsite.org/guide?lang=en"

// fakeWayback is an archive service that records every request it receives.
type fakeWayback struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
	dump []string
}

func newFakeWayback(t *testing.T, h http.HandlerFunc) *fakeWayback {
	t.Helper()
	f := &fakeWayback{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, _ := httputil.DumpRequest(r, true)
		f.mu.Lock()
		f.reqs = append(f.reqs, r)
		f.dump = append(f.dump, string(d))
		f.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// client points the real client at the fake, and dials the fake for any host:
// a followed redirect to web.archive.org never leaves the machine.
func (f *fakeWayback) client() WaybackClient {
	addr := f.srv.Listener.Addr().String()
	return WaybackClient{
		SaveURL: f.srv.URL + "/save/", CDXURL: f.srv.URL + "/cdx/search/cdx",
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}},
	}
}

const copyLinkAt = "https://web.archive.org/web/20261124120304/" + pageURL

// F-0503 crit. 8: Save reads the copy's link and capture date from Location
// (a redirect it does not follow) or from Content-Location.
func TestWaybackSaveReadsTheCopyLink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header string
	}{
		{"location", http.StatusFound, "Location"},
		{"content-location", http.StatusOK, "Content-Location"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/web/") {
					t.Error("the client followed the redirect to the copy")
				}
				w.Header().Set(tc.header, copyLinkAt)
				w.WriteHeader(tc.status)
			})
			s, err := f.client().Save(context.Background(), pageURL)
			if err != nil {
				t.Fatal(err)
			}
			if s.URL != copyLinkAt {
				t.Errorf("link = %q, want %q", s.URL, copyLinkAt)
			}
			if want := time.Date(2026, 11, 24, 12, 3, 4, 0, time.UTC); !s.CapturedAt.Equal(want) {
				t.Errorf("captured = %v, want %v", s.CapturedAt, want)
			}
			if got := f.reqs[0].URL.RequestURI(); got != "/save/"+pageURL {
				t.Errorf("request = %q, want /save/<page url>", got)
			}
		})
	}
}

// F-0503 crit. 10: a refused or linkless Save is an error, so the caller falls
// back on the nearest capture.
func TestWaybackSaveFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		loc    string
	}{
		{"rate limited", http.StatusTooManyRequests, copyLinkAt},
		{"server error", http.StatusServiceUnavailable, ""},
		{"no link", http.StatusOK, ""},
		{"not a copy", http.StatusFound, "/elsewhere"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.loc != "" {
					w.Header().Set("Location", tc.loc)
				}
				w.WriteHeader(tc.status)
			})
			if s, err := f.client().Save(context.Background(), pageURL); err == nil {
				t.Errorf("Save = %+v, want an error", s)
			}
		})
	}
}

// F-0503 crit. 10: Captures asks the CDX API for the page's 200 captures, one
// per day, and reads each row's timestamp into a link and a date; the header
// row is skipped, and a page with no capture is an empty list.
func TestWaybackCaptures(t *testing.T) {
	f := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/cdx/search/cdx" || q.Get("url") != pageURL || q.Get("output") != "json" || q.Get("fl") != "timestamp,original,statuscode" ||
			q.Get("filter") != "statuscode:200" || q.Get("collapse") != "timestamp:8" {
			t.Errorf("request = %s, want the CDX query for the page URL", r.URL)
		}
		_, _ = w.Write([]byte(`[["timestamp","original","statuscode"],
["20191122194934","http://docs.publicsite.org/guide","200"],
["20250302010203","https://docs.publicsite.org/guide","200"]]`))
	})
	got, err := f.client().Captures(context.Background(), pageURL)
	if err != nil {
		t.Fatal(err)
	}
	want := []Snapshot{
		{URL: "https://web.archive.org/web/20191122194934/http://docs.publicsite.org/guide", CapturedAt: time.Date(2019, 11, 22, 19, 49, 34, 0, time.UTC)},
		{URL: "https://web.archive.org/web/20250302010203/https://docs.publicsite.org/guide", CapturedAt: time.Date(2025, 3, 2, 1, 2, 3, 0, time.UTC)},
	}
	if !slices.Equal(got, want) {
		t.Errorf("captures = %+v, want %+v", got, want)
	}
	for _, body := range []string{"", "[]", `[["timestamp","original","statuscode"]]`} {
		none := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		if got, err := none.client().Captures(context.Background(), pageURL); err != nil || len(got) != 0 {
			t.Errorf("body %q: %+v, %v; want an empty list", body, got, err)
		}
	}
	bad := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	if _, err := bad.client().Captures(context.Background(), pageURL); err == nil {
		t.Error("a 429 is an error")
	}
}

// F-0503 crit. 10: a copy link that is not https://web.archive.org/web/<timestamp>/
// counts as no copy from Save.
func TestWaybackRefusesAForeignCopyLink(t *testing.T) {
	for _, loc := range []string{
		"https://web.archive.org.evil.org/web/20261124120304/x", "https://evil.org/web/20261124120304/x",
		"/web/20261124120304/x", "https://web.archive.org/save/x", "https://web.archive.org/web/2026/x",
		"ftp://web.archive.org/web/20261124120304/x", "https://user@web.archive.org/web/20261124120304/x",
	} {
		f := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusFound)
		})
		if s, err := f.client().Save(context.Background(), pageURL); err == nil {
			t.Errorf("Save with Location %q = %+v, want an error", loc, s)
		}
	}
}

// F-0503 crit. 3: each request carries the page URL and a generic User-Agent,
// with no cookie, no Authorization, no Referer and no body.
func TestWaybackRequestsCarryOnlyTheURL(t *testing.T) {
	f := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "1"})
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c := f.client()
	_, _ = c.Save(context.Background(), pageURL)
	_, _ = c.Save(context.Background(), pageURL)
	_, _ = c.Captures(context.Background(), pageURL)
	if len(f.reqs) != 3 {
		t.Fatalf("%d requests, want 3", len(f.reqs))
	}
	for i, r := range f.reqs {
		for _, h := range []string{"Cookie", "Authorization", "Referer"} {
			if v := r.Header.Get(h); v != "" {
				t.Errorf("request %d carries %s: %q", i, h, v)
			}
		}
		if ua := r.Header.Get("User-Agent"); ua != "cerne/"+version.Version {
			t.Errorf("request %d User-Agent = %q", i, ua)
		}
		if r.ContentLength > 0 || r.Method != http.MethodGet {
			t.Errorf("request %d = %s with %d bytes of body", i, r.Method, r.ContentLength)
		}
	}
}

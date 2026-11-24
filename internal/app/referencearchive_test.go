package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
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

func (f *fakeWayback) client() WaybackClient {
	return WaybackClient{SaveURL: f.srv.URL + "/save/", AvailableURL: f.srv.URL + "/wayback/available"}
}

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
				w.Header().Set(tc.header, "/web/20261124120304/"+pageURL)
				w.WriteHeader(tc.status)
			})
			s, err := f.client().Save(context.Background(), pageURL)
			if err != nil {
				t.Fatal(err)
			}
			if want := f.srv.URL + "/web/20261124120304/" + pageURL; s.URL != want {
				t.Errorf("link = %q, want %q", s.URL, want)
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
// back on Closest.
func TestWaybackSaveFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		loc    string
	}{
		{"rate limited", http.StatusTooManyRequests, "/web/20261124120304/" + pageURL},
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

// F-0503 crit. 10: Closest reads the availability API's closest copy; no copy
// is ErrNoSnapshot.
func TestWaybackClosest(t *testing.T) {
	f := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wayback/available" || r.URL.Query().Get("url") != pageURL {
			t.Errorf("request = %s, want /wayback/available?url=<page url>", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"x","archived_snapshots":{"closest":{"status":"200","available":true,"url":"http://web.archive.org/web/20250302010203/https://docs.publicsite.org/guide","timestamp":"20250302010203"}}}`))
	})
	s, err := f.client().Closest(context.Background(), pageURL)
	if err != nil {
		t.Fatal(err)
	}
	if s.URL != "http://web.archive.org/web/20250302010203/https://docs.publicsite.org/guide" || !s.CapturedAt.Equal(time.Date(2025, 3, 2, 1, 2, 3, 0, time.UTC)) {
		t.Errorf("closest = %+v", s)
	}

	none := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"x","archived_snapshots":{}}`))
	})
	if _, err := none.client().Closest(context.Background(), pageURL); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("no copy: err = %v, want ErrNoSnapshot", err)
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
	_, _ = c.Closest(context.Background(), pageURL)
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

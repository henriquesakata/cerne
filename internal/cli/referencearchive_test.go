package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

type archiveFake struct {
	projDir, repoDir string
	wayback          *httptest.Server
	waybackHits      atomic.Int32
	pageHits         atomic.Int32
}

type pageTransport func(*http.Request) (*http.Response, error)

func (f pageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// archiveFixture is a project with a fake Wayback Machine and pages answered
// in process (no socket, no DNS: the resolver says every name is public).
// archiveNet points at them for the test. The fake saves any page with
// saveStatus (302 = a new copy); a page path ending in /old is refused (429)
// and has an existing copy from 2025-03-02. Page /private answers 404, any
// other 200. archive turns references.archive on in .cernerc.
func archiveFixture(t *testing.T, archive bool, saveStatus int) *archiveFake {
	t.Helper()
	f := &archiveFake{}
	f.wayback = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.waybackHits.Add(1)
		switch {
		case strings.HasPrefix(r.URL.Path, "/save/"):
			target := strings.TrimPrefix(r.URL.RequestURI(), "/save/")
			switch {
			case strings.HasSuffix(target, "/old"):
				w.WriteHeader(http.StatusTooManyRequests)
			case saveStatus == http.StatusFound:
				w.Header().Set("Location", "https://web.archive.org/web/20261124120000/"+target)
				w.WriteHeader(http.StatusFound)
			default:
				w.WriteHeader(saveStatus)
			}
		case r.URL.Path == "/wayback/available":
			if u := r.URL.Query().Get("url"); strings.HasSuffix(u, "/old") {
				_, _ = w.Write([]byte(`{"archived_snapshots":{"closest":{"available":true,"url":"https://web.archive.org/web/20250302080000/` + u + `","timestamp":"20250302080000"}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"archived_snapshots":{}}`))
		default:
			t.Errorf("unexpected archive request %s", r.URL)
		}
	}))
	t.Cleanup(f.wayback.Close)
	old := archiveNet
	archiveNet = func() app.ArchiveNet {
		return app.ArchiveNet{
			// Every dial goes to the fake: a followed redirect to web.archive.org
			// never leaves the machine.
			Client: app.WaybackClient{SaveURL: f.wayback.URL + "/save/", AvailableURL: f.wayback.URL + "/wayback/available",
				Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, f.wayback.Listener.Addr().String())
				}}},
			LookupIP: func(context.Context, string, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("93.184.215.14")}, nil
			},
			Transport: pageTransport(func(r *http.Request) (*http.Response, error) {
				f.pageHits.Add(1)
				rec := httptest.NewRecorder()
				if r.URL.Path == "/private" {
					rec.WriteHeader(http.StatusNotFound)
				}
				res := rec.Result()
				res.Request = r
				return res, nil
			}),
		}
	}
	t.Cleanup(func() { archiveNet = old })
	f.projDir, _, f.repoDir = setupProject(t)
	if archive {
		f.turnOn(t)
	}
	return f
}

func (f *archiveFake) turnOn(t *testing.T) {
	t.Helper()
	rc := filepath.Join(f.projDir, ".cernerc")
	b, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, append(b, "\n[references]\narchive = true\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *archiveFake) cerne(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execCerne(t, append(args, "--dir", f.projDir)...)
	if err != nil {
		t.Fatalf("cerne %v: %v\n%s", args, err, out)
	}
	return out
}

func (f *archiveFake) ref(t *testing.T, id string) store.Reference {
	t.Helper()
	r, err := store.LoadReference(f.repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// F-0503 crit. 8: with the setting on, reference new archives the page and
// records the copy's link, date and origin.
func TestReferenceNewArchivesThePage(t *testing.T) {
	f := archiveFixture(t, true, http.StatusFound)
	out := f.cerne(t, "reference", "new", "Guide", "--url", "https://docs.publicsite.org/guide")
	link := "https://web.archive.org/web/20261124120000/https://docs.publicsite.org/guide"
	for _, want := range []string{"R-0001 registered (external: https://docs.publicsite.org/guide).\n", "archive: " + link + " (saved 2026-11-24)\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if a := f.ref(t, "R-0001").Archive; a == nil || a.Status != "archived" || a.Origin != "saved" || a.URL != link || a.CapturedAt.IsZero() {
		t.Fatalf("archive = %+v", a)
	}
}

// F-0503 crit. 8: a failing archive service does not stop the registration:
// the reference exists, with its archive pending.
func TestReferenceNewArchivePending(t *testing.T) {
	f := archiveFixture(t, true, http.StatusServiceUnavailable)
	out := f.cerne(t, "reference", "new", "Guide", "--url", "https://docs.publicsite.org/guide")
	if !strings.Contains(out, "R-0001 registered") || !strings.Contains(out, "archive: pending (save failed: 503)\n") {
		t.Errorf("output:\n%s", out)
	}
	if r := f.ref(t, "R-0001"); r.State != "current" || r.Archive == nil || r.Archive.Status != "pending" {
		t.Fatalf("reference = %+v", r)
	}
}

// F-0503 crit. 7: a reference registered with --no-archive is never sent,
// not by reference new, a later batch or a named run.
func TestReferenceNewNoArchive(t *testing.T) {
	f := archiveFixture(t, true, http.StatusFound)
	out := f.cerne(t, "reference", "new", "Guide", "--url", "https://docs.publicsite.org/guide", "--no-archive")
	if strings.Contains(out, "archive:") {
		t.Errorf("a no-archive reference printed an archive line:\n%s", out)
	}
	if r := f.ref(t, "R-0001"); !r.NoArchive || r.Archive != nil {
		t.Fatalf("reference = %+v, want no-archive and no archive", r)
	}
	if out := f.cerne(t, "reference", "archive"); !strings.Contains(out, "nothing to archive") {
		t.Errorf("batch:\n%s", out)
	}
	if out := f.cerne(t, "reference", "archive", "R-0001"); !strings.Contains(out, "R-0001  skipped (no-archive)\n") {
		t.Errorf("named:\n%s", out)
	}
	if f.waybackHits.Load()+f.pageHits.Load() != 0 {
		t.Errorf("%d archive and %d page requests, want none", f.waybackHits.Load(), f.pageHits.Load())
	}
	if !f.ref(t, "R-0001").NoArchive {
		t.Error("the no-archive mark was lost")
	}
}

// F-0503 crit. 2: with the setting off, reference new sends nothing and
// reference archive refuses, dry run included.
func TestReferenceArchiveOffSendsNothing(t *testing.T) {
	f := archiveFixture(t, false, http.StatusFound)
	if out := f.cerne(t, "reference", "new", "Guide", "--url", "https://docs.publicsite.org/guide"); strings.Contains(out, "archive") {
		t.Errorf("reference new spoke of the archive with the setting off:\n%s", out)
	}
	for _, args := range [][]string{{"reference", "archive"}, {"reference", "archive", "--dry-run"}, {"reference", "archive", "R-0001"}} {
		out, err := execCerne(t, append(args, "--dir", f.projDir)...)
		if !errors.Is(err, app.ErrArchiveOff) || !strings.Contains(out, "Settings screen") {
			t.Errorf("%v: err = %v, want ErrArchiveOff naming the Settings screen\n%s", args, err, out)
		}
	}
	if f.waybackHits.Load()+f.pageHits.Load() != 0 || f.ref(t, "R-0001").Archive != nil {
		t.Errorf("%d archive and %d page requests, archive %+v; want none", f.waybackHits.Load(), f.pageHits.Load(), f.ref(t, "R-0001").Archive)
	}
}

// F-0503 crit. 9: reference archive takes the references with no copy,
// sorted by id, one line each and a summary; a second run finds nothing.
func TestReferenceArchiveBatchOutput(t *testing.T) {
	f := archiveFixture(t, false, http.StatusFound)
	for _, u := range []string{
		"https://docs.publicsite.org/guide", "https://docs.publicsite.org/private", "http://10.0.0.5/wiki",
		"https://docs.publicsite.org/p?token=abc", "https://docs.publicsite.org/old",
	} {
		f.cerne(t, "reference", "new", "Src "+u, "--url", u)
	}
	f.cerne(t, "reference", "new", "Kept home", "--url", "https://docs.publicsite.org/home", "--no-archive")
	f.turnOn(t)
	out := f.cerne(t, "reference", "archive")
	want := "R-0001  archived  https://web.archive.org/web/20261124120000/https://docs.publicsite.org/guide  (saved 2026-11-24)\n" +
		"R-0002  not public (HTTP 404)\n" +
		"R-0003  skipped (internal host)\n" +
		"R-0004  skipped (secret in query)\n" +
		"R-0005  existing  https://web.archive.org/web/20250302080000/https://docs.publicsite.org/old  (captured 2025-03-02)\n" +
		"processed 5: 1 archived, 1 existing, 0 pending, 1 not public, 2 skipped\n"
	if !strings.Contains(out, want) {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "R-0006") {
		t.Errorf("the no-archive reference was taken:\n%s", out)
	}
	if out := f.cerne(t, "reference", "archive"); !strings.Contains(out, "nothing to archive") {
		t.Errorf("second run:\n%s", out)
	}
}

// F-0503 crit. 9: --dry-run lists what would be sent and what not, with each
// source URL, and sends and records nothing.
func TestReferenceArchiveDryRun(t *testing.T) {
	f := archiveFixture(t, false, http.StatusFound)
	f.cerne(t, "reference", "new", "Guide", "--url", "https://docs.publicsite.org/guide")
	f.cerne(t, "reference", "new", "Private", "--url", "https://docs.publicsite.org/private")
	f.turnOn(t)
	out := f.cerne(t, "reference", "archive", "--dry-run")
	want := "R-0001  would send  https://docs.publicsite.org/guide\n" +
		"R-0002  not public (HTTP 404)  https://docs.publicsite.org/private\n" +
		"dry run, nothing sent: 1 would send, 0 pending, 1 not public, 0 skipped\n"
	if !strings.Contains(out, want) {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
	if f.waybackHits.Load() != 0 || f.ref(t, "R-0001").Archive != nil || f.ref(t, "R-0002").Archive != nil {
		t.Errorf("a dry run reached the archive (%d) or recorded something", f.waybackHits.Load())
	}
}

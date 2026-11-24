package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// scriptClient is an archive service whose answers a test sets; it records the
// URLs it was asked for.
type scriptClient struct {
	mu      sync.Mutex
	asked   []string
	save    func(string) (Snapshot, error)
	closest func(string) (Snapshot, error)
}

func (c *scriptClient) Save(_ context.Context, u string) (Snapshot, error) {
	c.mu.Lock()
	c.asked = append(c.asked, "save "+u)
	c.mu.Unlock()
	return c.save(u)
}

func (c *scriptClient) Closest(_ context.Context, u string) (Snapshot, error) {
	c.mu.Lock()
	c.asked = append(c.asked, "closest "+u)
	c.mu.Unlock()
	return c.closest(u)
}

var (
	savedAt   = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	existedAt = time.Date(2025, 3, 2, 8, 0, 0, 0, time.UTC)
)

func saves(u string) (Snapshot, error) {
	return Snapshot{URL: "https://web.archive.org/web/20261124120000/" + u, CapturedAt: savedAt}, nil
}

func refuses(string) (Snapshot, error) { return Snapshot{}, httpStatusError(http.StatusTooManyRequests) }

func noCopy(string) (Snapshot, error) { return Snapshot{}, ErrNoSnapshot }

// publicNet passes the gate for any URL: public DNS answer, 200 page.
func publicNet(c ArchiveClient) ArchiveNet {
	return ArchiveNet{Client: c, LookupIP: publicLookup, Transport: handlerTransport(func(w http.ResponseWriter, _ *http.Request) {})}
}

// archiveBackend is refsBackend (R-0001..n at https://example.com/<i>) with a
// .cernerc whose references.archive is on or off.
func archiveBackend(t *testing.T, n int, on bool) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir = refsBackend(t, n)
	rc := "project_id = \"p\"\nbackend = \"b\"\n"
	if on {
		rc += "[references]\narchive = true\n"
	}
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	return repoDir, projDir
}

// setRef rewrites a reference in the backend.
func setRef(t *testing.T, repoDir, projDir, id string, edit func(*store.Reference)) {
	t.Helper()
	if err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		r, err := store.LoadReference(repoDir, id)
		if err != nil {
			return "", err
		}
		edit(&r)
		return "seed " + id, store.SaveReference(repoDir, r)
	}); err != nil {
		t.Fatal(err)
	}
}

// F-0503 crit. 2: with the setting off an archive run refuses before any
// network use and records nothing.
func TestArchiveReferencesOffRefuses(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 1, false)
	before := commitCount(t, repoDir)
	for _, dry := range []bool{false, true} {
		if _, err := ArchiveReferences(repoDir, projDir, nil, offlineNet(t), dry); !errors.Is(err, ErrArchiveOff) {
			t.Errorf("dry=%v: err = %v, want ErrArchiveOff", dry, err)
		}
	}
	if commitCount(t, repoDir) != before {
		t.Error("an off run committed")
	}
}

// F-0503 crit. 8: a saved copy is recorded with its link, capture date and
// origin saved, in one commit with a reference.archived event.
func TestArchiveReferencesRecordsTheCopy(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 1, true)
	c := &scriptClient{save: saves, closest: noCopy}
	before := commitCount(t, repoDir)
	res, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false)
	if err != nil {
		t.Fatal(err)
	}
	want := store.ReferenceArchive{URL: "https://web.archive.org/web/20261124120000/https://example.com/1", CapturedAt: savedAt, Origin: "saved", Status: "archived"}
	if len(res) != 1 || res[0].ID != "R-0001" || res[0].ReferenceArchive != want {
		t.Fatalf("res = %+v, want R-0001 %+v", res, want)
	}
	if got := loadRef(t, repoDir, "R-0001").Archive; got == nil || *got != want {
		t.Fatalf("stored %+v, want %+v", got, want)
	}
	if n := commitCount(t, repoDir); n != before+1 {
		t.Errorf("%d commits, want 1", n-before)
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "reference.archived" || last.Payload["status"] != "archived" || last.Payload["origin"] != "saved" || last.Payload["link"] != want.URL {
		t.Errorf("event = %+v", last)
	}
	for k := range last.Payload {
		if !slices.Contains([]string{"status", "origin", "link", "reason"}, k) {
			t.Errorf("event carries %q", k)
		}
	}
}

// F-0503 crit. 10: a refused Save falls back on Closest and records the
// existing copy.
func TestArchiveReferencesFallsBackOnClosest(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 1, true)
	c := &scriptClient{save: refuses, closest: func(u string) (Snapshot, error) {
		return Snapshot{URL: "https://web.archive.org/web/20250302080000/" + u, CapturedAt: existedAt}, nil
	}}
	if _, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false); err != nil {
		t.Fatal(err)
	}
	got := loadRef(t, repoDir, "R-0001").Archive
	if got == nil || got.Status != "archived" || got.Origin != "existing" || !got.CapturedAt.Equal(existedAt) || !strings.Contains(got.URL, "/web/20250302080000/") {
		t.Fatalf("stored %+v, want the existing copy", got)
	}
	if want := []string{"save https://example.com/1", "closest https://example.com/1"}; !slices.Equal(c.asked, want) {
		t.Errorf("asked %v, want %v", c.asked, want)
	}
}

// F-0503 crit. 8: when Save and Closest both fail the reference stays, pending.
func TestArchiveReferencesPendingWhenTheServiceFails(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 1, true)
	c := &scriptClient{save: refuses, closest: noCopy}
	res, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false)
	if err != nil {
		t.Fatal(err)
	}
	got := loadRef(t, repoDir, "R-0001")
	if got.Archive == nil || got.Archive.Status != "pending" || got.Archive.Reason != "save failed: 429" || got.State != "current" {
		t.Fatalf("stored %+v, want a current reference with a pending archive (save failed: 429)", got)
	}
	if len(res) != 1 || res[0].Status != "pending" {
		t.Errorf("res = %+v", res)
	}
}

// F-0503 crit. 9: a run with no ids takes only the references with no copy or
// a pending one; not public, skipped, archived and no-archive ones stay out.
func TestArchiveReferencesBatchTakesUncopiedAndPending(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 6, true)
	for id, a := range map[string]*store.ReferenceArchive{
		"R-0002": {Status: "pending", Reason: "save failed: 429"},
		"R-0003": {Status: "not_public", Reason: "HTTP 404"},
		"R-0004": {Status: "archived", Origin: "saved", URL: "https://web.archive.org/web/20250101000000/https://example.com/4", CapturedAt: existedAt},
		"R-0005": {Status: "skipped", Reason: "internal host"},
	} {
		setRef(t, repoDir, projDir, id, func(r *store.Reference) { r.Archive = a })
	}
	setRef(t, repoDir, projDir, "R-0006", func(r *store.Reference) { r.NoArchive = true })
	c := &scriptClient{save: saves, closest: noCopy}
	res, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range res {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []string{"R-0001", "R-0002"}) {
		t.Fatalf("processed %v, want [R-0001 R-0002]", ids)
	}
	if want := []string{"save https://example.com/1", "save https://example.com/2"}; !slices.Equal(c.asked, want) {
		t.Errorf("asked %v, want %v", c.asked, want)
	}
	if a := loadRef(t, repoDir, "R-0003").Archive; a.Status != "not_public" {
		t.Errorf("R-0003 = %+v, want untouched", a)
	}
}

// F-0503 crit. 7: a no-archive reference is never sent, by a later batch or
// when named, and keeps its mark.
func TestArchiveReferencesNoArchiveSurvives(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 1, true)
	setRef(t, repoDir, projDir, "R-0001", func(r *store.Reference) { r.NoArchive = true })
	for _, ids := range [][]string{nil, {"R-0001"}} {
		res, err := ArchiveReferences(repoDir, projDir, ids, offlineNet(t), false)
		if err != nil {
			t.Fatal(err)
		}
		if ids != nil && (len(res) != 1 || res[0].Status != "skipped" || res[0].Reason != "no-archive") {
			t.Errorf("named: res = %+v, want skipped (no-archive)", res)
		}
	}
	if r := loadRef(t, repoDir, "R-0001"); !r.NoArchive {
		t.Error("the no-archive mark was lost")
	}
}

// F-0503 crit. 9: a dry run passes the gate but never calls the archive
// service and records nothing.
func TestArchiveReferencesDryRun(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 2, true)
	setRef(t, repoDir, projDir, "R-0002", func(r *store.Reference) { r.URL = "http://10.0.0.4/wiki" })
	before := commitCount(t, repoDir)
	n := publicNet(failClient{t})
	res, err := ArchiveReferences(repoDir, projDir, nil, n, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Status != "would_send" || res[0].URL != "https://example.com/1" || res[1].Status != "skipped" || res[1].Reason != "internal host" {
		t.Fatalf("res = %+v", res)
	}
	if commitCount(t, repoDir) != before || loadRef(t, repoDir, "R-0001").Archive != nil {
		t.Error("a dry run recorded something")
	}
}

// F-0503 crit. 3: through the real Wayback client, the archive service gets
// the reference URL and nothing else of the project: no title, summary, ids,
// informs, project name, cookie, Authorization or Referer.
func TestArchiveReferencesSendOnlyTheURL(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Uses sources", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	const title, summary = "Zephyr confidential roadmap", "classified summary of the vendor deal"
	if _, err := CreateExternalReference(repoDir, projDir, title, pageURL, summary, []string{"F-0001"}, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("project_id = \"p\"\nbackend = \"b\"\n[references]\narchive = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFakeWayback(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	if _, err := ArchiveReferences(repoDir, projDir, nil, publicNet(f.client()), false); err != nil {
		t.Fatal(err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("%d requests to the archive, want 2 (save, then closest)", len(f.reqs))
	}
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range f.dump {
		if !strings.Contains(d, pageURL) && !strings.Contains(d, "url=https%3A%2F%2Fdocs.publicsite.org%2Fguide%3Flang%3Den") {
			t.Errorf("request %d lacks the page URL:\n%s", i, d)
		}
		for _, leak := range []string{title, "Zephyr", summary, "classified", "R-0001", "F-0001", proj.Name, "Cookie", "Authorization", "Referer"} {
			if strings.Contains(d, leak) {
				t.Errorf("request %d carries %q:\n%s", i, leak, d)
			}
		}
	}
}

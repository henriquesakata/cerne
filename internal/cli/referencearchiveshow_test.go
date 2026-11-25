package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

const copyLink = "https://web.archive.org/web/20261124120000/https://docs.publicsite.org/guide"

// seedArchive records an archive outcome (and the no-archive mark) on a
// reference, as a run would.
func seedArchive(t *testing.T, projDir, repoDir, id string, a *store.ReferenceArchive, noArchive bool) {
	t.Helper()
	if err := app.MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		r, err := store.LoadReference(repoDir, id)
		if err != nil {
			return "", err
		}
		r.Archive, r.NoArchive = a, noArchive
		return "seed " + id, store.SaveReference(repoDir, r)
	}); err != nil {
		t.Fatal(err)
	}
}

var savedCopy = &store.ReferenceArchive{URL: copyLink, CapturedAt: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC), Origin: "saved", Status: "archived"}

// F-0503 crit. 11: show prints the copy's link and date, or why there is none.
func TestShowReferenceArchive(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for _, u := range []string{"https://docs.publicsite.org/guide", "https://docs.publicsite.org/b", "https://docs.publicsite.org/c", "https://docs.publicsite.org/d"} {
		if out, err := execCerne(t, "reference", "new", "Src", "--url", u, "--force", "--dir", projDir); err != nil {
			t.Fatalf("reference new: %v\n%s", err, out)
		}
	}
	seedArchive(t, projDir, repoDir, "R-0001", savedCopy, false)
	seedArchive(t, projDir, repoDir, "R-0002", &store.ReferenceArchive{Status: "pending", Reason: "save failed: 429"}, false)
	seedArchive(t, projDir, repoDir, "R-0003", &store.ReferenceArchive{Status: "not_public", Reason: "HTTP 404"}, false)
	seedArchive(t, projDir, repoDir, "R-0004", nil, true)
	for id, want := range map[string]string{
		"R-0001": "archive: " + copyLink + " (saved 2026-11-24)\n",
		"R-0002": "archive: pending (save failed: 429)\n",
		"R-0003": "archive: not public (HTTP 404)\n",
		"R-0004": "archive: skipped (no-archive)\n",
	} {
		out, err := execCerne(t, "show", id, "--dir", projDir)
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("show %s: %v, want %q in:\n%s", id, err, want, out)
		}
	}
}

// F-0503 crit. 11: reference list carries the copy's link and date on the
// reference's line.
func TestReferenceListShowsTheCopy(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "reference", "new", "Guide", "--url", "https://docs.publicsite.org/guide", "--dir", projDir); err != nil {
		t.Fatalf("reference new: %v\n%s", err, out)
	}
	seedArchive(t, projDir, repoDir, "R-0001", savedCopy, false)
	out, err := execCerne(t, "reference", "list", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "R-0001") {
			if !strings.Contains(l, "archive "+copyLink+" 2026-11-24") {
				t.Errorf("line lacks the copy and its date: %s", l)
			}
			return
		}
	}
	t.Fatalf("no R-0001 line:\n%s", out)
}

// F-0503 crit. 11: the check line of a gone or changed reference ends with the
// copy's link, its origin and its date.
func TestReferenceCheckShowsTheCopy(t *testing.T) {
	projDir, repoDir, flip := checkFixture(t)
	seedArchive(t, projDir, repoDir, "R-0003", savedCopy, false)
	seedArchive(t, projDir, repoDir, "R-0002", savedCopy, false)
	seedArchive(t, projDir, repoDir, "R-0001", savedCopy, false)
	out, err := execCerne(t, "reference", "check", "--dir", projDir)
	if err != nil {
		t.Fatalf("check: %v\n%s", err, out)
	}
	if !strings.Contains(out, "R-0003  gone (HTTP 404)  informs F-0001  archive: "+copyLink+" (saved 2026-11-24)\n") {
		t.Errorf("gone line lacks the copy:\n%s", out)
	}
	if !strings.Contains(out, "R-0001  ok\n") {
		t.Errorf("an ok line carries more than its result:\n%s", out)
	}
	flip.Store(true)
	out, err = execCerne(t, "reference", "check", "R-0002", "--dir", projDir)
	if err != nil {
		t.Fatalf("check R-0002: %v\n%s", err, out)
	}
	if !strings.Contains(out, "R-0002  changed  informs F-0001  archive: "+copyLink+" (saved 2026-11-24)\n") {
		t.Errorf("changed line lacks the copy:\n%s", out)
	}
}

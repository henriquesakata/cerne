package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// F-0449 (E-0002 invariant 4): a transition made from the UI says that its
// tracker round failed, as the CLI does on stderr.
func TestTransitionReturnsMirrorWarnings(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	repoDir, projDir, origin := newGitBackend(t)
	trackertest.Register(t, &trackertest.Fake{Name: "pub", Err: errors.New("tracker down")})
	rc := "project_id = \"p\"\nbackend = \"" + origin + "\"\n\n[mirror]\nenabled = true\ntrackers = [{name = \"pub\", kind = \"fake\", url = \"https://fake.test/o/pub\"}]\n"
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	f.Mirror = &store.MirrorRef{Tracker: "pub", Kind: "fake", ID: "1", URL: "https://fake.test/pub/issues/1"}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	var got struct {
		To       string   `json:"to"`
		Warnings []string `json:"warnings"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition", `{"action":"reject","reason":"not now"}`, &got)
	if rec.Code != http.StatusOK || got.To != "rejected" {
		t.Fatalf("status = %d, body %s; want 200 and rejected", rec.Code, rec.Body.String())
	}
	want := "cerne: mirror F-0001 -> pub: tracker down; run 'cerne mirror sync'"
	if len(got.Warnings) != 1 || got.Warnings[0] != want {
		t.Fatalf("warnings = %q, want [%q]", got.Warnings, want)
	}
}

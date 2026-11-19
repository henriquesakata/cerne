package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// TestFeatureAcceptPrintsMirrorWarning (F-0449 crit. 5 on the accept path,
// which prints its own outcome): a failed tracker round reaches stderr as the
// exact line, and the card still lands on done.
func TestFeatureAcceptPrintsMirrorWarning(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	trackertest.Register(t, &trackertest.Fake{Name: "fk", Err: errors.New("tracker down")})
	for key, v := range map[string]any{
		"mirror.enabled":  true,
		"mirror.trackers": []any{map[string]any{"name": "fk", "kind": "fake", "url": "https://fake.test/o/r"}},
	} {
		if err := project.SetSettingValue(projDir, key, project.ScopeProject, v); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	id := newFeature(t, projDir, "Feature A", 1)
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	f.State = domain.StateImplemented
	f.Mirror = &store.MirrorRef{Tracker: "fk", Kind: "fake", ID: "1", URL: "https://fake.test/fk/issues/1"}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	_, stderr, err := execCerneSplit(t, "feature", "accept", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("accept: %v\nstderr: %s", err, stderr)
	}
	if want := "cerne: mirror " + id + " -> fk: tracker down; run 'cerne mirror sync'\n"; !strings.Contains(stderr, want) {
		t.Fatalf("stderr = %q, want the line %q", stderr, want)
	}
	if got, _ := store.LoadFeature(repoDir, id); got.State != domain.StateDone {
		t.Fatalf("state = %s, want done", got.State)
	}
}

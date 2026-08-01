package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// F-0200 makes SafePull skip its fetch under a fresh sync mark, so every read
// command serves the cached clone for up to a minute. That tradeoff is only
// honest while the commands that exist to FETCH still do — they are the escape
// hatch a user reaches for when they want the remote now. These tests pin that:
// each one plants the freshest possible mark (a real fetch, in this very
// process), advances origin behind the command's back, and demands the command
// see it anyway.

// advanceOriginDisjoint pushes a plan.md for F-0001 from a throwaway clone,
// leaving the local spec.md untouched — a remote advance on a different
// document, so nothing here is testing conflict handling.
func advanceOriginDisjoint(t *testing.T, origin, body string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "mate")
	if err := gitback.Clone(origin, clone); err != nil {
		t.Fatalf("Clone mate: %v", err)
	}
	doc := realDoc("F-0001", body)
	mustWrite(t, filepath.Join(clone, "content", "features", "F-0001", "plan.md"), doc)
	if _, err := gitback.CommitAndPush(clone, "remote plan"); err != nil {
		t.Fatalf("CommitAndPush mate: %v", err)
	}
	return doc
}

// TestPullFetchesDespiteFreshSyncMark pins criterion 7 for pull: it resolves
// through resolvePaths + an explicit Fetch, never resolveProject's SafePull, so
// the elision window can never reach it.
func TestPullFetchesDespiteFreshSyncMark(t *testing.T) {
	projDir, origin, repoDir, _ := pulledFeatureProject(t, "Conteúdo original.")
	if err := gitback.Fetch(repoDir); err != nil { // freshest possible mark
		t.Fatalf("Fetch: %v", err)
	}
	theirPlan := advanceOriginDisjoint(t, origin, "Plano remoto.")

	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	planPath := filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento", "plan.md")
	if got, err := os.ReadFile(planPath); err != nil || string(got) != theirPlan {
		t.Fatalf("pull served the cached clone instead of fetching: %q err=%v", got, err)
	}
}

// TestPushFetchesDespiteFreshSyncMark pins criterion 7 for push: it rebases per
// document over advances on OTHER documents, which it can only do having
// fetched them.
func TestPushFetchesDespiteFreshSyncMark(t *testing.T) {
	projDir, origin, repoDir, specPath := pulledFeatureProject(t, "Conteúdo original.")
	if err := gitback.Fetch(repoDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	advanceOriginDisjoint(t, origin, "Plano remoto.")

	mustWrite(t, specPath, realDoc("F-0001", "Minha edição local."))
	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push over a disjoint remote advance: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sent: 1 document(s)") {
		t.Fatalf("push did not send the edit:\n%s", out)
	}
}

// TestSyncFetchesDespiteFreshSyncMark pins criterion 7 for sync (pull + push in
// one forward path): the remote advance lands locally and the local edit lands
// on origin, both of which require a real fetch.
func TestSyncFetchesDespiteFreshSyncMark(t *testing.T) {
	projDir, origin, repoDir, specPath := pulledFeatureProject(t, "Conteúdo original.")
	if err := gitback.Fetch(repoDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	theirPlan := advanceOriginDisjoint(t, origin, "Plano remoto.")

	mustWrite(t, specPath, realDoc("F-0001", "Minha edição local."))
	if out, err := execCerne(t, "sync", "--dir", projDir); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	planPath := filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento", "plan.md")
	if got, err := os.ReadFile(planPath); err != nil || string(got) != theirPlan {
		t.Fatalf("sync did not bring the remote advance: %q err=%v", got, err)
	}
}

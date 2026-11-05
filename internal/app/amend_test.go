package app

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// amendedBody is a result that passes the result checks of a spec with no
// numbered criteria (seedFeatureInState's): a complete Gauntlet record.
const amendedBody = "# Resultado\n\nEmendado antes do aceite.\n" + testGauntletTail

// TestAmendReplacesResultWithEvent (F-0305 crit. 8): the result is replaced
// under a single cerne_id frontmatter — stamped when the body has none, kept
// when it already names the feature — with a feature.doc_edited event carrying
// doc, state, reason and actor, in one commit, the state untouched.
func TestAmendReplacesResultWithEvent(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented, "spec.md", "result.md")
	head0, _ := gitback.HeadSHA(origin)

	f, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody, "critério 2 sem prova")
	if err != nil {
		t.Fatalf("AmendFeatureResult: %v", err)
	}
	if f.State != domain.StateImplemented {
		t.Fatalf("state = %s, want implemented", f.State)
	}
	got, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "result.md"))
	if string(got) != "---\ncerne_id: F-0001\n---\n\n"+strings.TrimSpace(amendedBody)+"\n" {
		t.Fatalf("result.md = %q, want the body behind cerne's stamp", got)
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "feature.doc_edited" || last.EntityID != "F-0001" || last.Payload["doc"] != "result" ||
		last.Payload["state"] != string(domain.StateImplemented) || last.Payload["reason"] != "critério 2 sem prova" ||
		last.Actor != EventActor() {
		t.Fatalf("last event = %+v, want feature.doc_edited{doc:result,state:implemented,reason} by %s", last, EventActor())
	}
	if log := gitOut(t, origin, "log", "--format=%s", head0+"..HEAD"); log != "cerne: amend F-0001/result.md" {
		t.Fatalf("origin commits since seed = %q, want the single amend commit", log)
	}
	if loaded, _ := store.LoadFeature(repoDir, "F-0001"); loaded.State != domain.StateImplemented {
		t.Fatalf("persisted state = %s, want implemented", loaded.State)
	}

	// What `cerne show --doc result` prints comes back with its frontmatter: kept, not doubled.
	shown := "---\ncerne_id: F-0001\nimported_from: docs/x/\n---\n\n" + amendedBody + "\nSegunda emenda.\n"
	if _, err := AmendFeatureResult(repoDir, projDir, "F-0001", shown, "segunda emenda"); err != nil {
		t.Fatalf("amend with frontmatter: %v", err)
	}
	got, _ = os.ReadFile(FeatureDocPath(repoDir, "F-0001", "result.md"))
	if strings.Count(string(got), "cerne_id:") != 1 || !strings.Contains(string(got), "imported_from: docs/x/") {
		t.Fatalf("result.md = %q, want the body's own frontmatter, once", got)
	}

	// Another feature's document is refused, not restamped.
	if _, err := AmendFeatureResult(repoDir, projDir, "F-0001", strings.Replace(shown, "F-0001", "F-0002", 1), "arquivo errado"); err == nil {
		t.Fatal("amend accepted a body whose frontmatter names F-0002")
	}
}

// TestAmendRefusesResultInDebt (F-0305 crit. 10): a new result that fails
// lint.CheckResult is refused with ErrResultIncomplete and the previous result
// stays, with no event and no commit.
func TestAmendRefusesResultInDebt(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented, "spec.md", "result.md")
	path := FeatureDocPath(repoDir, "F-0001", "result.md")
	before, _ := os.ReadFile(path)
	evsBefore, _ := store.ReadEvents(repoDir)
	head0, _ := gitback.HeadSHA(origin)

	_, err := AmendFeatureResult(repoDir, projDir, "F-0001", "# Resultado\n\nSem registro do Gauntlet.\n", "trocar o result")
	if !errors.Is(err, ErrResultIncomplete) {
		t.Fatalf("err = %v, want ErrResultIncomplete", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatalf("result.md changed:\n%s", after)
	}
	if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evsBefore) {
		t.Fatalf("events = %d, want %d", len(evs), len(evsBefore))
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

// gitOut runs git in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

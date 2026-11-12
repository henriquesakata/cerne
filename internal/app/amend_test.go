package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/plantmpl"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
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

	f, changed, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody, "critério 2 sem prova")
	if err != nil || !changed {
		t.Fatalf("AmendFeatureResult: changed %v, err %v", changed, err)
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
	if _, _, err := AmendFeatureResult(repoDir, projDir, "F-0001", shown, "segunda emenda"); err != nil {
		t.Fatalf("amend with frontmatter: %v", err)
	}
	got, _ = os.ReadFile(FeatureDocPath(repoDir, "F-0001", "result.md"))
	if strings.Count(string(got), "cerne_id:") != 1 || !strings.Contains(string(got), "imported_from: docs/x/") {
		t.Fatalf("result.md = %q, want the body's own frontmatter, once", got)
	}

	// Another feature's document is refused, not restamped.
	if _, _, err := AmendFeatureResult(repoDir, projDir, "F-0001", strings.Replace(shown, "F-0001", "F-0002", 1), "arquivo errado"); err == nil {
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

	_, _, err := AmendFeatureResult(repoDir, projDir, "F-0001", "# Resultado\n\nSem registro do Gauntlet.\n", "trocar o result")
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

// TestAmendIdenticalIsNoOp: an amend whose body equals the stored result writes
// nothing, appends no event and makes no commit (like SaveFeatureDoc's
// byte-identical write) — and still refuses a closed feature first.
func TestAmendIdenticalIsNoOp(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented, "spec.md", "result.md")
	if _, _, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody, "primeira"); err != nil {
		t.Fatalf("first amend: %v", err)
	}
	evsBefore, _ := store.ReadEvents(repoDir)
	head0, _ := gitback.HeadSHA(origin)

	f, changed, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody, "de novo")
	if err != nil || changed || f.ID != "F-0001" {
		t.Fatalf("identical amend: f %s, changed %v, err %v — want a successful no-op", f.ID, changed, err)
	}
	if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evsBefore) {
		t.Fatalf("events = %d, want %d", len(evs), len(evsBefore))
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}

	seedFeatureInState(t, repoDir, "F-0002", domain.StateDone, "spec.md", "result.md")
	stored, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0002", "result.md"))
	if _, _, err := AmendFeatureResult(repoDir, projDir, "F-0002", string(stored), "igual"); !errors.Is(err, ErrAmendNotImplemented) {
		t.Fatalf("done, identical body: err = %v, want ErrAmendNotImplemented", err)
	}
}

// TestAmendUnchangedIsJudgedAgainstOrigin (F-0328 critic C3): same as the
// spec — an amend body that only matches a clone trailing origin is written,
// not reported unchanged.
func TestAmendUnchangedIsJudgedAgainstOrigin(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented, "spec.md", "result.md")
	if _, changed, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody, "primeira"); err != nil || !changed {
		t.Fatalf("first amend: changed %v, err %v", changed, err)
	}
	const rel = "content/features/F-0001/result.md"
	advanceRemote(t, origin, rel, "---\ncerne_id: F-0001\n---\n\n# Outro resultado\n"+testGauntletTail)

	if _, changed, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody, "de volta"); err != nil || !changed {
		t.Fatalf("stale clone: changed %v, err %v — want a write, origin holds another result", changed, err)
	}
	if got, _ := originHas(t, origin, rel); got != "---\ncerne_id: F-0001\n---\n\n"+strings.TrimSpace(amendedBody)+"\n" {
		t.Fatalf("origin result = %q, want the amended body", got)
	}
}

// TestStampedDocFrontmatter (F-0328 crit. 1-3, the rule F-0305 wrote for the
// amend): the body's opening block is kept when it names the feature — quoted
// or not, stored unquoted like every reader expects — and a refusal says what
// is wrong with it, for the document being written. The keys an OKF pull
// projects (type, state, timestamp…) are dropped: a stale state must never
// reach the backend (C14). Each refusal says what: another feature's id, or a
// '---' block (a frontmatter or a horizontal rule) that carries no cerne_id at
// all, or one that never closes — a block is only "key: value" and blank lines
// up to its '---', so a later rule or code fence never closes it (F-0328
// critic C2), and a block right after the first is a second frontmatter:
// ours again is dropped, anything else refused (C1) — in the amend's own words, with no sentinel
// prefix. Every accepted form is a fixed point: the
// stored file fed back (what `cerne show --doc` prints, one newline more) is
// stored unchanged, which is what makes an identical rewrite detectable.
func TestStampedDocFrontmatter(t *testing.T) {
	const rest = "\n# R\n\nCorpo.\n"
	for _, tc := range []struct {
		name, doc, body, want, refusal string
	}{
		{"no block", "result", "# R\n\nCorpo.\n", "---\ncerne_id: F-0001\n---\n\n# R\n\nCorpo.\n", ""},
		{"plain id", "result", "---\ncerne_id: F-0001\nx: y\n---\n" + rest, "---\ncerne_id: F-0001\nx: y\n---\n" + rest, ""},
		{"double-quoted id", "result", "---\ncerne_id: \"F-0001\"\n---\n" + rest, "---\ncerne_id: F-0001\n---\n" + rest, ""},
		{"single-quoted id", "result", "---\ncerne_id: 'F-0001'\n---\n" + rest, "---\ncerne_id: F-0001\n---\n" + rest, ""},
		{"bom and blank lines", "spec", "\ufeff\n\n# R\n\nCorpo.\n\n\n", "---\ncerne_id: F-0001\n---\n\n# R\n\nCorpo.\n", ""},
		{"other id", "result", "---\ncerne_id: F-0002\n---\n" + rest, "", `the body's frontmatter names cerne_id "F-0002", not F-0001 — is it another feature's result?`},
		{"other id, spec", "spec", "---\ncerne_id: F-0002\n---\n" + rest, "", `the body's frontmatter names cerne_id "F-0002", not F-0001 — is it another feature's spec?`},
		{"block without id", "result", "---\ntitle: x\n---\n" + rest, "", "the body opens a '---' block without cerne_id: start from 'cerne show F-0001 --doc result' or drop the block"},
		{"block without id, spec", "spec", "---\ntitle: x\n---\n" + rest, "", "the body opens a '---' block without cerne_id: start from 'cerne show F-0001 --doc spec' or drop the block"},
		{"horizontal rule", "result", "---\n" + rest, "", "the body's '---' block never closes: close it with a '---' line, or start from 'cerne show F-0001 --doc result'"},
		{"crlf", "spec", "---\r\ncerne_id: F-0001\r\nx: y\r\n---\r\n\r\n# R\r\n\r\nCorpo.\r\n", "---\ncerne_id: F-0001\nx: y\n---\n\n# R\n\nCorpo.\n", ""},
		{"bom after a blank line", "spec", "\n\ufeff---\ncerne_id: F-0001\n---\n" + rest, "---\ncerne_id: F-0001\n---\n" + rest, ""},
		{"blank line inside the block", "spec", "---\ncerne_id: F-0001\n\nx: y\n---\n" + rest, "---\ncerne_id: F-0001\n\nx: y\n---\n" + rest, ""},
		{"closed only by a later rule", "spec", "---\ncerne_id: F-0001\n\n# R\n\n## Problema\n\nTexto real.\n\n---\n", "", "the body's '---' block never closes: close it with a '---' line, or start from 'cerne show F-0001 --doc spec'"},
		{"rule in the middle", "result", "---\ncerne_id: F-0001\n# R\n\n---\n\nMais.\n", "", "the body's '---' block never closes: close it with a '---' line, or start from 'cerne show F-0001 --doc result'"},
		{"dashes inside a code fence", "spec", "---\ncerne_id: F-0001\n\n```yaml\n---\nx: 1\n```\n", "", "the body's '---' block never closes: close it with a '---' line, or start from 'cerne show F-0001 --doc spec'"},
		{"okf projection fed back", "spec", "---\ntype: spec\ntitle: \"Login: Google\"\ndescription: x\ntimestamp: 2026-11-05T10:00:00Z\ntags: [a, b]\nstate: candidate\nowner: x\ncerne_id: F-0001\n---\n" + rest, "---\nowner: x\ncerne_id: F-0001\n---\n" + rest, ""},
		{"own block twice", "spec", "---\ncerne_id: F-0001\n---\n\n---\ncerne_id: 'F-0001'\nx: y\n---\n" + rest, "---\ncerne_id: F-0001\n---\n" + rest, ""},
		{"own block, then a rule", "spec", "---\ncerne_id: F-0001\n---\n\n---\n" + rest, "---\ncerne_id: F-0001\n---\n\n---\n" + rest, ""},
		{"own block, then another feature's", "spec", "---\ncerne_id: F-0001\n---\n\n---\ncerne_id: F-0002\n---\n" + rest, "", `the body's frontmatter names cerne_id "F-0002", not F-0001 — is it another feature's spec?`},
		{"own block, then one without id", "spec", "---\ncerne_id: F-0001\n---\n\n---\ntitle: x\n---\n" + rest, "", "the body opens a '---' block without cerne_id: start from 'cerne show F-0001 --doc spec' or drop the block"},
		{"indented key", "spec", "---\n  cerne_id: F-0001\n---\n" + rest, "", "the body's '---' block never closes: close it with a '---' line, or start from 'cerne show F-0001 --doc spec'"},
		{"empty id", "result", "---\ncerne_id:\n---\n" + rest, "", "the body opens a '---' block without cerne_id: start from 'cerne show F-0001 --doc result' or drop the block"},
		{"block never closes", "result", "---\ncerne_id: F-0001\n# R\n\nCorpo.\n", "", "the body's '---' block never closes: close it with a '---' line, or start from 'cerne show F-0001 --doc result'"},
		{"block never closes, spec", "spec", "---\ncerne_id: F-0001\n# R\n", "", "the body's '---' block never closes: close it with a '---' line, or start from 'cerne show F-0001 --doc spec'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stampedDoc("F-0001", tc.doc, tc.body, false)
			if tc.refusal != "" {
				// The full text is pinned: the result's messages are the amend's own,
				// unchanged since F-0305 — the sentinel matches without a prefix.
				if !errors.Is(err, ErrBodyFrontmatter) || err.Error() != tc.refusal {
					t.Fatalf("err = %v, want ErrBodyFrontmatter saying exactly %q", err, tc.refusal)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v — want %q", got, err, tc.want)
			}
			if again, err := stampedDoc("F-0001", tc.doc, got+"\n", false); err != nil || again != got {
				t.Fatalf("not a fixed point: %q, %v — want %q", again, err, got)
			}
		})
	}
}

// TestStampedDocNewFeature (F-0328 critic C4): on creation the id is not
// minted yet, so a refusal names only what the body carries and never points
// at a `cerne show` of a feature that does not exist; a block naming the id
// about to be minted is kept like any own block.
func TestStampedDocNewFeature(t *testing.T) {
	const rest = "\n# R\n\nCorpo.\n"
	if got, err := stampedDoc("F-0002", "spec", "---\ncerne_id: F-0002\nx: y\n---\n"+rest, true); err != nil || got != "---\ncerne_id: F-0002\nx: y\n---\n"+rest {
		t.Fatalf("own block on creation: %q, %v", got, err)
	}
	for _, tc := range []struct{ name, body, refusal string }{
		{"other id", "---\ncerne_id: F-0009\n---\n" + rest, "the body carries cerne_id F-0009 — a new feature's spec cannot carry another feature's frontmatter: drop the block"},
		{"own block, then another feature's", "---\ncerne_id: F-0002\n---\n\n---\ncerne_id: F-0009\n---\n" + rest, "the body carries cerne_id F-0009 — a new feature's spec cannot carry another feature's frontmatter: drop the block"},
		{"block without id", "---\ntitle: x\n---\n" + rest, "the body opens a '---' block without cerne_id: drop it — cerne stamps a new feature's spec itself"},
		{"block never closes", "---\ncerne_id: F-0002\n# R\n", "the body's '---' block never closes: close it with a '---' line, or drop it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := stampedDoc("F-0002", "spec", tc.body, true)
			if !errors.Is(err, ErrBodyFrontmatter) || err.Error() != tc.refusal {
				t.Fatalf("err = %v, want ErrBodyFrontmatter saying exactly %q", err, tc.refusal)
			}
		})
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

// amendedSpec is a spec body the doc amend stores behind cerne's stamp.
const amendedSpec = "# Doc\n\n## Objetivo\n\n1. O critério reescrito com o prefixo novo.\n" + testBarTail

// TestAmendDocArchivesRevisionWithEvent (F-0338 crit. 1): the spec of an
// approved feature and the plan of one in development are replaced in one
// commit, the replaced body kept byte for byte as revision N of that document,
// with a feature.doc_edited event carrying doc, state, reason, rev and actor;
// the state does not move, and each document counts its own revisions.
func TestAmendDocArchivesRevisionWithEvent(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateInDevelopment, "spec.md", "plan.md")
	before, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
	head0, _ := gitback.HeadSHA(origin)

	f, rev, changed, err := AmendFeatureDoc(repoDir, projDir, "F-0001", "spec", amendedSpec, "prefixo mudou")
	if err != nil || !changed || rev != 1 || f.State != domain.StateApproved {
		t.Fatalf("AmendFeatureDoc = %s, rev %d, changed %v, err %v", f.State, rev, changed, err)
	}
	if r1, _ := os.ReadFile(filepath.Join(repoDir, "content", "revisions", "F-0001", "spec", "r1.md")); string(r1) != string(before) {
		t.Fatalf("r1 = %q, want the replaced spec byte for byte %q", r1, before)
	}
	if got, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md")); string(got) != "---\ncerne_id: F-0001\n---\n\n"+strings.TrimSpace(amendedSpec)+"\n" {
		t.Fatalf("spec.md = %q, want the new body behind cerne's stamp", got)
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "feature.doc_edited" || last.EntityID != "F-0001" || last.Payload["doc"] != "spec" ||
		last.Payload["state"] != string(domain.StateApproved) || last.Payload["reason"] != "prefixo mudou" ||
		last.Payload["rev"] != "1" || last.Actor != EventActor() {
		t.Fatalf("last event = %+v, want feature.doc_edited{doc:spec,state:approved,reason,rev:1}", last)
	}
	if log := gitOut(t, origin, "log", "--format=%s", head0+"..HEAD"); log != "cerne: amend F-0001/spec.md" {
		t.Fatalf("origin commits = %q, want the single amend commit", log)
	}
	if loaded, _ := store.LoadFeature(repoDir, "F-0001"); loaded.State != domain.StateApproved {
		t.Fatalf("persisted state = %s", loaded.State)
	}

	if _, rev, _, err := AmendFeatureDoc(repoDir, projDir, "F-0001", "spec", amendedSpec+"\nMais.\n", "segunda"); err != nil || rev != 2 {
		t.Fatalf("second spec amend: rev %d, err %v — want 2", rev, err)
	}
	f, rev, changed, err = AmendFeatureDoc(repoDir, projDir, "F-0002", "plan", "# Plano\n\n1. Tarefa reescrita.\n", "auditoria")
	if err != nil || !changed || rev != 1 || f.State != domain.StateInDevelopment {
		t.Fatalf("plan amend = %s, rev %d, changed %v, err %v", f.State, rev, changed, err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "revisions", "F-0002", "plan", "r1.md")); err != nil {
		t.Fatalf("plan revision: %v", err)
	}
}

// TestAmendDocRefusesStates (F-0338 crit. 2, 3): a candidate is refused with
// the path of a draft (pull/push or `cerne feature spec`, no plan yet); the
// closed states with DocFixPhrase for the document asked. Nothing is written.
func TestAmendDocRefusesStates(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	states := []domain.FeatureState{domain.StateCandidate, domain.StateImplemented, domain.StateDone, domain.StateRejected, domain.StateSuperseded}
	for i, s := range states {
		seedFeatureInState(t, repoDir, fmt.Sprintf("F-%04d", i+1), s, "spec.md", "plan.md")
	}
	head0, _ := gitback.HeadSHA(origin)
	for i, s := range states {
		id := fmt.Sprintf("F-%04d", i+1)
		for _, doc := range []string{"spec", "plan"} {
			before, _ := os.ReadFile(FeatureDocPath(repoDir, id, doc+".md"))
			_, _, _, err := AmendFeatureDoc(repoDir, projDir, id, doc, amendedSpec, "corrigir")
			want := DocFixPhrase(id, doc, s)
			if s == domain.StateCandidate {
				want = "'cerne feature spec " + id + "'"
			}
			if !errors.Is(err, ErrAmendDocState) || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s %s %s: err = %v, want ErrAmendDocState naming %q", id, s, doc, err, want)
			}
			if s == domain.StateCandidate && !strings.Contains(err.Error(), "no plan yet") {
				t.Fatalf("candidate refusal %q does not say the plan does not exist", err)
			}
			if after, _ := os.ReadFile(FeatureDocPath(repoDir, id, doc+".md")); string(after) != string(before) {
				t.Fatalf("%s %s changed", id, doc)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "revisions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refusal archived a revision: %v", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

// TestAmendDocInputAndNoOp (F-0338 crit. 4, 5): no reason, an empty body, an
// unknown document or an appended amendment heading is refused before the
// backend; a body equal to the stored one by specContent's normalization
// (frontmatter, CRLF, trailing blanks) is a no-op — no revision, no event, no
// commit.
func TestAmendDocInputAndNoOp(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	head0, _ := gitback.HeadSHA(origin)
	for _, tc := range []struct {
		name, doc, body, reason string
		want                    error
		text                    string
	}{
		{"no reason", "spec", amendedSpec, " ", nil, "needs a reason"},
		{"empty body", "spec", " \n", "r", nil, "empty spec body"},
		{"result", "result", amendedSpec, "r", ErrUnknownDoc, ""},
		{"appended amendment", "spec", amendedSpec + "\n## Emenda (2026-11-10)\n\nTroca o prefixo.\n", "r", ErrAppendedAmendment, "line 11"},
	} {
		_, _, _, err := AmendFeatureDoc(repoDir, projDir, "F-0001", tc.doc, tc.body, tc.reason)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || !strings.Contains(err.Error(), tc.text) {
			t.Fatalf("%s: err = %v, want %v containing %q", tc.name, err, tc.want, tc.text)
		}
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved on a refused amend: %s -> %s", head0, h)
	}

	stored, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
	same := strings.ReplaceAll(strings.TrimPrefix(string(stored), "---\ncerne_id: F-0001\n---\n"), "\n", "  \r\n") + "\n\n"
	evsBefore, _ := store.ReadEvents(repoDir)
	_, rev, changed, err := AmendFeatureDoc(repoDir, projDir, "F-0001", "spec", same, "igual")
	if err != nil || changed || rev != 0 {
		t.Fatalf("same body: rev %d, changed %v, err %v — want a no-op", rev, changed, err)
	}
	if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evsBefore) {
		t.Fatalf("events = %d, want %d", len(evs), len(evsBefore))
	}
	if k, _ := store.CountDocRevisions(repoDir, "F-0001", "spec"); k != 0 {
		t.Fatalf("revisions = %d, want 0", k)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved on a no-op: %s -> %s", head0, h)
	}
}

// TestAmendDocStalesApprovalOnlyForSpec (F-0338 crit. 6): amending the spec of
// an approved feature stales its approval like any edit; amending the plan
// leaves it as it was — the approve keeps no copy of the plan.
func TestAmendDocStalesApprovalOnlyForSpec(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	for _, id := range []string{"F-0001", "F-0002"} {
		seedFeatureInState(t, repoDir, id, domain.StateApproved, "spec.md", "plan.md")
		if err := keepApprovedSpec(repoDir, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed approved copies"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := AmendFeatureDoc(repoDir, projDir, "F-0001", "spec", amendedSpec, "spec"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := AmendFeatureDoc(repoDir, projDir, "F-0002", "plan", "# Plano\n\n1. Outra tarefa.\n", "plano"); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"F-0001": true, "F-0002": false} {
		f, _ := store.LoadFeature(repoDir, id)
		if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale != want {
			t.Fatalf("%s: approval stale = %v, want %v", id, stale, want)
		}
	}
}

// TestAmendDocRefusesMissingDoc (F-0338, TL finding on slice 1): a document
// that is absent, or still only the template stub, has nothing to amend — an
// empty r1 would be permanent junk in an append-only history and a line in
// `## amendments` that amended nothing. Refused without writing, pointing to
// .docs/ + `cerne push` for the first text.
func TestAmendDocRefusesMissingDoc(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateApproved, "spec.md")
	stub := "---\ncerne_id: F-0002\n---\n\n" + plantmpl.Stub("Feature F-0002")
	if err := os.WriteFile(FeatureDocPath(repoDir, "F-0002", "plan.md"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed plan stub"); err != nil {
		t.Fatal(err)
	}
	head0, _ := gitback.HeadSHA(origin)
	for _, id := range []string{"F-0001", "F-0002"} {
		_, _, _, err := AmendFeatureDoc(repoDir, projDir, id, "plan", "# Plano\n\n1. Tarefa.\n", "primeiro plano")
		if !errors.Is(err, ErrAmendNoDoc) || !strings.Contains(err.Error(), "cerne push") {
			t.Fatalf("%s: err = %v, want ErrAmendNoDoc pointing to .docs/ + cerne push", id, err)
		}
	}
	if _, err := os.Stat(FeatureDocPath(repoDir, "F-0001", "plan.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused amend wrote the plan: %v", err)
	}
	if got, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0002", "plan.md")); string(got) != stub {
		t.Fatalf("a refused amend replaced the stub: %q", got)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "revisions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refusal archived a revision: %v", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

// TestAmendDocRefusesBodyWithoutRealContent (F-0338 crit. 4, round 2): a new
// body that is only frontmatter or a template stub, and a reason with a line
// break, are refused before the backend: nothing written, no revision.
func TestAmendDocRefusesBodyWithoutRealContent(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	head0, _ := gitback.HeadSHA(origin)
	before, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
	for _, tc := range []struct{ name, body, reason, text string }{
		{"frontmatter only", "---\ncerne_id: F-0001\n---\n", "r", "empty"},
		{"spec stub", spectmpl.Stub("Feature F-0001"), "r", "empty"},
		{"plan stub as spec", "---\ncerne_id: F-0001\n---\n\n" + plantmpl.Stub("Feature F-0001"), "r", "empty"},
		{"reason with LF", amendedSpec, "a\n## x", "one line"},
		{"reason with CR", amendedSpec, "a\rb", "one line"},
	} {
		_, _, _, err := AmendFeatureDoc(repoDir, projDir, "F-0001", "spec", tc.body, tc.reason)
		if err == nil || !strings.Contains(err.Error(), tc.text) {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.text)
		}
	}
	if got, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md")); string(got) != string(before) {
		t.Fatalf("a refused amend replaced the spec: %q", got)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "revisions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refusal archived a revision: %v", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

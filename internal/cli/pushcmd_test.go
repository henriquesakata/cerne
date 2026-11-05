package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// pulledProject builds the standard push fixture: a project with feature
// F-0001 carrying a real spec, already pulled into .docs/.
func pulledProject(t *testing.T) (projDir, origin, repoDir, specPath string) {
	t.Helper()
	projDir, origin, repoDir = setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	specPath = filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento", "spec.md")
	return projDir, origin, repoDir, specPath
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func sha256OfString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestPushNetNewPlan: a plan.md authored after the feature-new spec stub is
// pushable (the cerne-native flow), written to the backend and tracked.
func TestPushNetNewPlan(t *testing.T) {
	projDir, origin, repoDir, _ := pulledProject(t)
	planPath := filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento", "plan.md")
	planBody := "---\ncerne_id: F-0001\n---\n\n# Plano\n\nPassos do parcelamento.\n"
	mustWrite(t, planPath, planBody)

	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push net-new plan: %v\noutput: %s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "plan.md"))
	if err != nil || string(got) != planBody {
		t.Fatalf("backend plan = %q, %v; want the authored plan verbatim", string(got), err)
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("clone verify: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(verify, "content", "features", "F-0001", "plan.md")); err != nil {
		t.Fatalf("plan not pushed to origin: %v", err)
	}
	manifest, err := project.LoadManifest(projDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if _, ok := manifest["current/features/F-0001-parcelamento/plan.md"]; !ok {
		t.Fatalf("manifest missing the new plan.md: %+v", manifest)
	}
}

func TestPushGoldenPath(t *testing.T) {
	projDir, origin, repoDir, specPath := pulledProject(t)

	edited := "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nDividir compras em parcelas.\n\n## Critérios de aceite\n\n- Editado localmente antes do push.\n"
	mustWrite(t, specPath, edited)

	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push: %v\noutput: %s", err, out)
	}

	// Verbatim write-back into the backend clone.
	backendSpec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("backend spec: %v", err)
	}
	if string(backendSpec) != edited {
		t.Fatalf("backend spec = %q, want the edited content verbatim", backendSpec)
	}

	// The push reached origin: a fresh clone sees the edit.
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone verify: %v", err)
	}
	pushed, err := os.ReadFile(filepath.Join(verify, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("spec in fresh clone: %v", err)
	}
	if string(pushed) != edited {
		t.Fatalf("origin spec = %q, want the edited content", pushed)
	}

	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if lock.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, originHead)
	}
	rel := "current/features/F-0001-parcelamento/spec.md"
	manifest, err := project.LoadManifest(projDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest[rel] != sha256OfString(edited) {
		t.Fatalf("manifest[%s] = %s, want sha256 of the edited content", rel, manifest[rel])
	}
	want := "Sent: 1 document(s) (snapshot " + originHead[:8] + ")."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing %q:\n%s", want, out)
	}

	// Second push: nothing left to send.
	out, err = execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("second push: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nothing to send.") {
		t.Fatalf("second push output = %q, want 'Nothing to send.'", out)
	}
}

// A concurrent push to a DIFFERENT document no longer blocks: the push
// auto-rebases onto the advanced backend and succeeds, preserving both (C1/C2).
func TestPushAutoRebasesDisjointAdvance(t *testing.T) {
	projDir, origin, _, specPath := pulledProject(t)

	// Another machine pushes plan.md, disjoint from our spec.md edit.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	theirPlan := realDoc("F-0001", "Plano vindo de outra máquina.")
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "plan.md"), theirPlan)
	if _, err := gitback.CommitAndPush(clone2, "external disjoint change"); err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	// Our disjoint edit pushes cleanly — no --force, no manual retry.
	mineSpec := realDoc("F-0001", "Edição local que coexiste com o avanço remoto.")
	mustWrite(t, specPath, mineSpec)
	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("disjoint push should succeed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Sent: 1 document(s)") {
		t.Fatalf("output missing success:\n%s", out)
	}

	// Origin holds BOTH the remote plan and our spec.
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone verify: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(verify, "content", "features", "F-0001", "plan.md")); err != nil || string(got) != theirPlan {
		t.Fatalf("remote plan.md lost: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(verify, "content", "features", "F-0001", "spec.md")); err != nil || string(got) != mineSpec {
		t.Fatalf("our spec.md not pushed: %q err=%v", got, err)
	}

	// docs.lock advanced to the new origin HEAD.
	lock, _ := project.LoadLock(projDir)
	originHead, _ := gitback.HeadSHA(origin)
	if lock.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, originHead)
	}
}

// Two machines editing the SAME document: the second push is a conflict (named
// + diffed), nothing is sent, the remote keeps its version (C3/C7).
func TestPushSameDocConflict(t *testing.T) {
	projDir, origin, _, specPath := pulledProject(t)

	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	theirSpec := realDoc("F-0001", "Versão do outro agente para o MESMO documento.")
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "spec.md"), theirSpec)
	theirHead, err := gitback.CommitAndPush(clone2, "their spec edit")
	if err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	mineSpec := realDoc("F-0001", "Minha versão divergente do MESMO documento.")
	mustWrite(t, specPath, mineSpec)
	out, err := execCerne(t, "push", "--dir", projDir)
	if err == nil {
		t.Fatalf("same-document push should conflict\noutput: %s", out)
	}
	if !strings.Contains(out, "Conflict in 1 document(s)") ||
		!strings.Contains(out, "content/features/F-0001/spec.md") {
		t.Fatalf("conflict output missing doc name:\n%s", out)
	}
	// The diff shows both sides, distinguishing a real conflict from a false collision.
	if !strings.Contains(out, "Minha versão") || !strings.Contains(out, "Versão do outro") {
		t.Fatalf("conflict output missing the diff:\n%s", out)
	}
	// Non-destructive: origin still holds their version, mine not pushed.
	if originHead, _ := gitback.HeadSHA(origin); originHead != theirHead {
		t.Fatalf("origin HEAD = %s, want untouched %s (nothing pushed on conflict)", originHead, theirHead)
	}
}

func TestPushEditedContextErrors(t *testing.T) {
	projDir, _, _, _ := pulledProject(t)
	ctxPath := filepath.Join(projDir, ".docs", "CONTEXT.md")
	body, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("read CONTEXT.md: %v", err)
	}
	mustWrite(t, ctxPath, string(body)+"\nEdição manual indevida.\n")

	out, err := execCerne(t, "push", "--dir", projDir)
	if err == nil {
		t.Fatalf("push with edited CONTEXT.md succeeded\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "CONTEXT.md is generated") || !strings.Contains(err.Error(), "run 'cerne pull' to restore") {
		t.Fatalf("error %q, want the generated-CONTEXT.md message", err)
	}
}

func TestPushMissingCerneID(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	mustWrite(t, specPath, "# Parcelamento\n\nEdição que perdeu o frontmatter.\n")

	out, err := execCerne(t, "push", "--dir", projDir)
	if err == nil {
		t.Fatalf("push without cerne_id succeeded\noutput: %s", out)
	}
	msg := err.Error()
	for _, want := range []string{
		"cerne_id",
		"current/features/F-0001-parcelamento/spec.md",
		"new documents are born from 'cerne feature new'",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestPushUntrackedFile(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	mustWrite(t, filepath.Join(filepath.Dir(specPath), "notes.md"), "# Notas soltas\n")

	out, err := execCerne(t, "push", "--dir", projDir)
	if err == nil {
		t.Fatalf("push with untracked file succeeded\noutput: %s", out)
	}
	msg := err.Error()
	for _, want := range []string{
		"untracked file",
		"current/features/F-0001-parcelamento/notes.md",
		"run 'cerne pull' to resync",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

// TestPushOpensPickupAutoWork (F-0208 crit. 1): pushing an edited plan.md for
// a still-approved feature opens the pickup auto-work in the same commit as
// the push — the agent's first backend touch after claiming the feature.
func TestPushOpensPickupAutoWork(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	planPath := filepath.Join(projDir, ".docs", "current", "features", "F-0001-feature-a", "plan.md")
	mustWrite(t, planPath, "---\ncerne_id: F-0001\n---\n\n# Plano\n\nPasso 1: implementar.\n")

	if out, err := execCerne(t, "push", "--dir", projDir); err != nil {
		t.Fatalf("push: %v\noutput: %s", err, out)
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		t.Fatalf("LoadAllWorks: %v", err)
	}
	if len(works) != 1 || !works[0].Auto || works[0].State != domain.WorkActive || works[0].Refs[0] != id {
		t.Fatalf("works = %+v, want 1 active auto ref %s", works, id)
	}
}

func TestPushDecisionDoc(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	d := store.Decision{ID: "D-0001", Title: "Usar Postgres", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	docDir := filepath.Join(repoDir, "content", "decisions")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mustWrite(t, filepath.Join(docDir, "D-0001.md"), "---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\nMotivação original.\n")
	if _, err := gitback.CommitAndPush(repoDir, "seed decision"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}

	matPath := filepath.Join(projDir, ".docs", "current", "decisions", "D-0001-usar-postgres.md")
	edited := "---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\nMotivação original.\n\n## Consequências\n\n- Editado localmente.\n"
	mustWrite(t, matPath, edited)

	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push: %v\noutput: %s", err, out)
	}
	backendDoc, err := os.ReadFile(filepath.Join(repoDir, "content", "decisions", "D-0001.md"))
	if err != nil {
		t.Fatalf("backend decision doc: %v", err)
	}
	if string(backendDoc) != edited {
		t.Fatalf("backend decision doc = %q, want the edited content mapped to content/decisions/D-0001.md", backendDoc)
	}
	if !strings.Contains(out, "Sent: 1 document(s)") {
		t.Fatalf("output missing 'Enviado: 1 documento(s)':\n%s", out)
	}
}

// Criterion 5 (F-0102): push reconciles from the session sidecar, so a stale
// committed docs.lock (the normal state on a feature branch with pin_branch
// set) does not turn the push into a false same-document conflict.
func TestPushUsesSessionSnapshotBase(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	// Regress the committed lock to an ancient snapshot: with the old
	// lock-as-base behavior the spec would look changed-both-ways and
	// conflict; with the sidecar base it is only a local edit.
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	lock.Snapshot = ""
	if err := project.SaveLock(projDir, lock); err != nil {
		t.Fatalf("SaveLock stale: %v", err)
	}
	mustWrite(t, specPath, realDoc("F-0001", "Editado na feature branch."))

	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push with stale lock but valid sidecar: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sent: 1 document(s)") {
		t.Fatalf("push should send the edit:\n%s", out)
	}
}

// Criterion 5 fallback (F-0102): without the sidecar (pre-0102 checkout) the
// committed docs.lock is still the base; with neither, push asks for a pull.
func TestPushFallsBackToLockBase(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	if err := os.Remove(filepath.Join(projDir, ".docs", ".snapshot")); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}
	mustWrite(t, specPath, realDoc("F-0001", "Editado sem sidecar."))
	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push without sidecar (lock base): %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sent: 1 document(s)") {
		t.Fatalf("push should send the edit via the lock base:\n%s", out)
	}

	// Neither sidecar nor docs.lock: the pre-0102 guidance error.
	if err := os.Remove(filepath.Join(projDir, ".docs", ".snapshot")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove sidecar again: %v", err)
	}
	if err := os.Remove(filepath.Join(projDir, "docs.lock")); err != nil {
		t.Fatalf("remove docs.lock: %v", err)
	}
	mustWrite(t, specPath, realDoc("F-0001", "Editado sem base nenhuma."))
	out, err = execCerne(t, "push", "--dir", projDir)
	if err == nil {
		t.Fatalf("push without any base should fail\n%s", out)
	}
	if !strings.Contains(out, "run 'cerne pull' first") {
		t.Fatalf("error should guide to pull:\n%s", out)
	}
}

func TestPushDryRunReportsAndDoesNotPush(t *testing.T) {
	projDir, origin, repoDir, specPath := pulledProject(t)
	repoHeadBefore, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA repo: %v", err)
	}
	originHeadBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	// A tracked local edit, not yet pushed.
	mustWrite(t, specPath, realDoc("F-0001", "Conteúdo editado, validado em dry-run."))

	out, _ := execCerne(t, "push", "--dry-run", "--dir", projDir)
	if !strings.Contains(out, "content/features/F-0001/spec.md") {
		t.Fatalf("dry-run should list the doc that would go:\n%s", out)
	}
	// No commit/push happened.
	if h, _ := gitback.HeadSHA(repoDir); h != repoHeadBefore {
		t.Fatalf("dry-run mutated the cache clone: %s -> %s", repoHeadBefore, h)
	}
	if h, _ := gitback.HeadSHA(origin); h != originHeadBefore {
		t.Fatalf("dry-run pushed to origin: %s -> %s", originHeadBefore, h)
	}
	// The edit is still pending — a real push after the dry-run sends it.
	out2, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("real push after dry-run: %v\n%s", err, out2)
	}
	if !strings.Contains(out2, "Sent: 1 document(s)") {
		t.Fatalf("real push should send the still-pending edit:\n%s", out2)
	}
}

// TestPushRefusesChangedDocOfFrozenFeature (F-0305 crit. 7): between the
// complete and the next pull the result is still tracked in .docs/; an edit to
// it is refused by push, push --dry-run and lint --staged alike, naming the
// state's path, with nothing sent. The untouched tracked docs pass silently.
func TestPushRefusesChangedDocOfFrozenFeature(t *testing.T) {
	projDir, repoDir, id := startedFeature(t)
	writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
		t.Fatalf("complete: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "push", "--dir", projDir); err != nil || !strings.Contains(out, "Nothing to send.") {
		t.Fatalf("clean .docs/ of a frozen feature: err %v\n%s", err, out)
	}
	resultBackend := filepath.Join(repoDir, "content", "features", id, "result.md")
	before, _ := os.ReadFile(resultBackend)
	headBefore, _ := gitback.HeadSHA(repoDir)
	mustWrite(t, filepath.Join(projDir, ".docs", "current", "features", "F-0001-feature-a", "result.md"), realDoc(id, "Reescrito depois do complete."))

	for _, args := range [][]string{{"push"}, {"push", "--dry-run"}, {"lint", "--staged"}} {
		_, err := execCerne(t, append(args, "--dir", projDir)...)
		if !errors.Is(err, app.ErrFeatureFrozen) || !strings.Contains(err.Error(), "cerne feature amend "+id) {
			t.Fatalf("%v: err = %v, want ErrFeatureFrozen naming 'cerne feature amend %s'", args, err, id)
		}
	}
	if after, _ := os.ReadFile(resultBackend); string(after) != string(before) {
		t.Fatal("backend result.md changed")
	}
	if h, _ := gitback.HeadSHA(repoDir); h != headBefore {
		t.Fatalf("backend HEAD moved: %s -> %s", headBefore, h)
	}

	// Accepted (done), the same edit points at the errata instead.
	if out, err := execCerne(t, "feature", "accept", id, "--dir", projDir); err != nil {
		t.Fatalf("accept: %v\noutput: %s", err, out)
	}
	_, err := execCerne(t, "push", "--dir", projDir)
	if !errors.Is(err, app.ErrFeatureFrozen) || !strings.Contains(err.Error(), "cerne feature errata "+id) {
		t.Fatalf("done: err = %v, want ErrFeatureFrozen naming 'cerne feature errata %s'", err, id)
	}
	t.Logf("push refusal (done): %v", err)
	if after, _ := os.ReadFile(resultBackend); string(after) != string(before) {
		t.Fatal("backend result.md changed after accept")
	}
}

// TestPushRefusalOfClosedDocUnblocks (F-0305): the refusal tells how to get
// out — amend from the local copy itself (no edit lost), then delete it — and
// following it unblocks push for good: a pull does not resurrect the copy.
func TestPushRefusalOfClosedDocUnblocks(t *testing.T) {
	projDir, repoDir, id := startedFeature(t)
	writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
		t.Fatalf("complete: %v\noutput: %s", err, out)
	}
	rel := ".docs/current/features/F-0001-feature-a/result.md"
	local := filepath.Join(projDir, filepath.FromSlash(rel))
	mustWrite(t, local, realDoc(id, "Reescrito depois do complete."))

	_, err := execCerne(t, "push", "--dir", projDir)
	for _, want := range []string{
		"cerne feature amend " + id + " --file " + rel,
		"unblock:    remove " + rel,
	} {
		if !errors.Is(err, app.ErrFeatureFrozen) || !strings.Contains(err.Error(), want) {
			t.Fatalf("push: err = %v, want ErrFeatureFrozen containing %q", err, want)
		}
	}
	t.Logf("push refusal (implemented): %v", err)

	if out, err := execCerne(t, "feature", "amend", id, "--file", local, "--reason", "reescrito", "--dir", projDir); err != nil {
		t.Fatalf("amend from the local copy: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md")); !strings.Contains(string(got), "Reescrito depois do complete.") {
		t.Fatalf("amend lost the edit:\n%s", got)
	}
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][]string{{"push"}, {"pull"}, {"push"}} {
		out, err := execCerne(t, append(step, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", step, err, out)
		}
		if step[0] == "push" && !strings.Contains(out, "Nothing to send.") {
			t.Fatalf("%v: want Nothing to send., got:\n%s", step, out)
		}
		if _, err := os.Stat(local); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("after %v the local copy is back (stat err %v)", step, err)
		}
	}
}

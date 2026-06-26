package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	want := "Enviado: 1 documento(s) (snapshot " + originHead[:8] + ")."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing %q:\n%s", want, out)
	}

	// Second push: nothing left to send.
	out, err = execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("second push: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nada para enviar.") {
		t.Fatalf("second push output = %q, want 'Nada para enviar.'", out)
	}
}

func TestPushDriftDetected(t *testing.T) {
	projDir, origin, _, specPath := pulledProject(t)
	lockBefore, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}

	// Another machine pushes to origin after our pull.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "plan.md"), realDoc("F-0001", "Plano vindo de outra máquina."))
	remoteSHA, err := gitback.CommitAndPush(clone2, "external change")
	if err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	mustWrite(t, specPath, realDoc("F-0001", "Edição local que não deve ser enviada."))
	out, err := execCerne(t, "push", "--dir", projDir)
	if err == nil {
		t.Fatalf("push with drift succeeded\noutput: %s", out)
	}
	msg := err.Error()
	for _, want := range []string{
		"backend avançou desde seu último pull",
		"rode 'cerne pull' antes do push",
		lockBefore.Snapshot[:8],
		remoteSHA[:8],
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("drift error %q missing %q", msg, want)
		}
	}

	// Nothing was committed: origin HEAD is still the external SHA.
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if originHead != remoteSHA {
		t.Fatalf("origin HEAD = %s, want untouched %s", originHead, remoteSHA)
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
	if !strings.Contains(err.Error(), "CONTEXT.md é gerado") || !strings.Contains(err.Error(), "rode 'cerne pull' para restaurar") {
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
		"novos documentos nascem com 'cerne feature new'",
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
		"não rastreado",
		"current/features/F-0001-parcelamento/notes.md",
		"rode 'cerne pull' para ressincronizar",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
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
	if !strings.Contains(out, "Enviado: 1 documento(s)") {
		t.Fatalf("output missing 'Enviado: 1 documento(s)':\n%s", out)
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
	if !strings.Contains(out2, "Enviado: 1 documento(s)") {
		t.Fatalf("real push should send the still-pending edit:\n%s", out2)
	}
}

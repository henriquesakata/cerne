package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// C4: from the classic deadlock state — an unsent local edit plus a backend that
// advanced on a disjoint document — `cerne sync` moves forward without --force:
// it merges the remote advance and pushes the local edit in one command.
func TestSyncAdvancesFromDeadlock(t *testing.T) {
	projDir, origin, _, specPath := pulledFeatureProject(t, "Conteúdo original.")

	mine := "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nEdição local não-enviada.\n"
	mustWrite(t, specPath, mine)

	// Backend advances on a disjoint document (the deadlock trigger).
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	theirPlan := realDoc("F-0001", "Plano remoto disjunto.")
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "plan.md"), theirPlan)
	if _, err := gitback.CommitAndPush(clone2, "remote disjoint plan"); err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	out, err := execCerne(t, "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("sync should advance from deadlock without --force: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Enviado: 1 documento(s)") {
		t.Fatalf("sync did not push the local edit:\n%s", out)
	}

	// Origin now holds both the remote plan and my pushed spec.
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone verify: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(verify, "content", "features", "F-0001", "plan.md")); err != nil || string(got) != theirPlan {
		t.Fatalf("remote plan lost after sync: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(verify, "content", "features", "F-0001", "spec.md")); err != nil || string(got) != mine {
		t.Fatalf("my spec not pushed by sync: %q err=%v", got, err)
	}

	// Idempotent: a second sync has nothing to send.
	out, err = execCerne(t, "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("second sync: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Nada para enviar.") {
		t.Fatalf("second sync should be a no-op send:\n%s", out)
	}
}

// A real same-document conflict stops sync in the pull step (non-destructive),
// before any push: nothing is sent and the remote keeps its version.
func TestSyncStopsOnSameDocConflict(t *testing.T) {
	projDir, origin, _, specPath := pulledFeatureProject(t, "Conteúdo original.")

	mine := "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nMinha edição não-enviada.\n"
	mustWrite(t, specPath, mine)

	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	theirSpec := realDoc("F-0001", "Versão remota do mesmo documento.")
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "spec.md"), theirSpec)
	theirHead, err := gitback.CommitAndPush(clone2, "remote same-doc edit")
	if err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	out, err := execCerne(t, "sync", "--dir", projDir)
	if err == nil {
		t.Fatalf("sync should stop on a same-document conflict\n%s", out)
	}
	if strings.Contains(out, "Enviado:") {
		t.Fatalf("sync must not push when the pull step conflicts:\n%s", out)
	}
	// Mine kept; remote untouched.
	if got, _ := os.ReadFile(specPath); string(got) != mine {
		t.Fatalf("conflict must keep my version locally:\n%s", got)
	}
	if originHead, _ := gitback.HeadSHA(origin); originHead != theirHead {
		t.Fatalf("origin advanced to %s, want untouched %s", originHead, theirHead)
	}
}

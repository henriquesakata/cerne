package cli

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

func TestFeatureReparent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mustExec := func(args ...string) {
		t.Helper()
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	mustExec("feature", "new", "Épico", "--context", "ctx de teste")
	mustExec("feature", "new", "Filha", "--context", "ctx de teste")

	out, err := execCerne(t, "feature", "reparent", "F-0002", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("reparent: %v\n%s", err, out)
	}
	if !strings.Contains(out, "F-0002 is now a child of F-0001") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	// F-0235 crit.5: reparenting under a still-stub-planned epic prints the advisory.
	if !strings.Contains(out, "plano do épico scaffoldado — preencha antes do approve (N-0031)") {
		t.Fatalf("output missing epic-scaffold advisory:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.Parent != "F-0001" {
		t.Fatalf("parent = %q, want F-0001", f.Parent)
	}

	out, err = execCerne(t, "feature", "reparent", "F-0002", "--clear", "--dir", projDir)
	if err != nil {
		t.Fatalf("clear: %v\n%s", err, out)
	}
	if !strings.Contains(out, "F-0002 detached from its epic") {
		t.Fatalf("clear output missing confirmation:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.Parent != "" {
		t.Fatalf("parent after clear = %q, want empty", f.Parent)
	}

	if _, err := execCerne(t, "feature", "reparent", "F-0002", "--dir", projDir); err == nil {
		t.Fatalf("reparent without parent or --clear succeeded, want error")
	}
}

// TestFeatureReparentEchoesCanonicalParent (F-0326): the confirmation and the
// epic-plan advisory use the ids as stored (canonical), not as typed.
func TestFeatureReparentEchoesCanonicalParent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for _, title := range []string{"Épico", "Filha"} {
		if out, err := execCerne(t, "feature", "new", title, "--context", "ctx de teste", "--dir", projDir); err != nil {
			t.Fatalf("feature new %s: %v\n%s", title, err, out)
		}
	}

	out, err := execCerne(t, "feature", "reparent", "f-0002", " f-0001 ", "--dir", projDir)
	if err != nil {
		t.Fatalf("reparent: %v\n%s", err, out)
	}
	if !strings.Contains(out, "F-0002 is now a child of F-0001.\n") {
		t.Fatalf("confirmation does not name the canonical epic:\n%s", out)
	}
	if !strings.Contains(out, "plano do épico scaffoldado") {
		t.Fatalf("epic-plan advisory missing for the canonical epic:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.Parent != "F-0001" {
		t.Fatalf("parent = %q, want F-0001", f.Parent)
	}
}

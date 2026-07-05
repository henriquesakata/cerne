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
	if !strings.Contains(out, "F-0002 agora é filha de F-0001") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.Parent != "F-0001" {
		t.Fatalf("parent = %q, want F-0001", f.Parent)
	}

	out, err = execCerne(t, "feature", "reparent", "F-0002", "--clear", "--dir", projDir)
	if err != nil {
		t.Fatalf("clear: %v\n%s", err, out)
	}
	if !strings.Contains(out, "F-0002 desvinculada do épico") {
		t.Fatalf("clear output missing confirmation:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.Parent != "" {
		t.Fatalf("parent after clear = %q, want empty", f.Parent)
	}

	if _, err := execCerne(t, "feature", "reparent", "F-0002", "--dir", projDir); err == nil {
		t.Fatalf("reparent without parent or --clear succeeded, want error")
	}
}

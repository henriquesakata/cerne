package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// TestConcurrentDisjointEditsRoundTripViaSync reproduces the two-agents reality:
// two worktrees of the SAME project share one backend and one cache clone. Each
// edits a DIFFERENT document; both reconcile through `cerne sync` with no
// --force, no manual retry, and nothing lost — the deadlock the fix removes.
func TestConcurrentDisjointEditsRoundTripViaSync(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir()) // one shared cache clone for both worktrees
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	// Agent A's worktree seeds the backend with a feature and a note.
	projA := t.TempDir()
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--dir", projA)
	mustCerne(t, "feature", "new", "Feature A", "--dir", projA)
	mustCerne(t, "note", "new", "Nota B", "--dir", projA)
	mustCerne(t, "pull", "--dir", projA)

	// Agent B is a second worktree of the same project (same committed .cernerc).
	projB := t.TempDir()
	if err := os.WriteFile(filepath.Join(projB, ".cernerc"), []byte(mustRead(t, filepath.Join(projA, ".cernerc"))), 0o644); err != nil {
		t.Fatalf("seed projB .cernerc: %v", err)
	}
	mustCerne(t, "pull", "--dir", projB)

	specA := filepath.Join(projA, ".docs", "current", "features", "F-0001-feature-a", "spec.md")
	noteB := filepath.Join(projB, ".docs", "current", "notes", "N-0001-nota-b.md")

	editA := "---\ncerne_id: F-0001\n---\n\n# Feature A\n\nSpec escrita pelo agente A.\n"
	editB := "---\ncerne_id: N-0001\n---\n\n# Nota B\n\nConhecimento escrito pelo agente B.\n"
	if err := os.WriteFile(specA, []byte(editA), 0o644); err != nil {
		t.Fatalf("edit spec A: %v", err)
	}
	if err := os.WriteFile(noteB, []byte(editB), 0o644); err != nil {
		t.Fatalf("edit note B: %v", err)
	}

	// Both sync against the shared backend. The second one's backend has already
	// advanced on a disjoint document — it merges and pushes anyway.
	mustCerne(t, "sync", "--dir", projA)
	mustCerne(t, "sync", "--dir", projB)

	// A fresh clone (a third machine's view) sees BOTH edits intact.
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone verify: %v", err)
	}
	if got := mustRead(t, filepath.Join(verify, "content", "features", "F-0001", "spec.md")); got != editA {
		t.Fatalf("agent A's spec on origin = %q, want %q", got, editA)
	}
	if got := mustRead(t, filepath.Join(verify, "content", "notes", "N-0001.md")); got != editB {
		t.Fatalf("agent B's note on origin = %q, want %q", got, editB)
	}
}

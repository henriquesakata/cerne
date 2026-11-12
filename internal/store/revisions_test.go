package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocRevisions (F-0338): a feature document's replaced bodies live under
// content/revisions/<id>/<doc>/r<N>.md, byte for byte, numbered per document;
// N = K+1 reads the live document, and a runbook keeps its own path.
func TestDocRevisions(t *testing.T) {
	repo := t.TempDir()
	live := filepath.Join(repo, "content", "features", "F-0001", "spec.md")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("live spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for i, body := range []string{"first spec\r\n", "second spec"} {
		n, err := ArchiveDocRevision(repo, "F-0001", "spec", body)
		if err != nil || n != i+1 {
			t.Fatalf("ArchiveDocRevision #%d = %d, %v", i+1, n, err)
		}
		got, _ := os.ReadFile(filepath.Join(repo, "content", "revisions", "F-0001", "spec", "r"+string(rune('1'+i))+".md"))
		if string(got) != body {
			t.Fatalf("r%d = %q, want %q byte for byte", i+1, got, body)
		}
	}
	if n, err := ArchiveDocRevision(repo, "F-0001", "plan", "first plan"); err != nil || n != 1 {
		t.Fatalf("plan counts apart: %d, %v", n, err)
	}
	if k, _ := CountDocRevisions(repo, "F-0001", "spec"); k != 2 {
		t.Fatalf("CountDocRevisions(spec) = %d, want 2", k)
	}

	if got, err := LoadDocRevision(repo, "F-0001", "spec", 1); err != nil || got != "first spec\r\n" {
		t.Fatalf("rev 1 = %q, %v", got, err)
	}
	if got, err := LoadDocRevision(repo, "F-0001", "spec", 3); err != nil || got != "live spec\n" {
		t.Fatalf("rev K+1 = %q, %v — want the live document", got, err)
	}
	if _, err := LoadDocRevision(repo, "F-0001", "spec", 4); err == nil || !strings.Contains(err.Error(), "F-0001 spec has revisions 1..3") {
		t.Fatalf("out of range: %v, want the valid range", err)
	}

	// A runbook stays at content/revisions/<RB>/r<N>.md.
	if n, err := ArchiveRevision(repo, "RB-0001", "old body"); err != nil || n != 1 {
		t.Fatalf("ArchiveRevision = %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "content", "revisions", "RB-0001", "r1.md")); err != nil {
		t.Fatalf("runbook revision path moved: %v", err)
	}
	if _, err := LoadRevision(repo, "RB-0001", 3); err == nil || !strings.Contains(err.Error(), "RB-0001 has revisions 1..2, no revision 3") {
		t.Fatalf("runbook range message changed: %v", err)
	}
}

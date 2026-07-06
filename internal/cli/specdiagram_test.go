package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

// TestSpecDiagramByoKeyWritesAndCommits drives `cerne spec diagram` in byo-key
// mode: the provider returns a Mermaid sequence diagram, which is saved as
// content/artifacts/<id>/sequence.mmd in the backend clone, origin advances and
// the lock snapshot is updated.
func TestSpecDiagramByoKeyWritesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	// The provider returns a Mermaid sequence diagram; \n stays escaped in the
	// JSON body (a literal newline would be invalid JSON), so the model "emits"
	// a two-line diagram.
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"sequenceDiagram\n  A->>B: hi"}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	lockBefore, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock before: %v", err)
	}
	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "spec", "diagram", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("spec diagram byo-key: %v\n%s", err, out)
	}

	// The provider received a prompt mentioning the feature.
	if !strings.Contains(gotPrompt, "Boletos") || !strings.Contains(gotPrompt, "F-0001") {
		t.Fatalf("provider prompt missing the feature id/title:\n%s", gotPrompt)
	}

	// The .mmd artifact was written to the backend clone with the diagram.
	artifactPath := filepath.Join(repoDir, "content", "artifacts", "F-0001", "sequence.mmd")
	mmd, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read drafted diagram: %v", err)
	}
	if !strings.HasPrefix(string(mmd), "sequenceDiagram\n  A->>B: hi") {
		t.Fatalf("diagram missing the synthesized Mermaid:\n%s", mmd)
	}

	// Origin advanced (push happened).
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after spec diagram")
	}
	// Lock snapshot updated to the new origin HEAD.
	lockAfter, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock after: %v", err)
	}
	if lockAfter.Snapshot == lockBefore.Snapshot || lockAfter.Snapshot != headAfter {
		t.Fatalf("lock.Snapshot = %s, want new origin HEAD %s", lockAfter.Snapshot, headAfter)
	}

	// Confirmation naming the .mmd artifact.
	if !strings.Contains(out, "drafted and sent") || !strings.Contains(out, "sequence.mmd") {
		t.Fatalf("spec diagram output missing confirmation:\n%s", out)
	}
}

// TestSpecDiagramByoKeyCustomName honours --name, slugging and saving the
// diagram under the normalized <name>.mmd.
func TestSpecDiagramByoKeyCustomName(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"sequenceDiagram\n  A->>B: hi"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	if out, err := execCerne(t, "spec", "diagram", "F-0001", "--name", "Fluxo Principal", "--dir", projDir); err != nil {
		t.Fatalf("spec diagram custom name: %v\n%s", err, out)
	}
	if _, err := os.ReadFile(filepath.Join(repoDir, "content", "artifacts", "F-0001", "fluxo-principal.mmd")); err != nil {
		t.Fatalf("diagram not saved under normalized name: %v", err)
	}
}

// TestSpecDiagramDelegationPrintsPrompt drives the default (delegation) mode:
// no provider is called, the assembled prompt is printed along with the target
// artifact path, and nothing is written to the backend.
func TestSpecDiagramDelegationPrintsPrompt(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	out, err := execCerne(t, "spec", "diagram", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("spec diagram delegation: %v\n%s", err, out)
	}
	for _, want := range []string{"sequenceDiagram", "F-0001", "content/artifacts/F-0001/sequence.mmd", "cerne push"} {
		if !strings.Contains(out, want) {
			t.Fatalf("delegation output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "F-0001", "sequence.mmd")); err == nil {
		t.Fatalf("delegation mode must not write the artifact")
	}
}

// TestSpecDiagramUnknownFeature errors when the id does not exist in the backend.
func TestSpecDiagramUnknownFeature(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, "http://127.0.0.1:0", "TEST_BYO_KEY")

	_, err := execCerne(t, "spec", "diagram", "F-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("spec diagram on unknown feature succeeded, want error")
	}
	if !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("error %q does not name the missing feature", err)
	}
}

// TestSpecDiagramNonSequenceErrors fails when the provider returns text that is
// not a Mermaid sequence diagram (the ErrNotMermaidSequence path).
func TestSpecDiagramNonSequenceErrors(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"flowchart TD\n  A-->B"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	_, err := execCerne(t, "spec", "diagram", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("spec diagram with non-sequence output succeeded, want error")
	}
}

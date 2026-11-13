package cli

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestPushRecordsDocAuthor (F-0375 crit. 1): the push records one
// feature.doc_edited per feature document it sends — in the same commit as
// the documents, with the writer and no rev (not an amend).
func TestPushRecordsDocAuthor(t *testing.T) {
	projDir, origin, _, specPath := pulledProject(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_SESSION", "")
	t.Setenv("CERNE_PROVIDER", "openai")
	t.Setenv("CERNE_MODEL", "gpt-x")
	mustWrite(t, specPath, "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nDividir compras em parcelas, editado.\n")
	mustWrite(t, filepath.Join(filepath.Dir(specPath), "plan.md"), "---\ncerne_id: F-0001\n---\n\n# Plano\n\nPassos.\n")
	start := time.Now().UTC()
	if out, err := execCerne(t, "push", "--dir", projDir); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatal(err)
	}
	evs, err := store.ReadEvents(verify)
	if err != nil {
		t.Fatal(err)
	}
	var docs []string
	for _, ev := range evs {
		if ev.EntityID != "F-0001" || ev.Type != "feature.doc_edited" || ev.At.Before(start) {
			continue
		}
		p := ev.Payload
		if p["provider"] != "openai" || p["model"] != "gpt-x" || p["source"] != "declared" || p["rev"] != "" {
			t.Fatalf("push event payload = %v, want declared openai/gpt-x without rev", p)
		}
		docs = append(docs, p["doc"])
	}
	slices.Sort(docs)
	if !slices.Equal(docs, []string{"plan", "spec"}) {
		t.Fatalf("doc_edited docs = %v, want one per pushed document", docs)
	}
	files, err := exec.Command("git", "-C", verify, "show", "--name-only", "--format=", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	got := string(files)
	for _, want := range []string{"content/features/F-0001/spec.md", "content/features/F-0001/plan.md", "events/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("push commit files = %q, missing %s (events go in the same commit)", got, want)
		}
	}
}

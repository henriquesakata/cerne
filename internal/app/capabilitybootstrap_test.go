package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/store"
)

func bootstrapFixture(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newFeatureBackend(t)
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Camada de issues", State: domain.StateImplemented},
		{ID: "F-0002", Title: "Board de issues", State: domain.StateInDevelopment},
		{ID: "F-0003", Title: "Rejeitada", State: domain.StateRejected},
	} {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	commitBackend(t, repoDir)
	return repoDir, projDir
}

const proposalReply = `{
  "tree": [
    {
      "title": "Studio",
      "children": [
        {"title": "Issues", "features": ["F-0001", "F-0002"]}
      ]
    }
  ]
}`

// TestCapabilityBootstrapProposal pins F-0188 criterion 4: the proposal lands
// as reviewable files under .docs/.capability-proposal/ and the backend is NOT
// touched; terminal features stay out of the prompt.
func TestCapabilityBootstrapProposal(t *testing.T) {
	repoDir, projDir := bootstrapFixture(t)
	fake := &llm.Fake{Reply: proposalReply}

	prop, err := GenerateCapabilityProposal(context.Background(), repoDir, projDir, fake)
	if err != nil {
		t.Fatalf("GenerateCapabilityProposal: %v", err)
	}
	if len(prop.Tree) != 1 || prop.Tree[0].Title != "Studio" {
		t.Fatalf("proposal = %+v", prop)
	}
	prompt := fake.Prompts[0]
	if !strings.Contains(prompt, "F-0001") || !strings.Contains(prompt, "F-0002") {
		t.Fatalf("prompt missing live features:\n%s", prompt)
	}
	if strings.Contains(prompt, "F-0003") {
		t.Fatalf("prompt should not include terminal features:\n%s", prompt)
	}
	for _, name := range []string{"proposal.json", "proposal.md"} {
		if _, err := os.Stat(filepath.Join(projDir, ".docs", ".capability-proposal", name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	caps, err := store.LoadAllCapabilities(repoDir)
	if err != nil {
		t.Fatalf("LoadAllCapabilities: %v", err)
	}
	if len(caps) != 0 {
		t.Fatalf("backend touched by proposal: %+v", caps)
	}
}

// TestCapabilityBootstrapEmptyReply: an empty completion writes nothing.
func TestCapabilityBootstrapEmptyReply(t *testing.T) {
	repoDir, projDir := bootstrapFixture(t)
	fake := &llm.Fake{Reply: "  "}
	if _, err := GenerateCapabilityProposal(context.Background(), repoDir, projDir, fake); !errors.Is(err, ErrEmptyProposal) {
		t.Fatalf("err = %v, want ErrEmptyProposal", err)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".docs", ".capability-proposal")); !os.IsNotExist(err) {
		t.Fatalf("proposal dir should not exist on empty reply")
	}
}

// TestCapabilityBootstrapApplyIdempotent pins F-0188 criterion 5: --apply
// creates the tree + links under the capability-bootstrap actor, and applying
// again neither duplicates capabilities nor re-links features.
func TestCapabilityBootstrapApplyIdempotent(t *testing.T) {
	repoDir, projDir := bootstrapFixture(t)
	fake := &llm.Fake{Reply: proposalReply}
	if _, err := GenerateCapabilityProposal(context.Background(), repoDir, projDir, fake); err != nil {
		t.Fatalf("GenerateCapabilityProposal: %v", err)
	}

	res, err := ApplyCapabilityProposal(repoDir, projDir)
	if err != nil {
		t.Fatalf("ApplyCapabilityProposal: %v", err)
	}
	if res.Created != 2 || res.Linked != 2 {
		t.Fatalf("apply = %+v, want 2 created, 2 linked", res)
	}
	caps, _ := store.LoadAllCapabilities(repoDir)
	if len(caps) != 2 {
		t.Fatalf("capabilities = %+v", caps)
	}
	var issues store.Capability
	for _, c := range caps {
		if c.Title == "Issues" {
			issues = c
		}
	}
	if issues.Parent == "" {
		t.Fatalf("child capability lost its parent: %+v", caps)
	}
	f, _ := store.LoadFeature(repoDir, "F-0001")
	if len(f.Capabilities) != 1 || f.Capabilities[0] != issues.ID {
		t.Fatalf("F-0001 capabilities = %v, want [%s]", f.Capabilities, issues.ID)
	}

	again, err := ApplyCapabilityProposal(repoDir, projDir)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if again.Created != 0 || again.Linked != 0 {
		t.Fatalf("re-apply = %+v, want 0 created, 0 linked", again)
	}
	caps, _ = store.LoadAllCapabilities(repoDir)
	if len(caps) != 2 {
		t.Fatalf("capabilities after re-apply = %+v", caps)
	}
}

// TestCapabilityBootstrapApplyWithoutProposal: a clear error, nothing written.
func TestCapabilityBootstrapApplyWithoutProposal(t *testing.T) {
	repoDir, projDir := bootstrapFixture(t)
	if _, err := ApplyCapabilityProposal(repoDir, projDir); !errors.Is(err, ErrNoProposal) {
		t.Fatalf("err = %v, want ErrNoProposal", err)
	}
}

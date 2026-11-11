package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedDeliveryFeature saves one feature in the given state, with real
// lifecycle docs, and pushes the seed to origin.
func seedDeliveryFeature(t *testing.T, repoDir, id string, state domain.FeatureState, docs ...string) {
	t.Helper()
	now := time.Now().UTC()
	f := store.Feature{ID: id, Title: "Feature " + id, State: state, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	for _, doc := range docs {
		path := filepath.Join(repoDir, "content", "features", id, doc)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		body := "# Doc\n\n## Conteúdo real\n\nCorpo real do documento, não um stub.\n"
		if doc == "result.md" {
			body += "\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória: \"fixture\"\n" // complete result gate (F-0303)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", doc, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
}

// TestCompleteWithDeliveryFlags (F-0179 crit. 1, 2): `feature complete
// --commit --mr` persists both links on the entity.
func TestCompleteWithDeliveryFlags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	out, err := execCerne(t, "feature", "complete", "F-0001",
		"--commit", "https://gitlab.example/repo/-/commit/abc1234",
		"--mr", "https://gitlab.example/repo/-/merge_requests/7",
		"--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.DeliveryCommitURL != "https://gitlab.example/repo/-/commit/abc1234" ||
		f.DeliveryMRURL != "https://gitlab.example/repo/-/merge_requests/7" {
		t.Fatalf("delivery = %q / %q", f.DeliveryCommitURL, f.DeliveryMRURL)
	}
}

// TestCompleteDeliveryFlagsRejectedOnOtherActions: --commit/--mr belong to
// complete only.
func TestCompleteDeliveryFlagsRejectedOnOtherActions(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")

	out, err := execCerne(t, "feature", "reject", "F-0001", "--reason", "x",
		"--commit", "abc1234", "--dir", projDir)
	if err == nil {
		t.Fatalf("reject with --commit must fail\n%s", out)
	}
}

// TestCompleteBareSHAWithoutTemplateFails (crit. 4): fail fast, feature stays
// in_development.
func TestCompleteBareSHAWithoutTemplateFails(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	out, err := execCerne(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--dir", projDir)
	if err == nil {
		t.Fatalf("bare SHA without template must fail\n%s", out)
	}
	f, _ := store.LoadFeature(repoDir, "F-0001")
	if f.State != domain.StateInDevelopment {
		t.Fatalf("state = %s, want in_development untouched", f.State)
	}
}

// TestFeatureDeliverPostHoc (crit. 7): post-hoc capture on an implemented
// feature, resolving a bare SHA through the .cernerc template.
func TestFeatureDeliverPostHoc(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cernerc := filepath.Join(projDir, ".cernerc")
	data, err := os.ReadFile(cernerc)
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	data = append(data, []byte("\n[delivery]\ncommit_url = \"https://gitlab.example/repo/-/commit/{sha}\"\n")...)
	if err := os.WriteFile(cernerc, data, 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateImplemented)

	out, err := execCerne(t, "feature", "deliver", "F-0001",
		"--commit", "def5678",
		"--mr", "https://gitlab.example/repo/-/merge_requests/9",
		"--dir", projDir)
	if err != nil {
		t.Fatalf("deliver: %v\n%s", err, out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.DeliveryCommitURL != "https://gitlab.example/repo/-/commit/def5678" ||
		f.DeliveryMRURL != "https://gitlab.example/repo/-/merge_requests/9" {
		t.Fatalf("delivery = %q / %q", f.DeliveryCommitURL, f.DeliveryMRURL)
	}
	if f.State != domain.StateImplemented {
		t.Fatalf("state = %s, want implemented untouched", f.State)
	}
}

// TestFeatureDeliverTerminalFails (crit. 8).
func TestFeatureDeliverTerminalFails(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateRejected)

	out, err := execCerne(t, "feature", "deliver", "F-0001",
		"--mr", "https://gitlab.example/repo/-/merge_requests/9", "--dir", projDir)
	if err == nil {
		t.Fatalf("deliver on rejected must fail\n%s", out)
	}
}

// TestShowRendersDelivery (crit. 9): `cerne show` prints the delivery links.
func TestShowRendersDelivery(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Com entrega", State: domain.StateImplemented,
		DeliveryCommitURL: "https://gitlab.example/repo/-/commit/abc1234",
		DeliveryMRURL:     "https://gitlab.example/repo/-/merge_requests/7",
		CreatedAt:         now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "https://gitlab.example/repo/-/commit/abc1234") ||
		!strings.Contains(out, "https://gitlab.example/repo/-/merge_requests/7") {
		t.Fatalf("show output missing delivery links:\n%s", out)
	}
}

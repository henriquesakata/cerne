package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// staleLine is what show and next print for a stale approval (F-0318).
const staleLine = "approval: stale — spec changed since approval (re-approve: cerne feature approve F-0001)"

// staleFixture approves F-0001 and F-0002 through the CLI, then edits
// F-0001's spec: F-0001's approval is stale, F-0002's is not.
func staleFixture(t *testing.T) (projDir string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	for i, id := range []string{"F-0001", "F-0002"} {
		newFeature(t, projDir, "Feature "+id, i+1)
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real, versão aprovada."))
		commitBackend(t, repoDir)
		if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
			t.Fatalf("approve %s: %v\n%s", id, err, out)
		}
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real, com um critério novo."))
	commitBackend(t, repoDir)
	return projDir
}

// TestShowStaleApproval (F-0318 crit. 10): show exposes a stale approval in
// the text, with the re-approve command, and as approval_stale in --json.
func TestShowStaleApproval(t *testing.T) {
	projDir := staleFixture(t)

	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, staleLine) {
		t.Fatalf("show F-0001 (%v) missing %q:\n%s", err, staleLine, out)
	}
	if out, _ := execCerne(t, "show", "F-0002", "--dir", projDir); strings.Contains(out, "approval: stale") {
		t.Fatalf("show F-0002 reports a stale approval:\n%s", out)
	}

	for id, want := range map[string]bool{"F-0001": true, "F-0002": false} {
		out, err := execCerne(t, "show", id, "--json", "--dir", projDir)
		if err != nil {
			t.Fatalf("show %s --json: %v", id, err)
		}
		var got struct {
			ApprovalStale *bool `json:"approval_stale"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || got.ApprovalStale == nil || *got.ApprovalStale != want {
			t.Fatalf("show %s --json approval_stale = %v (%v), want %v:\n%s", id, got.ApprovalStale, err, want, out)
		}
	}
}

// TestNextStaleApproval (F-0318 crit. 10): next flags the stale approval on
// the feature's own line in READY TO START — not again among the debts — and
// as approval_stale in --json, so the agent sees the block before the start.
func TestNextStaleApproval(t *testing.T) {
	projDir := staleFixture(t)

	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	ready := out[strings.Index(out, "READY TO START"):]
	if i := strings.Index(ready, "\n\n"); i > 0 {
		ready = ready[:i]
	}
	if !strings.Contains(ready, "F-0001") || !strings.Contains(ready, staleLine) || strings.Count(ready, "approval: stale") != 1 {
		t.Fatalf("READY TO START does not flag F-0001 (only):\n%s", out)
	}
	if strings.Contains(out, "SPEC-CHANGED-AFTER-APPROVAL") {
		t.Fatalf("the stale approval is repeated among the debts:\n%s", out)
	}

	out, err = execCerne(t, "next", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("next --json: %v", err)
	}
	var got struct {
		Groups []struct {
			State    string `json:"state"`
			Features []struct {
				ID            string `json:"id"`
				ApprovalStale bool   `json:"approval_stale"`
			} `json:"features"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("next --json: %v\n%s", err, out)
	}
	stale := map[string]bool{}
	for _, g := range got.Groups {
		if g.State != "approved" {
			continue
		}
		for _, f := range g.Features {
			stale[f.ID] = f.ApprovalStale
		}
	}
	if len(stale) != 2 || !stale["F-0001"] || stale["F-0002"] {
		t.Fatalf("approved group approval_stale = %v, want F-0001 true, F-0002 false:\n%s", stale, out)
	}
}

// TestFeatureSpecStalesApproval (F-0318 crit. 2): rewriting an approved spec
// through the CLI says, in the same output, that the approval went stale and
// how it is renewed.
func TestFeatureSpecStalesApproval(t *testing.T) {
	projDir := staleFixture(t)
	pipeStdinBody(t, "## Problema\n\nReescrita depois da aprovação.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n")
	out, err := execCerne(t, "feature", "spec", "F-0002", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature spec on approved: %v\n%s", err, out)
	}
	if want := strings.ReplaceAll(staleLine, "F-0001", "F-0002"); !strings.Contains(out, want) {
		t.Fatalf("feature spec output missing %q:\n%s", want, out)
	}
}

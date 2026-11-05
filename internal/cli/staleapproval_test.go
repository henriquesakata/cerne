package cli

import (
	"encoding/json"
	"fmt"
	"os"
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

// section returns next's text block that opens with header, up to the blank
// line closing it ("" when absent).
func section(out, header string) string {
	i := strings.Index(out, header)
	if i < 0 {
		return ""
	}
	block := out[i:]
	if j := strings.Index(block, "\n\n"); j > 0 {
		block = block[:j]
	}
	return block
}

// TestNextStaleApproval (F-0318 crit. 10): next never lists a stale approval
// under READY TO START, whose action is the start it refuses. It gets its own
// section, with the human gates, naming the re-approve, and keeps the
// per-feature stale line — not repeated among the debts. --json mirrors it as
// its own queue, each feature carrying approval_stale.
func TestNextStaleApproval(t *testing.T) {
	projDir := staleFixture(t)

	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	ready := section(out, "READY TO START")
	if !strings.Contains(ready, "READY TO START (1)") || !strings.Contains(ready, "F-0002") || strings.Contains(ready, "F-0001") {
		t.Fatalf("READY TO START must hold F-0002 only:\n%s", out)
	}
	waiting := section(out, "WAITING FOR RE-APPROVAL")
	if !strings.Contains(waiting, "WAITING FOR RE-APPROVAL (1)") || !strings.Contains(waiting, "cerne feature approve <id>") ||
		!strings.Contains(waiting, "F-0001") || !strings.Contains(waiting, staleLine) {
		t.Fatalf("no WAITING FOR RE-APPROVAL section holding F-0001 with its stale line:\n%s", out)
	}
	if strings.Index(out, "READY TO START") > strings.Index(out, "WAITING FOR RE-APPROVAL") {
		t.Fatalf("the re-approval section belongs with the human gates, after READY TO START:\n%s", out)
	}
	if strings.Count(out, "approval: stale") != 1 || strings.Contains(out, "SPEC-CHANGED-AFTER-APPROVAL") {
		t.Fatalf("the stale approval must be said once, on its line:\n%s", out)
	}

	out, err = execCerne(t, "next", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("next --json: %v", err)
	}
	var got struct {
		Groups []struct {
			Queue    string `json:"queue"`
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
	byQueue := map[string][]string{}
	for _, g := range got.Groups {
		for _, f := range g.Features {
			byQueue[g.Queue] = append(byQueue[g.Queue], fmt.Sprintf("%s:%s:%v", f.ID, g.State, f.ApprovalStale))
		}
	}
	if fmt.Sprint(byQueue["ready_to_start"]) != "[F-0002:approved:false]" || fmt.Sprint(byQueue["awaiting_reapproval"]) != "[F-0001:approved:true]" {
		t.Fatalf("queues = %v, want ready_to_start [F-0002] and awaiting_reapproval [F-0001 stale]:\n%s", byQueue, out)
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

// TestPushSaysWhenItStalesAnApproval (F-0318 crit. 2): the push that leaves an
// approved feature stale says so after the Sent line — the reason, the human
// re-approve, the blocked start. Only a push that changes the content does: a
// CRLF/format-only push and a revert to the approved text print nothing.
func TestPushSaysWhenItStalesAnApproval(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	if out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	approved, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	const line = "F-0001: approval is now stale — spec changed since approval; a human re-approves with 'cerne feature approve F-0001' (start is blocked until then)"
	push := func(body string) string {
		t.Helper()
		mustWrite(t, specPath, body)
		out, err := execCerne(t, "push", "--dir", projDir)
		if err != nil || !strings.Contains(out, "Sent: 1 document(s)") {
			t.Fatalf("push: %v\n%s", err, out)
		}
		return out
	}

	if out := push(strings.ReplaceAll(string(approved), "\n", "\r\n")); strings.Contains(out, "approval is now stale") {
		t.Fatalf("a CRLF-only push reported a stale approval:\n%s", out)
	}
	out := push(string(approved) + "\nCritério novo depois da aprovação.\n")
	if !strings.Contains(out, line) || strings.Index(out, line) < strings.Index(out, "Sent:") {
		t.Fatalf("content push: want, after the Sent line, %q:\n%s", line, out)
	}
	if out := push(string(approved)); strings.Contains(out, "approval is now stale") {
		t.Fatalf("a revert to the approved text reported a stale approval:\n%s", out)
	}
}

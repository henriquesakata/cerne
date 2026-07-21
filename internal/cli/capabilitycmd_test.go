package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestCapabilityNewAndList pins F-0188 criteria 1 and 3: new mints C-NNNN
// (active) with an optional validated parent; list renders the hierarchy
// (--tree) and machine output (--json) with linked-feature counts.
func TestCapabilityNewAndList(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	out, err := execCerne(t, "capability", "new", "Studio", "--dir", projDir)
	if err != nil {
		t.Fatalf("capability new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "C-0001") {
		t.Fatalf("output missing C-0001:\n%s", out)
	}
	if out, err = execCerne(t, "capability", "new", "Issues", "--parent", "C-0001", "--dir", projDir); err != nil {
		t.Fatalf("capability new child: %v\noutput: %s", err, out)
	}

	c, err := store.LoadCapability(repoDir, "C-0002")
	if err != nil {
		t.Fatalf("LoadCapability: %v", err)
	}
	if c.Parent != "C-0001" || c.State != domain.CapabilityActive {
		t.Fatalf("child = %+v, want parent C-0001 active", c)
	}

	// Uma feature linkada aparece na contagem do list.
	if out, err = execCerne(t, "feature", "new", "Relatar bugs", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	if out, err = execCerne(t, "feature", "capability", "F-0001", "C-0002", "--dir", projDir); err != nil {
		t.Fatalf("feature capability: %v\noutput: %s", err, out)
	}

	tree, err := execCerne(t, "capability", "list", "--tree", "--dir", projDir)
	if err != nil {
		t.Fatalf("capability list --tree: %v\noutput: %s", err, tree)
	}
	if !strings.Contains(tree, "C-0001") || !strings.Contains(tree, "  C-0002") {
		t.Fatalf("tree output missing indented child:\n%s", tree)
	}
	if !strings.Contains(tree, "1 feature") {
		t.Fatalf("tree output missing linked-feature count:\n%s", tree)
	}

	jout, err := execCerne(t, "capability", "list", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("capability list --json: %v\noutput: %s", err, jout)
	}
	var payload struct {
		Capabilities []struct {
			ID       string `json:"id"`
			Parent   string `json:"parent"`
			Features int    `json:"features"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(jout), &payload); err != nil {
		t.Fatalf("list --json parse: %v\n%s", err, jout)
	}
	if len(payload.Capabilities) != 2 || payload.Capabilities[1].Features != 1 {
		t.Fatalf("json payload = %+v", payload)
	}
}

// TestCapabilityNewUnknownParent: parent inexistente aborta sem escrever nada.
func TestCapabilityNewUnknownParent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "capability", "new", "Orfa", "--parent", "C-9999", "--dir", projDir); err == nil {
		t.Fatalf("capability new with unknown parent should fail")
	}
	all, err := store.LoadAllCapabilities(repoDir)
	if err != nil {
		t.Fatalf("LoadAllCapabilities: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("capabilities written on failed new: %+v", all)
	}
}

// TestCapabilityRetire pins F-0188 criterion 6: retire only when the node has
// no active children and no linked features.
func TestCapabilityRetire(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mustExec(t, projDir, "capability", "new", "Studio")
	mustExec(t, projDir, "capability", "new", "Issues", "--parent", "C-0001")

	// Com filho ativo → recusa.
	if _, err := execCerne(t, "capability", "retire", "C-0001", "--reason", "x", "--dir", projDir); err == nil {
		t.Fatalf("retire with active child should fail")
	}

	// Com feature linkada → recusa.
	mustExec(t, projDir, "feature", "new", "Relatar bugs", "--context", "ctx")
	mustExec(t, projDir, "feature", "capability", "F-0001", "C-0002")
	if _, err := execCerne(t, "capability", "retire", "C-0002", "--reason", "x", "--dir", projDir); err == nil {
		t.Fatalf("retire with linked feature should fail")
	}

	// Desfaz o vínculo → retire da folha passa; depois o pai também.
	mustExec(t, projDir, "feature", "capability", "F-0001", "--clear")
	mustExec(t, projDir, "capability", "retire", "C-0002", "--reason", "reorganizada")
	c, err := store.LoadCapability(repoDir, "C-0002")
	if err != nil {
		t.Fatalf("LoadCapability: %v", err)
	}
	if c.State != domain.CapabilitySuperseded || c.Reason != "reorganizada" {
		t.Fatalf("retired = %+v", c)
	}
	mustExec(t, projDir, "capability", "retire", "C-0001", "--reason", "fim")
}

// TestFeatureCapabilityLink pins F-0188 criterion 2: the link is a validated
// list on the feature; unknown ids abort without writing; --clear removes it.
func TestFeatureCapabilityLink(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mustExec(t, projDir, "feature", "new", "Relatar bugs", "--context", "ctx")
	mustExec(t, projDir, "capability", "new", "Studio")
	mustExec(t, projDir, "capability", "new", "Issues", "--parent", "C-0001")

	if _, err := execCerne(t, "feature", "capability", "F-0001", "C-0001,C-9999", "--dir", projDir); err == nil {
		t.Fatalf("link with unknown capability should fail")
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if len(f.Capabilities) != 0 {
		t.Fatalf("capabilities written on failed link: %v", f.Capabilities)
	}

	mustExec(t, projDir, "feature", "capability", "F-0001", "C-0001,C-0002")
	f, _ = store.LoadFeature(repoDir, "F-0001")
	if len(f.Capabilities) != 2 || f.Capabilities[0] != "C-0001" || f.Capabilities[1] != "C-0002" {
		t.Fatalf("capabilities = %v, want [C-0001 C-0002]", f.Capabilities)
	}

	mustExec(t, projDir, "feature", "capability", "F-0001", "--clear")
	f, _ = store.LoadFeature(repoDir, "F-0001")
	if len(f.Capabilities) != 0 {
		t.Fatalf("capabilities after --clear = %v, want empty", f.Capabilities)
	}
}

// TestShowCapability: the generic reader resolves the new kind by id.
func TestShowCapability(t *testing.T) {
	projDir, _, _ := setupProject(t)
	mustExec(t, projDir, "capability", "new", "Studio")
	out, err := execCerne(t, "show", "C-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show C-0001: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Studio") || !strings.Contains(out, "active") {
		t.Fatalf("show output missing title/state:\n%s", out)
	}
}

// TestCapabilityBootstrapNeedsLLM pins F-0188 criterion 4 (error half): with
// no callable [llm] mode configured, bootstrap fails with a clear pointer.
func TestCapabilityBootstrapNeedsLLM(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "capability", "bootstrap", "--dir", projDir)
	if err == nil {
		t.Fatalf("bootstrap without [llm] should fail\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "[llm]") {
		t.Fatalf("error should point at the [llm] config, got: %v", err)
	}
}

// mustExec runs a cerne command against projDir and fails the test on error.
func mustExec(t *testing.T, projDir string, args ...string) {
	t.Helper()
	full := append(args, "--dir", projDir)
	if out, err := execCerne(t, full...); err != nil {
		t.Fatalf("cerne %s: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
}

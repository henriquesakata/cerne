package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestShowFeature(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}

	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show F-0001: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"F-0001", "feature", "[candidate]", "Login com Google"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show F-0001 output missing %q:\n%s", want, out)
		}
	}
}

func TestShowEpicAndTask(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "epic", "new", "Épico teste", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("epic new: %v", err)
	}
	// The approve bar gate (F-0545) also covers epic specs; the creation-seeded
	// spec has no bar, so give the epic a real one.
	writeDoc(t, repoDir, "E-0001", "spec.md", realDoc("E-0001", "PRD real do épico."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", "E-0001", "--dir", projDir); err != nil {
		t.Fatalf("feature approve E-0001: %v", err)
	}
	if _, err := execCerne(t, "task", "new", "Passo teste", "--parent", "E-0001", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("task new: %v", err)
	}

	out, err := execCerne(t, "show", "E-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show E-0001: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "E-0001") || !strings.Contains(out, "feature") {
		t.Fatalf("show E-0001 output missing expected fields:\n%s", out)
	}

	out, err = execCerne(t, "show", "T-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show T-0001: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "T-0001") || !strings.Contains(out, "feature") {
		t.Fatalf("show T-0001 output missing expected fields:\n%s", out)
	}

	if _, err := execCerne(t, "show", "T-9999", "--dir", projDir); err == nil {
		t.Fatal("show T-9999: expected not-found error, got none")
	}
}

func TestShowNoteWithBody(t *testing.T) {
	projDir, _, _ := setupProject(t)
	body := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(body, []byte("go-git ignora ~/.ssh/config; use a key-file fallback.\n"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if _, err := execCerne(t, "note", "new", "SSH gotcha", "--file", body, "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}

	out, err := execCerne(t, "show", "N-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show N-0001: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "N-0001") || !strings.Contains(out, "note") {
		t.Fatalf("show N-0001 missing header:\n%s", out)
	}
	if !strings.Contains(out, "key-file fallback") {
		t.Fatalf("show N-0001 missing body content:\n%s", out)
	}
}

func TestShowJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}

	out, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v\noutput: %s", err, out)
	}
	var got struct {
		Kind   string `json:"kind"`
		Entity struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			State string `json:"state"`
		} `json:"entity"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if got.Kind != "feature" || got.Entity.ID != "F-0001" || got.Entity.State != "candidate" {
		t.Fatalf("json = %+v, want feature/F-0001/candidate", got)
	}
}

// seedFeatureDocs writes real spec/plan/result bodies straight into the backend
// clone, standing in for the LLM authoring layer.
func seedFeatureDocs(t *testing.T, repoDir, id string, docs map[string]string) {
	t.Helper()
	files := map[string]string{}
	for name, body := range docs {
		files[filepath.ToSlash(filepath.Join("content", "features", id, name+".md"))] = body
	}
	writeTree(t, repoDir, files)
}

func TestShowFeatureDoc(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	seedFeatureDocs(t, repoDir, "F-0001", map[string]string{
		"spec":   "# Spec\n\nO que o login precisa fazer.\n",
		"result": "# Result\n\nDébito: falta rate limit no callback.\n",
	})

	out, err := execCerne(t, "show", "F-0001", "--doc", "result", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --doc result: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "falta rate limit") {
		t.Fatalf("show --doc result missing body:\n%s", out)
	}
	if strings.Contains(out, "O que o login precisa fazer") {
		t.Fatalf("show --doc result leaked the spec:\n%s", out)
	}
	if strings.Contains(out, "[candidate]") || strings.Contains(out, "## result") {
		t.Fatalf("show --doc result should print the raw doc, got:\n%s", out)
	}
}

func TestShowFeatureDocJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	seedFeatureDocs(t, repoDir, "F-0001", map[string]string{
		"spec":   "# Spec\n\nO que o login precisa fazer.\n",
		"result": "# Result\n\nDébito: falta rate limit no callback.\n",
	})

	out, err := execCerne(t, "show", "F-0001", "--doc", "result", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --doc result --json: %v\noutput: %s", err, out)
	}
	var got struct {
		Kind    string `json:"kind"`
		Content []struct {
			Name string `json:"name"`
			Body string `json:"body"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if len(got.Content) != 1 || got.Content[0].Name != "result" || !strings.Contains(got.Content[0].Body, "rate limit") {
		t.Fatalf("json content = %+v, want only the result doc", got.Content)
	}
}

func TestShowFeatureDocMissing(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	if out, err := execCerne(t, "show", "F-0001", "--doc", "result", "--dir", projDir); err == nil {
		t.Fatalf("show --doc result succeeded on a feature with no result:\n%s", out)
	}
}

func TestShowDocRejectsNonFeature(t *testing.T) {
	projDir, _, _ := setupProject(t)
	body := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(body, []byte("nota de teste\n"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if _, err := execCerne(t, "note", "new", "SSH gotcha", "--file", body, "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	if out, err := execCerne(t, "show", "N-0001", "--doc", "spec", "--dir", projDir); err == nil {
		t.Fatalf("show --doc on a note succeeded:\n%s", out)
	}
}

func TestShowDocRejectsUnknownName(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	if out, err := execCerne(t, "show", "F-0001", "--doc", "resumo", "--dir", projDir); err == nil {
		t.Fatalf("show --doc resumo succeeded:\n%s", out)
	}
}

func TestShowUnknownPrefix(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "show", "Z-0001", "--dir", projDir); err == nil {
		t.Fatal("show Z-0001 succeeded, want unknown-prefix error")
	}
}

func TestShowNotFound(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "show", "F-9999", "--dir", projDir); err == nil {
		t.Fatal("show F-9999 succeeded, want not-found error")
	}
}

// F-0885 crit. 8 (and the CLI of crit. 1 and 3): promote of several pendings
// takes --title and prints one line per pending; link points one more at the
// same feature; show of that feature lists, in a section of its own, every
// pending it pays with the state of each — not the one it never paid, and the
// origin keeps only its own open-debts section.
func TestShowFeatureListsPendingsItPays(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	pendingNew(t, projDir, "Débito A", "--from", "F-0001")
	pendingNew(t, projDir, "Débito B", "--from", "F-0001")
	pendingNew(t, projDir, "Débito ligado depois", "--from", "F-0001")
	pendingNew(t, projDir, "Débito de fora", "--from", "F-0001")
	if _, err := execCerne(t, "pending", "promote", "P-0001", "P-0002", "--as", "feature", "--dir", projDir); !errors.Is(err, app.ErrPendingGroupTitle) {
		t.Fatalf("promote of two without --title: err = %v, want ErrPendingGroupTitle", err)
	}
	out, err := execCerne(t, "pending", "promote", "P-0001", "P-0002", "--as", "feature", "--title", "Pagar A e B", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001: open -> promoted (F-0002)\nP-0002: open -> promoted (F-0002)") {
		t.Fatalf("promote of two: %v\n%s", err, out)
	}
	if f, err := store.LoadFeature(repoDir, "F-0002"); err != nil || f.Title != "Pagar A e B" {
		t.Fatalf("F-0002 = %+v, %v; want the --title", f, err)
	}
	out, err = execCerne(t, "pending", "link", "P-0003", "F-0002", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0003: open -> promoted (F-0002)") {
		t.Fatalf("link: %v\n%s", err, out)
	}
	// A pending already paid keeps pointing at the feature: its state shows.
	p, err := store.LoadPending(repoDir, "P-0003")
	if err != nil {
		t.Fatal(err)
	}
	p.State = domain.PendingResolved
	if err := store.SavePending(repoDir, p); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	out, err = execCerne(t, "show", "F-0002", "--dir", projDir)
	if err != nil {
		t.Fatalf("show F-0002: %v\n%s", err, out)
	}
	want := "## pays\nP-0001  [promoted]  Débito A\nP-0002  [promoted]  Débito B\nP-0003  [resolved]  Débito ligado depois\n"
	if !strings.Contains(out, want) || strings.Contains(out, "P-0004") {
		t.Fatalf("show F-0002 must list the three it pays, with their states, and not P-0004:\n%s", out)
	}
	out, err = execCerne(t, "show", "F-0002", "--json", "--dir", projDir)
	if err != nil || !strings.Contains(out, `"name": "pays"`) {
		t.Fatalf("show --json lacks the pays section: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || strings.Contains(out, "## pays") || !strings.Contains(out, "## pending\nP-0004  Débito de fora") {
		t.Fatalf("show F-0001 must keep only its open debt P-0004, with no pays section: %v\n%s", err, out)
	}
}

// F-0885 crit. 8 (and the CLI of crit. 4): show of a linked pending names the
// paying feature with its state, text and json; unlink brings it back to open
// and the forward link goes.
func TestShowPendingNamesTargetState(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newOriginFeature(t, projDir, "Origem")
	newOriginFeature(t, projDir, "Quem paga")
	pendingNew(t, projDir, "Débito", "--from", "F-0001")
	if out, err := execCerne(t, "pending", "link", "P-0001", "F-0002", "--dir", projDir); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "promoted to: F-0002 (candidate)\n") {
		t.Fatalf("show P-0001 lacks the target's state: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "P-0001", "--json", "--dir", projDir)
	var doc struct {
		Entity struct {
			PromotedTo      string `json:"promoted_to"`
			PromotedToState string `json:"promoted_to_state"`
		} `json:"entity"`
	}
	if err != nil || json.Unmarshal([]byte(out), &doc) != nil || doc.Entity.PromotedTo != "F-0002" || doc.Entity.PromotedToState != "candidate" {
		t.Fatalf("show --json = %+v, %v; want promoted_to F-0002 (candidate)\n%s", doc, err, out)
	}
	out, err = execCerne(t, "pending", "unlink", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "P-0001: promoted -> open") {
		t.Fatalf("unlink: %v\n%s", err, out)
	}
	out, err = execCerne(t, "show", "P-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "[open]") || strings.Contains(out, "promoted to:") {
		t.Fatalf("show after unlink: %v\n%s", err, out)
	}
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

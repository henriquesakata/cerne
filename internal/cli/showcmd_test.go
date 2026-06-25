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
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--dir", projDir); err != nil {
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
	if _, err := execCerne(t, "feature", "new", "Login com Google", "--dir", projDir); err != nil {
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

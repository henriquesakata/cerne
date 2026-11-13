package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/ui"
)

// TestEmbeddedUICarriesNoExcalidraw pins F-0400 criteria 1 and 2: the
// embedded UI bundle and ui/package.json name neither Excalidraw nor the
// esm.sh CDN it fetched fonts from.
func TestEmbeddedUICarriesNoExcalidraw(t *testing.T) {
	banned := []string{"@excalidraw", "esm.sh"}
	pkg, err := os.ReadFile(filepath.Join("..", "..", "ui", "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	for _, b := range banned {
		if strings.Contains(string(pkg), b) {
			t.Errorf("ui/package.json mentions %q", b)
		}
	}
	err = fs.WalkDir(ui.Dist, "dist", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := fs.ReadFile(ui.Dist, p)
		if rerr != nil {
			return rerr
		}
		for _, b := range banned {
			if strings.Contains(string(data), b) {
				t.Errorf("embedded %s mentions %q", p, b)
			}
		}
		if strings.Contains(strings.ToLower(p), "excalidraw") {
			t.Errorf("embedded file name %s carries Excalidraw", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded dist: %v", err)
	}
}

// TestArtifactPutRefusesExcalidraw pins criterion 7: saving .excalidraw is a
// 400 whose body names the reason; .mmd is still accepted.
func TestArtifactPutRefusesExcalidraw(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeatureForArtifacts(t, repoDir)
	h := New(repoDir, projDir, "")
	var got map[string]any
	rec := doJSON(t, h, http.MethodPut, "/api/artifacts/F-0001/seq.excalidraw", sceneA, &got)
	want := "excalidraw artifacts are no longer editable; only .mmd can be saved"
	if rec.Code != http.StatusBadRequest || got["error"] != want {
		t.Fatalf("status = %d error = %v, want 400 %q", rec.Code, got["error"], want)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "F-0001", "seq.excalidraw")); err == nil {
		t.Fatalf("refused artifact was written")
	}
	var ok map[string]bool
	rec = doJSON(t, h, http.MethodPut, "/api/artifacts/F-0001/seq.mmd", "sequenceDiagram\n  A->>B: hi", &ok)
	if rec.Code != http.StatusOK || !ok["ok"] {
		t.Fatalf(".mmd status = %d body = %s, want 200", rec.Code, rec.Body.String())
	}
}

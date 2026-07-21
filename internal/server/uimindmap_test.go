package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestUIMultiProjectInfoMindmapFlag pins F-0187 criterion 4: GET /api/projects
// carries the per-project [ui] mindmap flag — false when the project's
// .cernerc disables the tab, true otherwise (absent config = default on).
func TestUIMultiProjectInfoMindmapFlag(t *testing.T) {
	repoA, projA, _ := newGitBackendNoClean(t)
	repoB, projB, _ := newGitBackendNoClean(t)
	cernerc := "project_id = \"proj-b\"\nbackend = \"x\"\ndefault_scope = \"\"\n[ui]\nmindmap = false\n"
	if err := os.WriteFile(filepath.Join(projB, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	resolve := func(dir string) (string, string, string, string, error) {
		switch dir {
		case "A":
			return repoA, projA, "proj-a", "Projeto A", nil
		case "B":
			return repoB, projB, "proj-b", "Projeto B", nil
		}
		return "", "", "", "", fmt.Errorf("dir desconhecido: %s", dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler, err := NewUIMulti(ctx, resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	postJSON(t, srv.URL+"/api/projects", map[string]string{"dir": "B"}, nil)

	var list projectsList
	getJSON(t, srv.URL+"/api/projects", &list)
	if len(list.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(list.Projects))
	}
	byPID := map[string]projectInfo{}
	for _, p := range list.Projects {
		byPID[p.PID] = p
	}
	if !byPID["proj-a"].Mindmap {
		t.Errorf("proj-a (no .cernerc) mindmap = false, want true (default on)")
	}
	if byPID["proj-b"].Mindmap {
		t.Errorf("proj-b ([ui] mindmap = false) mindmap = true, want false")
	}
}

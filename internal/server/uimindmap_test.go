package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestUIMultiProjectInfoMindmapFlag pins F-0187 criterion 4, now served by the
// settings registry (F-0314 criterion 8): GET /api/projects carries each
// project's mindmap.enabled — false when its .cernerc disables the tab, true
// otherwise (absent config = default on).
func TestUIMultiProjectInfoMindmapFlag(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	byPID := mindmapFlags(t, "", "[mindmap]\nenabled = false\n")
	if !byPID["proj-a"].Mindmap {
		t.Errorf("proj-a (no .cernerc) mindmap = false, want true (default on)")
	}
	if byPID["proj-b"].Mindmap {
		t.Errorf("proj-b ([mindmap] enabled = false) mindmap = true, want false")
	}
}

// TestUIMultiProjectInfoMindmapFromHostScope: the gate follows the whole
// resolution — a host-scope off reaches a project that does not set the key,
// and a project-scope on beats it.
func TestUIMultiProjectInfoMindmapFromHostScope(t *testing.T) {
	hostDir := t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte("[mindmap]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	byPID := mindmapFlags(t, "\n", "[mindmap]\nenabled = true\n")
	if byPID["proj-a"].Mindmap {
		t.Errorf("proj-a (key unset, host off) mindmap = true, want false")
	}
	if !byPID["proj-b"].Mindmap {
		t.Errorf("proj-b (project on, host off) mindmap = false, want true")
	}
}

// TestUIMultiProjectInfoMindmapSurvivesBadHostFile (critic C2): a typo in the
// per-user file, on another key or as broken TOML, never brings back a tab
// the project turned off.
func TestUIMultiProjectInfoMindmapSurvivesBadHostFile(t *testing.T) {
	for _, host := range []string{"[wiki]\nenabled = \"no\"\n", "[x"} {
		hostDir := t.TempDir()
		t.Setenv("CERNE_CONFIG_DIR", hostDir)
		if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte(host), 0o644); err != nil {
			t.Fatal(err)
		}
		if byPID := mindmapFlags(t, "", "[mindmap]\nenabled = false\n"); byPID["proj-b"].Mindmap {
			t.Errorf("host %q: proj-b ([mindmap] enabled = false) mindmap = true, want false", host)
		}
	}
}

// mindmapFlags registers two projects in a local ui — each gets a .cernerc
// ending in its extra, or none when extra is "" — and returns GET
// /api/projects by pid.
func mindmapFlags(t *testing.T, extraA, extraB string) map[string]projectInfo {
	t.Helper()
	repoA, projA, _ := newGitBackendNoClean(t)
	repoB, projB, _ := newGitBackendNoClean(t)
	for _, p := range []struct{ dir, id, extra string }{{projA, "proj-a", extraA}, {projB, "proj-b", extraB}} {
		if p.extra == "" {
			continue
		}
		cernerc := "project_id = \"" + p.id + "\"\nbackend = \"x\"\ndefault_scope = \"\"\n" + p.extra
		if err := os.WriteFile(filepath.Join(p.dir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
			t.Fatalf("write .cernerc: %v", err)
		}
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
	return byPID
}

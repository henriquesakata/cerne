package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// gameFixture commits a small three-stage game plus files the tools must hide.
func gameFixture(t *testing.T) *sdk.ClientSession {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"game/src/stages/stage-one.js":            "export const stage = {\n  backdrop: 'forest-night',\n}\n",
		"game/src/stages/stage-two.js":            "export const stage = { backdrop: 'desert' }\n",
		"game/assets/backdrops/forest-night.json": "{\"sky\":\"FOREST-NIGHT\"}\n",
		"game/README.md":                          "readme\n",
		".env":                                    "SECRET=forest-night\n",
		".hidden/notes.txt":                       "forest-night\n",
		"keys/server.pem":                         "forest-night\n",
		"keys/credentials.json":                   "forest-night\n",
		"bin/blob.dat":                            "forest-night\x00binary",
		"big.txt":                                 strings.Repeat("forest-night\n", 400000),
		"many.txt":                                strings.Repeat("common line\n", 200),
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range [][]string{{"init", "-q"}, {"add", "-f", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "c"}} {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	// Changes outside the commit must stay invisible.
	if err := os.WriteFile(filepath.Join(dir, "game/untracked.js"), []byte("forest-night\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tree, err := gitback.ProjectTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := NewProjectFileServer("test", tree).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "c", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callFiles(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return err.Error(), true
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: %+v", tool, res)
	}
	return res.Content[0].(*sdk.TextContent).Text, res.IsError
}

func TestSearchFilesFindsContent(t *testing.T) {
	cs := gameFixture(t)
	out, isErr := callFiles(t, cs, "search_files", map[string]any{"query": "FOREST-night"})
	if isErr {
		t.Fatal(out)
	}
	for _, want := range []string{"game/src/stages/stage-one.js:2:", "backdrop: 'forest-night'", "game/assets/backdrops/forest-night.json:1:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, hidden := range []string{".env", ".hidden", ".pem", "credentials", "blob.dat", "big.txt", "untracked", "stage-two"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%q must not appear:\n%s", hidden, out)
		}
	}
	out, _ = callFiles(t, cs, "search_files", map[string]any{"query": "forest-night", "path_glob": "game/src/**"})
	if !strings.Contains(out, "stage-one.js") || strings.Contains(out, "backdrops") {
		t.Errorf("path_glob not applied:\n%s", out)
	}
}

func TestSearchFilesCapsAndRejects(t *testing.T) {
	cs := gameFixture(t)
	out, _ := callFiles(t, cs, "search_files", map[string]any{"query": "common line"})
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n > 51 { // 50 matches plus a truncation note
		t.Errorf("%d lines, want at most 50 matches", n)
	}
	for _, bad := range []map[string]any{{"query": ""}, {"query": "x", "path_glob": "../**"}, {"query": "x", "path_glob": "/etc/*"}, {"query": "x", "path_glob": ".env"}} {
		if out, isErr := callFiles(t, cs, "search_files", bad); !isErr {
			t.Errorf("%v accepted: %s", bad, out)
		}
	}
}

func TestFindFilesGlobs(t *testing.T) {
	cs := gameFixture(t)
	out, _ := callFiles(t, cs, "find_files", map[string]any{"query": "**/*.js"})
	if !strings.Contains(out, "game/src/stages/stage-one.js") || strings.Contains(out, ".json") {
		t.Errorf("**/*.js:\n%s", out)
	}
	out, _ = callFiles(t, cs, "find_files", map[string]any{"query": "game/**"})
	if !strings.Contains(out, "game/README.md") || strings.Contains(out, "keys/") {
		t.Errorf("game/**:\n%s", out)
	}
	out, _ = callFiles(t, cs, "find_files", map[string]any{"query": "**"})
	if !strings.Contains(out, "game/README.md") || strings.Contains(out, ".env") || strings.Contains(out, ".pem") {
		t.Errorf("** must list the tree without secrets:\n%s", out)
	}
	out, _ = callFiles(t, cs, "find_files", map[string]any{"query": "STAGE-ONE"})
	if !strings.Contains(out, "stage-one.js") {
		t.Errorf("substring match lost:\n%s", out)
	}
}

func TestProjectFileToolDescriptions(t *testing.T) {
	cs := gameFixture(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := map[string]string{}
	for _, tl := range res.Tools {
		d[tl.Name] = tl.Description
	}
	if !strings.Contains(d["find_files"], "**/*.js") || !strings.Contains(d["find_files"], "game/**") || !strings.Contains(d["find_files"], "lists the tree") {
		t.Errorf("find_files description: %s", d["find_files"])
	}
	if !strings.Contains(d["search_files"], "contents") {
		t.Errorf("search_files description: %q", d["search_files"])
	}
}

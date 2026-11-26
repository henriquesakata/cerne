package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPServersListedWithMaskedSecrets(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "mcp-server"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", bin)

	writeFile(t, filepath.Join(proj, ".mcp.json"), `{
		"mcpServers": {
			"headroom": {"command": "mcp-server", "args": ["--x"], "env": {"API_KEY": "segredo-absoluto"}},
			"remoto": {"type": "http", "url": "https://mcp.example.com/sse", "headers": {"Authorization": "Bearer token-secreto"}}
		}
	}`)
	writeFile(t, filepath.Join(home, ".claude.json"), `{
		"mcpServers": {"global-tool": {"command": "faltando-no-path"}}
	}`)

	inv := Inspect(Options{ProjDir: proj, Home: home})

	it, ok := findItem(inv, "mcp", "headroom")
	if !ok {
		t.Fatal("project MCP server absent")
	}
	if it.State != StateConfigured || it.Origin != OriginProject {
		t.Errorf("headroom = %s/%s, want configured/project", it.State, it.Origin)
	}
	if !strings.Contains(it.Detail, "API_KEY") {
		t.Errorf("env var name not surfaced: %q", it.Detail)
	}

	it, ok = findItem(inv, "mcp", "remoto")
	if !ok {
		t.Fatal("remote MCP server absent")
	}
	if !strings.Contains(it.Detail, "http") {
		t.Errorf("transport not surfaced: %q", it.Detail)
	}

	it, ok = findItem(inv, "mcp", "global-tool")
	if !ok {
		t.Fatal("user MCP server absent")
	}
	if it.State != StateBroken || !it.OutsideRepo {
		t.Errorf("user server with missing binary = %s outside=%v, want broken/true", it.State, it.OutsideRepo)
	}
	if !strings.Contains(it.Detail, "faltando-no-path") {
		t.Errorf("missing binary not named: %q", it.Detail)
	}

	// The leak gate: no secret value may appear anywhere in the payload.
	raw, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"segredo-absoluto", "token-secreto", "Bearer"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret %q leaked into the inventory payload", secret)
		}
	}
}

func TestMCPNeverSpawnsServer(t *testing.T) {
	proj := t.TempDir()
	bin := t.TempDir()
	marker := filepath.Join(bin, "executed")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	os.WriteFile(filepath.Join(bin, "trap-server"), []byte(script), 0o755)
	t.Setenv("PATH", bin)
	writeFile(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers": {"trap": {"command": "trap-server"}}}`)

	Inspect(Options{ProjDir: proj})

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("inspection executed the MCP server binary")
	}
}

// F-0512 crit. 4: the servers of the project's .cursor/mcp.json (origin
// project) and the user's ~/.cursor/mcp.json (origin user) are inventoried,
// consumed by Cursor, with no env or header value in the payload.
func TestMCPCursorServersListed(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "cerne"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", bin)
	writeFile(t, filepath.Join(proj, ".cursor", "mcp.json"), `{"mcpServers": {"cerne": {"type": "stdio", "command": "cerne", "args": ["mcp"], "env": {"TOKEN": "valor-secreto"}}}}`)
	writeFile(t, filepath.Join(home, ".cursor", "mcp.json"), `{"mcpServers": {"cursor-global": {"url": "https://mcp.example.com", "headers": {"Authorization": "Bearer cabecalho-secreto"}}}}`)

	inv := Inspect(Options{ProjDir: proj, Home: home})

	it, ok := findItem(inv, "mcp", "cerne")
	if !ok {
		t.Fatal(".cursor/mcp.json server absent")
	}
	if it.Origin != OriginProject || it.State != StateConfigured || it.OutsideRepo || it.Path != filepath.Join(proj, ".cursor", "mcp.json") {
		t.Errorf("cerne = %+v, want configured/project inside the repo", it)
	}
	if len(it.Consumers) != 1 || it.Consumers[0] != "cursor" {
		t.Errorf("consumers = %v, want [cursor]", it.Consumers)
	}
	it, ok = findItem(inv, "mcp", "cursor-global")
	if !ok {
		t.Fatal("~/.cursor/mcp.json server absent")
	}
	if it.Origin != OriginUser || !it.OutsideRepo || !strings.Contains(it.Detail, "Authorization") {
		t.Errorf("cursor-global = %+v, want user origin, outside the repo, header name surfaced", it)
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"valor-secreto", "cabecalho-secreto", "Bearer"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret %q leaked into the inventory payload", secret)
		}
	}
}

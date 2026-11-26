package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cursorMCPResult(t *testing.T, rs []Result) Result {
	t.Helper()
	for _, r := range rs {
		if r.Asset == "cursor: mcp.json" {
			return r
		}
	}
	t.Fatalf("no cursor: mcp.json result in %v", rs)
	return Result{}
}

// F-0512 crit. 3: the install declares the read-only cerne server in
// .cursor/mcp.json, keeps the user's other servers and keys, and a second run
// changes nothing.
func TestInstallCursorMCP(t *testing.T) {
	proj := t.TempDir()
	path := filepath.Join(proj, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	user := `{"mcpServers":{"mine":{"command":"mine-server","env":{"K":"v"}}},"other":true}`
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := InstallNetwork(proj, false, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if r := cursorMCPResult(t, rs); r.Status != StatusUpdated {
		t.Errorf("status = %s (%s), want updated", r.Status, r.Note)
	}
	first, _ := os.ReadFile(path)
	var doc struct {
		Servers map[string]map[string]any `json:"mcpServers"`
		Other   bool                      `json:"other"`
	}
	if err := json.Unmarshal(first, &doc); err != nil {
		t.Fatalf("mcp.json not JSON: %v\n%s", err, first)
	}
	got, _ := json.Marshal(doc.Servers["cerne"])
	if string(got) != `{"args":["mcp"],"command":"cerne","type":"stdio"}` {
		t.Errorf("cerne server = %s", got)
	}
	if doc.Servers["mine"]["command"] != "mine-server" || !doc.Other {
		t.Errorf("user entries lost:\n%s", first)
	}

	rs, err = InstallNetwork(proj, false, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if r := cursorMCPResult(t, rs); r.Status != StatusUnchanged {
		t.Errorf("second run = %s, want unchanged", r.Status)
	}
	if second, _ := os.ReadFile(path); string(second) != string(first) {
		t.Errorf("second run changed the file:\n%s\n---\n%s", first, second)
	}
}

// F-0512 crit. 3: a different cerne entry or a file that is not JSON is left
// as it was, reported skipped with the block to add by hand.
func TestInstallCursorMCPSkipsDivergentOrInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"divergent": `{"mcpServers":{"cerne":{"command":"/opt/cerne","args":["mcp","--x"]}}}`,
		"invalid":   `{"mcpServers": {`,
	} {
		t.Run(name, func(t *testing.T) {
			proj := t.TempDir()
			path := filepath.Join(proj, ".cursor", "mcp.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			rs, err := InstallNetwork(proj, false, ProviderCursor)
			if err != nil {
				t.Fatal(err)
			}
			r := cursorMCPResult(t, rs)
			if r.Status != StatusSkipped || !strings.Contains(r.Note, `"args": [`) {
				t.Errorf("want skipped with the block, got %+v", r)
			}
			if raw, _ := os.ReadFile(path); string(raw) != body {
				t.Errorf("file modified:\n%s", raw)
			}
		})
	}
}

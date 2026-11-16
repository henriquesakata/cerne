package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
)

// CopilotHooksState compares .github/hooks/cerne.json with what the install
// writes (FileOK / FileDivergent / FileMissing); an unreadable file counts as
// ok — the check never flags what it cannot read.
func CopilotHooksState(projDir string) string {
	disk, err := os.ReadFile(filepath.Join(projDir, filepath.FromSlash(copilotHooksFile)))
	if os.IsNotExist(err) {
		return FileMissing
	}
	want, wErr := copilotHooksBytes()
	if err != nil || wErr != nil || bytes.Equal(disk, want) {
		return FileOK
	}
	return FileDivergent
}

// CopilotMCPState compares the cerne server of the root .mcp.json with the one
// the install writes.
func CopilotMCPState(projDir string) string {
	raw, err := os.ReadFile(filepath.Join(projDir, ".mcp.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return FileMissing
		}
		return FileOK
	}
	var doc struct {
		Servers map[string]any `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return FileDivergent
	}
	cur, has := doc.Servers["cerne"]
	if !has {
		return FileMissing
	}
	var want any
	wantRaw, _ := json.Marshal(copilotMCPServer())
	_ = json.Unmarshal(wantRaw, &want)
	if reflect.DeepEqual(cur, want) {
		return FileOK
	}
	return FileDivergent
}

package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// mcpServer is the subset of an MCP server declaration the inventory needs.
// Env and header VALUES are deliberately never read into the item — only the
// key names surface (spec criterion 6), and no server is ever spawned
// (criterion 7).
type mcpServer struct {
	Command string            `json:"command"`
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Env     map[string]string `json:"env"`
	Headers map[string]string `json:"headers"`
}

// mcpItems builds the MCP class from the project's .mcp.json and the user's
// ~/.claude.json declarations.
func mcpItems(opts Options, ls *layerSet) []Item {
	var items []Item
	items = append(items, mcpFrom(filepath.Join(opts.ProjDir, ".mcp.json"), OriginProject, ls)...)
	if opts.Home != "" {
		items = append(items, mcpFrom(filepath.Join(opts.Home, ".claude.json"), OriginUser, ls)...)
	}
	return items
}

// mcpFrom reads one declaration file; absent file is silence, malformed file
// is an unreadable layer (criterion 15).
func mcpFrom(path, origin string, ls *layerSet) []Item {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Servers map[string]mcpServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		ls.layers = append(ls.layers, Layer{Path: path, Readable: false, Error: err.Error()})
		return nil
	}
	ls.layers = append(ls.layers, Layer{Path: path, Readable: true})

	names := make([]string, 0, len(doc.Servers))
	for name := range doc.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]Item, 0, len(names))
	for _, name := range names {
		s := doc.Servers[name]
		it := Item{
			Class:       "mcp",
			Name:        name,
			Path:        path,
			Origin:      origin,
			State:       StateConfigured,
			OutsideRepo: origin == OriginUser,
		}
		var details []string
		details = append(details, "transport: "+mcpTransport(s))
		if s.Command != "" {
			exe := strings.Fields(s.Command)[0]
			if _, err := exec.LookPath(exe); err != nil && !strings.ContainsRune(exe, os.PathSeparator) {
				it.State = StateBroken
				details = append(details, "binary \""+exe+"\" not resolvable on PATH")
			} else if strings.ContainsRune(exe, os.PathSeparator) {
				if _, err := os.Stat(exe); err != nil {
					it.State = StateBroken
					details = append(details, "binary "+exe+" not found")
				}
			}
		}
		if keys := sortedKeys(s.Env); len(keys) > 0 {
			details = append(details, "env: "+strings.Join(keys, ", "))
		}
		if keys := sortedKeys(s.Headers); len(keys) > 0 {
			details = append(details, "headers: "+strings.Join(keys, ", "))
		}
		it.Detail = strings.Join(details, "; ")
		items = append(items, it)
	}
	return items
}

// mcpTransport infers the declared transport: explicit type wins, else url
// means a remote server, else stdio.
func mcpTransport(s mcpServer) string {
	if s.Type != "" {
		return s.Type
	}
	if s.URL != "" {
		return "http"
	}
	return "stdio"
}

// sortedKeys returns just the key names — values never leave this file.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

// F-0512: Cursor runs an agent command that misses the user's allowlist in a
// sandbox (workspace_readwrite by default, R-0146) that writes only the
// workspace and /tmp and denies the network. The project's .cursor/sandbox.json
// widens it: its arrays are unioned with the user's ~/.cursor/sandbox.json and
// its paths may use ~/, so the versioned file is valid on every machine.

const cursorSandboxAsset = "cursor: sandbox.json"

// cursorMCP declares the read-only cerne server in .cursor/mcp.json, the only
// project MCP file Cursor reads (it ignores the root .mcp.json, R-0147).
func cursorMCP(projDir string) Result {
	server := map[string]any{"type": "stdio", "command": "cerne", "args": []any{"mcp"}}
	block, _ := marshalIndentNoHTML(map[string]any{"mcpServers": map[string]any{"cerne": server}}, "", "  ")
	r := mergeMCPServer("cursor: mcp.json", filepath.Join(projDir, ".cursor", "mcp.json"), server, "add the read-only cerne server by hand:\n"+string(block))
	if r.Status == StatusCreated || r.Status == StatusUpdated {
		r.Note = "read-only server (cerne mcp): the agent reads the board without the shell"
	}
	return r
}

// cursorSandboxBlock is the JSON a user merges by hand into a sandbox.json.
func cursorSandboxBlock(paths []string, host string) string {
	doc := map[string]any{"additionalReadwritePaths": paths}
	if host != "" {
		doc["networkPolicy"] = map[string]any{"allow": []string{host}}
	}
	out, _ := marshalIndentNoHTML(doc, "", "  ")
	return string(out)
}

// stringList reads a JSON array of strings: absent is empty, any other shape
// is not ok.
func stringList(v any) ([]string, bool) {
	if v == nil {
		return nil, true
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func hasHost(list []string, host string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return strings.EqualFold(s, host) })
}

// installCursorSandbox merges cerne's folders (and, with network, the remote
// backend's host) into the project's .cursor/sandbox.json, key by key, keeping
// every key and entry of the user's. Fail-closed: a file it cannot read as
// JSON, a folder specific to this machine, or an explicit deny of the host
// leaves the file untouched, reported skipped.
func installCursorSandbox(projDir string, network bool) Result {
	skip := func(note string) Result { return Result{Asset: cursorSandboxAsset, Status: StatusSkipped, Note: note} }
	portable, machine, err := sandboxPaths(projDir)
	if err != nil {
		return skip("cannot resolve cerne's folders (" + err.Error() + ") — run from a project with a .cernerc")
	}
	cfg, _, _ := project.LoadConfig(projDir) // sandboxPaths already read it
	host := gitback.RemoteHost(cfg.Backend)
	wantHost := ""
	if network {
		wantHost = host
	}
	if len(machine) > 0 {
		return skip("some of cerne's folders are specific to this machine, so nothing is written to the project file; merge this into your ~/.cursor/sandbox.json:\n" + cursorSandboxBlock(slices.Concat(portable, machine), wantHost))
	}
	byHand := "\n" + cursorSandboxBlock(portable, wantHost)

	path := filepath.Join(projDir, ".cursor", "sandbox.json")
	root := map[string]any{}
	raw, rErr := os.ReadFile(path)
	exists := rErr == nil
	if exists {
		if json.Unmarshal(raw, &root) != nil || root == nil {
			return skip("existing file is not valid JSON — add cerne's folders by hand:" + byHand)
		}
	} else if !os.IsNotExist(rErr) {
		return skip("cannot read: " + rErr.Error())
	}

	changed := !exists
	paths, ok := stringList(root["additionalReadwritePaths"])
	if !ok {
		return skip(`existing "additionalReadwritePaths" is not a list of strings — add cerne's folders by hand:` + byHand)
	}
	for _, p := range portable {
		if !slices.Contains(paths, p) {
			paths = append(paths, p)
			changed = true
		}
	}
	root["additionalReadwritePaths"] = paths

	pol, _ := root["networkPolicy"].(map[string]any)
	if pol == nil && root["networkPolicy"] != nil {
		return skip(`existing "networkPolicy" is not an object — add cerne's entries by hand:` + byHand)
	}
	allow, okAllow := stringList(pol["allow"])
	deny, okDeny := stringList(pol["deny"])
	var note string
	switch {
	case network && host == "":
		note = "the backend is local, so there is no host to allow: networkPolicy left as is (the whole network is never opened)"
	case network && (!okAllow || !okDeny):
		return skip(`existing networkPolicy allow or deny is not a list of strings — add the backend host by hand:` + byHand)
	case network && hasHost(deny, host):
		return skip("networkPolicy.deny lists " + host + ", the backend's host — not overridden, nothing written; remove it from deny and rerun, or rerun without --network to write only cerne's folders")
	case network:
		if !hasHost(allow, host) {
			// Only the host: a "default" here would replace the user's own
			// policy from ~/.cursor/sandbox.json inside this project.
			if pol == nil {
				pol = map[string]any{}
				root["networkPolicy"] = pol
			}
			pol["allow"] = append(allow, host)
			changed = true
		}
		note = "the sandbox allows " + host + " (the backend) for pull and push; the AI layer (plan draft, ask, compile) needs its provider's hosts added to networkPolicy.allow by hand"
	case host != "" && !hasHost(allow, host) && pol["default"] != "allow":
		note = "Cursor's sandbox denies the network by default, so pull and push with " + host + " fail inside it; to allow only that host, run: cerne hook install --provider cursor --network"
	}

	r := Result{Asset: cursorSandboxAsset, Status: StatusUnchanged, Note: note}
	if !changed {
		return r
	}
	out, err := marshalIndentNoHTML(root, "", "  ")
	if err != nil {
		return skip("cannot render config: " + err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return skip("cannot create directory: " + err.Error())
	}
	if err := writeFileAtomic(path, append(out, '\n'), 0o644); err != nil {
		return skip("cannot write: " + err.Error())
	}
	r.Status = StatusUpdated
	if !exists {
		r.Status = StatusCreated
	}
	return r
}

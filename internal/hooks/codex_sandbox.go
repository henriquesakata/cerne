package hooks

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

// F-0474: Codex runs tool commands in a sandbox whose default profile
// (:workspace) writes only the project, /tmp and $TMPDIR. cerne keeps its
// cache and local backend in the user's home, so every cerne command fails
// there. A project .codex/config.toml (read only in a trusted project) can
// widen the sandbox with a profile that extends :workspace, probed on
// codex-cli 0.169.0 (plan.md of F-0474). Paths use ~/ so the versioned file
// is valid on every machine of the team.

const (
	codexSandboxAsset  = "codex: config.toml (sandbox)"
	codexSandboxMarker = "# cerne (F-0474): sandbox"
)

// sandboxPaths returns the folders an agent sandbox must open for cerne
// (shared by the Codex and Cursor adapters, F-0512): portable ones (~/… forms,
// safe to commit) and machine-specific ones (env overrides, a backend outside
// the home), which are printed instead of written.
func sandboxPaths(projDir string) (portable, machine []string, err error) {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		return nil, nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	tilde := func(p string) (string, bool) {
		rel, err := filepath.Rel(home, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return "~/" + filepath.ToSlash(rel), true
	}

	if os.Getenv("CERNE_CACHE_DIR") != "" || os.Getenv("XDG_CACHE_HOME") != "" {
		cache, cErr := project.CacheDir(cfg.ProjectID)
		if cErr != nil {
			return nil, nil, cErr
		}
		machine = append(machine, cache)
	} else {
		portable = append(portable,
			"~/Library/Caches/cerne/"+cfg.ProjectID, // macOS os.UserCacheDir
			"~/.cache/cerne/"+cfg.ProjectID,         // Linux os.UserCacheDir
		)
	}
	if cfg.Backend != "" && gitback.IsLocalPath(cfg.Backend) {
		b := cfg.Backend
		if !filepath.IsAbs(b) {
			b = filepath.Join(projDir, b)
		}
		b = filepath.Clean(b)
		if rel, rErr := filepath.Rel(projDir, b); rErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return portable, machine, nil // inside the project: already writable
		}
		if t, ok := tilde(b); ok {
			portable = append(portable, t)
		} else {
			machine = append(machine, b)
		}
	}
	return portable, machine, nil
}

// codexNetworkTable (F-0484): codex-cli 0.169.0 has no domain list for the
// profile's network, only this switch.
const codexNetworkTable = "[permissions.cerne.network]\nenabled = true\n"

// codexNetworkState parses a config.toml: is permissions.cerne.network present,
// and is its enabled key true? An unparsable file reads as absent.
func codexNetworkState(raw string) (enabled, present bool) {
	var c struct {
		Permissions map[string]struct {
			Network map[string]any `toml:"network"`
		} `toml:"permissions"`
	}
	if _, err := toml.Decode(raw, &c); err != nil {
		return false, false
	}
	n := c.Permissions["cerne"].Network
	if n == nil {
		return false, false
	}
	e, _ := n["enabled"].(bool)
	return e, true
}

// CodexNetworkOpen reports whether the project's .codex/config.toml opens the
// network in the cerne profile.
func CodexNetworkOpen(projDir string) bool {
	raw, err := os.ReadFile(filepath.Join(projDir, ".codex", "config.toml"))
	if err != nil {
		return false
	}
	en, _ := codexNetworkState(string(raw))
	return en
}

// appendCodexNetwork adds only the network table to a file that already has the
// F-0474 block, validating the result before writing.
func appendCodexNetwork(path, body string) Result {
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	out := body + "\n" + codexNetworkTable
	var check map[string]any
	if _, err := toml.Decode(out, &check); err != nil {
		return Result{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "adding the network table would make the file invalid (" + err.Error() + ") — add it manually:\n" + codexNetworkTable}
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return Result{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "cannot write: " + err.Error()}
	}
	return Result{Asset: codexSandboxAsset, Status: StatusUpdated}
}

func codexProfileTables(paths []string, network bool) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("[permissions.cerne]\nextends = \":workspace\"\n\n[permissions.cerne.filesystem]\n")
	for _, p := range sorted {
		b.WriteString(strconv.Quote(p) + " = \"write\"\n")
	}
	if network {
		b.WriteString("\n" + codexNetworkTable)
	}
	return b.String()
}

// codexSandboxSnippet is what the user pastes into ~/.codex/config.toml when
// the install cannot (or must not) write it into the project.
func codexSandboxSnippet(paths []string, network bool) string {
	var b strings.Builder
	home, _ := os.UserHomeDir()
	b.WriteString("start Codex with: codex")
	for _, p := range paths {
		if strings.HasPrefix(p, "~/") && home != "" {
			p = filepath.Join(home, p[2:])
		}
		b.WriteString(" --add-dir " + strconv.Quote(p))
	}
	b.WriteString("\nor add to your ~/.codex/config.toml:\ndefault_permissions = \"cerne\"\n\n")
	b.WriteString(codexProfileTables(paths, network))
	return b.String()
}

// installCodexSandbox writes the managed sandbox block into the project's
// .codex/config.toml, preserving the user's lines. Fail-closed: a file that is
// not valid TOML, or would not be after the edit, is left untouched.
func installCodexSandbox(projDir string, network bool) []Result {
	portable, machine, err := sandboxPaths(projDir)
	if err != nil {
		return []Result{{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "cannot resolve cerne's folders (" + err.Error() + ") — run from a project with a .cernerc"}}
	}
	if len(machine) > 0 {
		// Any machine-specific folder: write nothing to the versioned file and
		// print ONE snippet with every folder (two layers defining the same
		// profile is unverified).
		return []Result{{Asset: codexSandboxAsset, Status: StatusSkipped,
			Note: "some of cerne's folders are specific to this machine, so nothing is written to the project file; " + codexSandboxSnippet(append(append([]string(nil), portable...), machine...), network)}}
	}
	var results []Result
	if len(portable) == 0 {
		return results
	}

	path := filepath.Join(projDir, ".codex", "config.toml")
	raw, rErr := os.ReadFile(path)
	exists := rErr == nil
	if exists {
		if strings.Contains(string(raw), codexSandboxMarker) {
			if network {
				switch enabled, present := codexNetworkState(string(raw)); {
				case present && !enabled:
					return []Result{{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "[permissions.cerne.network] has enabled = false — not changed; set it to true to open the network"}}
				case !present:
					return []Result{appendCodexNetwork(path, string(raw))}
				}
			}
			return append([]Result{{Asset: codexSandboxAsset, Status: StatusUnchanged}}, results...)
		}
		var top map[string]any
		if _, dErr := toml.Decode(string(raw), &top); dErr != nil {
			return append([]Result{{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "existing file is not valid TOML — add the sandbox profile manually: " + codexSandboxSnippet(portable, network)}}, results...)
		}
		if _, has := top["default_permissions"]; has {
			return append([]Result{{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "default_permissions is already set — not overwritten; " + codexSandboxSnippet(portable, network)}}, results...)
		}
	}

	body := string(raw)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	// default_permissions must come before any table header: a top-level key
	// after [table] would fall inside that table.
	out := codexSandboxMarker + "\ndefault_permissions = \"cerne\"\n\n" + body
	if body != "" {
		out += "\n"
	}
	out += codexProfileTables(portable, network)
	var check map[string]any
	if _, dErr := toml.Decode(out, &check); dErr != nil {
		return append([]Result{{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "adding the profile would make the file invalid (" + dErr.Error() + ") — add it manually: " + codexSandboxSnippet(portable, network)}}, results...)
	}
	if mErr := os.MkdirAll(filepath.Dir(path), 0o755); mErr != nil {
		return append([]Result{{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "cannot create directory: " + mErr.Error()}}, results...)
	}
	if wErr := writeFileAtomic(path, []byte(out), 0o644); wErr != nil {
		return append([]Result{{Asset: codexSandboxAsset, Status: StatusSkipped, Note: "cannot write: " + wErr.Error()}}, results...)
	}
	st := StatusUpdated
	if !exists {
		st = StatusCreated
	}
	return append([]Result{{Asset: codexSandboxAsset, Status: st}}, results...)
}

package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

type cursorSandbox struct {
	Type  string   `json:"type"`
	Paths []string `json:"additionalReadwritePaths"`
	Net   *struct {
		Default string   `json:"default"`
		Allow   []string `json:"allow"`
		Deny    []string `json:"deny"`
	} `json:"networkPolicy"`
}

func cursorSandboxPath(proj string) string { return filepath.Join(proj, ".cursor", "sandbox.json") }

func readCursorSandbox(t *testing.T, proj string) (cursorSandbox, string) {
	t.Helper()
	raw, err := os.ReadFile(cursorSandboxPath(proj))
	if err != nil {
		t.Fatal(err)
	}
	var c cursorSandbox
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("sandbox.json is not valid JSON: %v\n%s", err, raw)
	}
	return c, string(raw)
}

func cursorSandboxResult(t *testing.T, rs []Result) Result {
	t.Helper()
	for _, r := range rs {
		if r.Asset == "cursor: sandbox.json" {
			return r
		}
	}
	t.Fatalf("no cursor: sandbox.json result in %v", rs)
	return Result{}
}

func writeCursorSandbox(t *testing.T, proj, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(proj, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorSandboxPath(proj), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// F-0512 crit. 1 (review #1a): the renewal path (Install, what the automatic
// refresh and cerne update run) never creates or widens .cursor/sandbox.json or
// .cursor/mcp.json — only the explicit hook install (InstallNetwork) does.
func TestInstallCursorRenewalNeverTouchesSandboxOrMCP(t *testing.T) {
	proj, home := codexProject(t, "git@example.com:o/r.git")
	if _, err := Install(proj, ProviderCursor); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"sandbox.json", "mcp.json"} {
		if _, err := os.Stat(filepath.Join(proj, ".cursor", f)); !os.IsNotExist(err) {
			t.Errorf("renewal created .cursor/%s (stat err %v)", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, ".cursor", "rules", "cerne.mdc")); err != nil {
		t.Errorf("renewal no longer writes the rules: %v", err)
	}

	if _, err := InstallNetwork(proj, false, ProviderCursor); err != nil {
		t.Fatal(err)
	}
	_, before := readCursorSandbox(t, proj)
	other := filepath.Join(home, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \""+other+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(proj, ProviderCursor); err != nil {
		t.Fatal(err)
	}
	if _, after := readCursorSandbox(t, proj); after != before {
		t.Errorf("renewal widened the sandbox:\n%s\n---\n%s", before, after)
	}
}

// F-0512 crit. 1: the install opens the cache (both OS forms) and a local
// backend under the home, with ~/ paths, and writes no network policy.
func TestInstallCursorSandboxOpensCacheAndBackend(t *testing.T) {
	proj, home := codexProject(t, "x")
	backend := filepath.Join(home, "Library", "Application Support", "cerne", "backends", "demo.git")
	bareRepo(t, backend)
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \""+backend+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := InstallNetwork(proj, false, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if r := cursorSandboxResult(t, rs); r.Status != StatusCreated {
		t.Errorf("status = %s (%s), want created", r.Status, r.Note)
	}
	c, raw := readCursorSandbox(t, proj)
	for _, want := range []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo", "~/Library/Application Support/cerne/backends/demo.git"} {
		if !slices.Contains(c.Paths, want) {
			t.Errorf("additionalReadwritePaths missing %q:\n%s", want, raw)
		}
	}
	if c.Net != nil || c.Type != "" {
		t.Errorf("wrote keys beyond the folders:\n%s", raw)
	}
}

// F-0512 crit. 1: the user's keys and paths survive, ours are appended once, a
// second run changes nothing, and nothing is written outside the project.
func TestInstallCursorSandboxPreservesAndIsIdempotent(t *testing.T) {
	proj, home := codexProject(t, "git@example.com:o/r.git")
	writeCursorSandbox(t, proj, `{"type":"workspace_readwrite","additionalReadwritePaths":["~/mine","~/.cache/cerne/demo"],"networkPolicy":{"default":"deny","allow":["pypi.org"]}}`)

	rs, err := InstallNetwork(proj, false, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if r := cursorSandboxResult(t, rs); r.Status != StatusUpdated {
		t.Errorf("status = %s (%s), want updated", r.Status, r.Note)
	}
	c, first := readCursorSandbox(t, proj)
	want := []string{"~/mine", "~/.cache/cerne/demo", "~/Library/Caches/cerne/demo"}
	if !slices.Equal(c.Paths, want) {
		t.Errorf("paths = %v, want %v", c.Paths, want)
	}
	if c.Type != "workspace_readwrite" || c.Net == nil || c.Net.Default != "deny" || !slices.Equal(c.Net.Allow, []string{"pypi.org"}) {
		t.Errorf("user keys lost:\n%s", first)
	}

	rs, err = InstallNetwork(proj, false, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if r := cursorSandboxResult(t, rs); r.Status != StatusUnchanged {
		t.Errorf("second run status = %s, want unchanged", r.Status)
	}
	_, second := readCursorSandbox(t, proj)
	if first != second {
		t.Fatalf("second run changed the file:\n%s\n---\n%s", first, second)
	}
	if ents, _ := os.ReadDir(home); len(ents) != 0 {
		t.Errorf("home was written: %v", ents)
	}
}

// F-0512 crit. 1: a file that is not valid JSON is left untouched and the
// result is skipped with the block to add by hand.
func TestInstallCursorSandboxInvalidJSONSkipped(t *testing.T) {
	proj, _ := codexProject(t, "git@example.com:o/r.git")
	bad := `{"additionalReadwritePaths": [`
	writeCursorSandbox(t, proj, bad)
	rs, err := InstallNetwork(proj, false, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	r := cursorSandboxResult(t, rs)
	if r.Status != StatusSkipped || !strings.Contains(r.Note, "not valid JSON") || !strings.Contains(r.Note, "~/.cache/cerne/demo") {
		t.Errorf("want skipped with the block, got %+v", r)
	}
	if raw, _ := os.ReadFile(cursorSandboxPath(proj)); string(raw) != bad {
		t.Fatalf("invalid file was modified:\n%s", raw)
	}
}

// F-0512 crit. 1: a cache folder moved by CERNE_CACHE_DIR or XDG_CACHE_HOME is
// specific to this machine: nothing goes to the project file, and the block
// for ~/.cursor/sandbox.json is printed with every folder.
func TestInstallCursorSandboxMachineSpecificIsPrinted(t *testing.T) {
	for _, env := range []string{"CERNE_CACHE_DIR", "XDG_CACHE_HOME"} {
		t.Run(env, func(t *testing.T) {
			proj, home := codexProject(t, "git@example.com:o/r.git")
			cache := t.TempDir()
			t.Setenv(env, cache)
			rs, err := InstallNetwork(proj, true, ProviderCursor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(cursorSandboxPath(proj)); !os.IsNotExist(err) {
				t.Fatalf("project file written with a machine path (stat err %v)", err)
			}
			r := cursorSandboxResult(t, rs)
			if r.Status != StatusSkipped {
				t.Errorf("status = %s, want skipped", r.Status)
			}
			// macOS ignores XDG_CACHE_HOME: the folder is whatever cerne resolves.
			resolved, err := project.CacheDir("demo")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"~/.cursor/sandbox.json", "additionalReadwritePaths", resolved, `"example.com"`} {
				if !strings.Contains(r.Note, want) {
					t.Errorf("note missing %q:\n%s", want, r.Note)
				}
			}
			if ents, _ := os.ReadDir(home); len(ents) != 0 {
				t.Errorf("home was written: %v", ents)
			}
		})
	}
}

// F-0512 crit. 2: with --network, the host of a remote backend (SSH scp-like,
// ssh:// or HTTPS) goes into networkPolicy.allow once; without it, nothing
// opens the network and the note shows the command.
func TestInstallCursorSandboxNetworkAllowsBackendHost(t *testing.T) {
	cases := []struct{ backend, host string }{
		{"git@git.example.com:o/r.git", "git.example.com"},
		{"https://GitHub.com/o/r.git", "github.com"},
		{"ssh://git@host.example:2222/r.git", "host.example"},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			proj, _ := codexProject(t, tc.backend)
			rs, err := InstallNetwork(proj, false, ProviderCursor)
			if err != nil {
				t.Fatal(err)
			}
			if c, raw := readCursorSandbox(t, proj); c.Net != nil {
				t.Errorf("network policy written without --network:\n%s", raw)
			}
			if r := cursorSandboxResult(t, rs); !strings.Contains(r.Note, "cerne hook install --provider cursor --network") || !strings.Contains(r.Note, tc.host) {
				t.Errorf("note does not show the --network command for %s: %q", tc.host, r.Note)
			}

			for i := 0; i < 2; i++ {
				if _, err := InstallNetwork(proj, true, ProviderCursor); err != nil {
					t.Fatal(err)
				}
			}
			c, raw := readCursorSandbox(t, proj)
			if c.Net == nil || !slices.Equal(c.Net.Allow, []string{tc.host}) {
				t.Errorf("allow = %v, want [%s]:\n%s", c.Net, tc.host, raw)
			}
			if c.Net != nil && c.Net.Default != "" {
				t.Errorf("default written (would override the user's own policy): %q", c.Net.Default)
			}
		})
	}
}

// F-0512 crit. 2: an explicit deny of the backend host is never overridden —
// the file stays as it was and the result is skipped with a warning.
func TestInstallCursorSandboxNetworkNeverOverridesDeny(t *testing.T) {
	proj, _ := codexProject(t, "git@git.example.com:o/r.git")
	user := `{"networkPolicy":{"default":"deny","deny":["git.example.com"]}}`
	writeCursorSandbox(t, proj, user)
	rs, err := InstallNetwork(proj, true, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	r := cursorSandboxResult(t, rs)
	if r.Status != StatusSkipped || !strings.Contains(r.Note, "networkPolicy.deny") || !strings.Contains(r.Note, "git.example.com") {
		t.Errorf("want skipped with the deny warning, got %+v", r)
	}
	if raw, _ := os.ReadFile(cursorSandboxPath(proj)); string(raw) != user {
		t.Fatalf("file with an explicit deny was modified:\n%s", raw)
	}
}

// F-0512 crit. 2 (decision recorded in result.md): a local backend has no host
// to allow, so --network leaves networkPolicy alone — it never opens the whole
// network — and says so.
func TestInstallCursorSandboxNetworkLocalBackendOpensNothing(t *testing.T) {
	proj, home := codexProject(t, "x")
	backend := filepath.Join(home, "backends", "demo.git")
	bareRepo(t, backend)
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \""+backend+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := InstallNetwork(proj, true, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	c, raw := readCursorSandbox(t, proj)
	if c.Net != nil {
		t.Errorf("network policy written for a local backend:\n%s", raw)
	}
	if r := cursorSandboxResult(t, rs); !strings.Contains(r.Note, "local") || !strings.Contains(r.Note, "networkPolicy") {
		t.Errorf("note does not say why the network was left alone: %q", r.Note)
	}
}

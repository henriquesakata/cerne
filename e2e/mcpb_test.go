//go:build !windows

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henriquesakata/cerne/internal/mcp"
)

// Black-box tests of the MCP Registry listing (F-0455): the committed
// server.json template, the MCPB manifest and launcher, and the bundle that
// scripts/mcpb-bundle.sh builds from a release dist/. The schemas in testdata
// are verbatim copies of the registry's server.json schema (2025-12-11) and
// the MCPB manifest schema v0.3.

// validate checks doc against the JSON schema in testdata/<schemaFile>.
func validate(t *testing.T, schemaFile string, doc []byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s: %v", schemaFile, err)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: resolve: %v", schemaFile, err)
	}
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, doc)
	}
	if err := r.Validate(v); err != nil {
		t.Errorf("does not validate against %s: %v", schemaFile, err)
	}
}

type serverJSON struct {
	Schema      string `json:"$schema"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	WebsiteURL  string `json:"websiteUrl"`
	Repository  struct {
		URL    string `json:"url"`
		Source string `json:"source"`
	} `json:"repository"`
	Packages []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		FileSha256   string `json:"fileSha256"`
		Transport    struct {
			Type string `json:"type"`
		} `json:"transport"`
	} `json:"packages"`
}

func readServerJSON(t *testing.T, raw []byte) serverJSON {
	t.Helper()
	var s serverJSON
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("server.json: %v", err)
	}
	return s
}

// F-0455 criterion 1: the template carries every field the registry shows,
// one stdio MCPB package, and only the three per-release markers.
func TestMCPB_ServerJSONTemplate(t *testing.T) {
	raw, err := os.ReadFile("../server.json")
	if err != nil {
		t.Fatal(err)
	}
	s := readServerJSON(t, raw)
	if s.Name != "io.github.henriquesakata/cerne" {
		t.Errorf("name = %q", s.Name)
	}
	if s.Title == "" || s.WebsiteURL == "" {
		t.Errorf("title %q / websiteUrl %q must be set", s.Title, s.WebsiteURL)
	}
	if n := len(s.Description); n == 0 || n > 100 {
		t.Errorf("description has %d characters, want 1-100: %q", n, s.Description)
	}
	if s.Repository.URL != "https://github.com/henriquesakata/cerne" || s.Repository.Source != "github" {
		t.Errorf("repository = %+v", s.Repository)
	}
	if len(s.Packages) != 1 || s.Packages[0].RegistryType != "mcpb" || s.Packages[0].Transport.Type != "stdio" {
		t.Fatalf("packages = %+v, want one stdio mcpb package", s.Packages)
	}
	var markers []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\$\{([A-Za-z0-9_.]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			markers = append(markers, m[1])
		}
	}
	sort.Strings(markers)
	if strings.Join(markers, ",") != "SHA256,TAG,VERSION" {
		t.Errorf("markers = %v, want exactly SHA256, TAG, VERSION", markers)
	}
}

type mcpbManifest struct {
	ManifestVersion string `json:"manifest_version"`
	Version         string `json:"version"`
	Server          struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command           string   `json:"command"`
			Args              []string `json:"args"`
			PlatformOverrides map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"platform_overrides"`
		} `json:"mcp_config"`
	} `json:"server"`
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
	UserConfig map[string]struct {
		Type     string `json:"type"`
		Required bool   `json:"required"`
	} `json:"user_config"`
}

func readManifest(t *testing.T, raw []byte) mcpbManifest {
	t.Helper()
	var m mcpbManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	return m
}

// committedManifest is packaging/mcpb/manifest.json with its version marker
// filled in, as the bundle script does.
func committedManifest(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../packaging/mcpb/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.ReplaceAll(string(raw), "${VERSION}", "9.9.9"))
}

// F-0455 criterion 4: the client asks for the project folder and starts
// `mcp --dir <folder>`: through /bin/sh and the launcher on darwin/linux,
// straight through the amd64 .exe on Windows.
func TestMCPB_ManifestLaunch(t *testing.T) {
	raw := committedManifest(t)
	validate(t, "mcpb-manifest-v0.3.schema.json", raw)
	m := readManifest(t, raw)
	dir, ok := m.UserConfig["project_dir"]
	if !ok || dir.Type != "directory" || !dir.Required {
		t.Errorf("user_config.project_dir = %+v (present %v), want a required directory", dir, ok)
	}
	tail := []string{"mcp", "--dir", "${user_config.project_dir}"}
	mc := m.Server.MCPConfig
	if m.Server.Type != "binary" || m.Server.EntryPoint != "server/cerne.sh" {
		t.Errorf("server type %q entry_point %q", m.Server.Type, m.Server.EntryPoint)
	}
	if mc.Command != "/bin/sh" || strings.Join(mc.Args, " ") != strings.Join(append([]string{"${__dirname}/server/cerne.sh"}, tail...), " ") {
		t.Errorf("posix launch = %q %q", mc.Command, mc.Args)
	}
	win, ok := mc.PlatformOverrides["win32"]
	if !ok || win.Command != "${__dirname}/server/cerne_windows_amd64.exe" || strings.Join(win.Args, " ") != strings.Join(tail, " ") {
		t.Errorf("win32 launch = %+v (present %v)", win, ok)
	}
	if len(mc.PlatformOverrides) != 1 {
		t.Errorf("platform_overrides = %v, want win32 only", mc.PlatformOverrides)
	}
}

// F-0455 criterion 6: the manifest lists exactly the tools the server
// registers, so a tool added or removed on one side fails here.
func TestMCPB_ToolsMatchServer(t *testing.T) {
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	noop := func([]string) ([]byte, error) { return []byte("{}"), nil }
	if _, err := mcp.NewServer("test", noop).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var served, listed []string
	for _, tool := range res.Tools {
		served = append(served, tool.Name)
	}
	for _, tool := range readManifest(t, committedManifest(t)).Tools {
		listed = append(listed, tool.Name)
	}
	sort.Strings(served)
	sort.Strings(listed)
	if strings.Join(served, ",") != strings.Join(listed, ",") {
		t.Errorf("manifest tools %v, server tools %v", listed, served)
	}
}

// runLauncher runs packaging/mcpb/cerne.sh as the manifest does (/bin/sh
// <launcher> args) from a bundle-like server/ folder holding fake binaries
// for the given pairs, with a fake uname answering osName and machine.
func runLauncher(t *testing.T, osName, machine string, pairs map[string]os.FileMode, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	fakeBin := filepath.Join(dir, "bin")
	for _, d := range []string{server, fakeBin} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launcher, err := os.ReadFile("../packaging/mcpb/cerne.sh")
	if err != nil {
		t.Fatal(err)
	}
	// 0644: the client runs it through /bin/sh, so it needs no execute bit.
	if err := os.WriteFile(filepath.Join(server, "cerne.sh"), launcher, 0o644); err != nil {
		t.Fatal(err)
	}
	for pair, mode := range pairs {
		name := "cerne_" + pair
		if err := os.WriteFile(filepath.Join(server, name), []byte("#!/bin/sh\necho "+name+" \"$@\"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	uname := "#!/bin/sh\ncase $1 in -s) echo " + osName + " ;; -m) echo " + machine + " ;; *) exit 2 ;; esac\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "uname"), []byte(uname), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", append([]string{filepath.Join(server, "cerne.sh")}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+":/usr/bin:/bin")
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// F-0455 criterion 5: each darwin/linux × amd64/arm64 machine runs its own
// binary with the arguments, a binary that lost its execute bit runs anyway,
// and a pair the bundle lacks fails naming the OS and the architecture.
func TestMCPB_Launcher(t *testing.T) {
	all := map[string]os.FileMode{"darwin_amd64": 0o755, "darwin_arm64": 0o755, "linux_amd64": 0o755, "linux_arm64": 0o755}
	for _, c := range []struct{ os, machine, want string }{
		{"Darwin", "x86_64", "darwin_amd64"},
		{"Darwin", "arm64", "darwin_arm64"},
		{"Linux", "x86_64", "linux_amd64"},
		{"Linux", "amd64", "linux_amd64"},
		{"Linux", "aarch64", "linux_arm64"},
		{"Linux", "arm64", "linux_arm64"},
	} {
		out, stderr, err := runLauncher(t, c.os, c.machine, all, "mcp", "--dir", "/some project")
		if err != nil {
			t.Errorf("%s/%s: %v\n%s", c.os, c.machine, err, stderr)
			continue
		}
		if want := "cerne_" + c.want + " mcp --dir /some project\n"; out != want {
			t.Errorf("%s/%s ran %q, want %q", c.os, c.machine, out, want)
		}
	}

	out, stderr, err := runLauncher(t, "Linux", "x86_64", map[string]os.FileMode{"linux_amd64": 0o644}, "mcp")
	if err != nil || out != "cerne_linux_amd64 mcp\n" {
		t.Errorf("binary without execute bit: out %q err %v\n%s", out, err, stderr)
	}

	_, stderr, err = runLauncher(t, "Linux", "riscv64", all, "mcp")
	if err == nil {
		t.Error("linux/riscv64 succeeded with no binary for it")
	}
	if !strings.Contains(stderr, "linux/riscv64") {
		t.Errorf("stderr does not name the pair: %q", stderr)
	}
}

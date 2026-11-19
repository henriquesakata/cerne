//go:build !windows

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
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
	exec := func([]string) ([]byte, error) { return []byte("{}"), nil }
	if _, err := mcp.NewServer("test", exec).Connect(ctx, st, nil); err != nil {
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

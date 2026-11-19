//go:build !windows

package e2e_test

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
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

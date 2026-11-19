//go:build !windows

package e2e_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

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

// fakeDist writes the build-release.sh layout of tag into a temp dir: the
// five binaries (formulaAssets) with distinct content, minus omit, and their
// SHA256SUMS.
func fakeDist(t *testing.T, tag string, omit ...string) string {
	t.Helper()
	dist := t.TempDir()
	var sums strings.Builder
	for _, a := range formulaAssets {
		if contains(omit, a) {
			continue
		}
		name := "cerne_" + tag + "_" + a
		body := []byte("binary " + a)
		if err := os.WriteFile(filepath.Join(dist, name), body, 0o755); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(body), name)
	}
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(sums.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dist
}

func runBundle(t *testing.T, tag, dist string) (stderr string, err error) {
	t.Helper()
	if _, err := exec.LookPath("zip"); err != nil {
		t.Skip("zip not on PATH")
	}
	cmd := exec.Command("sh", "../scripts/mcpb-bundle.sh", tag, dist)
	var errOut strings.Builder
	cmd.Stderr = &errOut
	err = cmd.Run()
	return errOut.String(), err
}

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// F-0455 criterion 3: the bundle holds the versioned manifest, the launcher
// and the five binaries renamed without the tag (executable), the manifest
// validates against MCPB v0.3, and SHA256SUMS gains the bundle's line.
func TestMCPB_BundleContents(t *testing.T) {
	dist := fakeDist(t, "v9.9.9")
	sumsBefore, _ := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if stderr, err := runBundle(t, "v9.9.9", dist); err != nil {
		t.Fatalf("bundle: %v\n%s", err, stderr)
	}
	bundle := filepath.Join(dist, "cerne_v9.9.9.mcpb")
	zr, err := zip.OpenReader(bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string]*zip.File{}
	var names []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		got[f.Name] = f
		names = append(names, f.Name)
	}
	want := []string{"manifest.json", "server/cerne.sh"}
	for _, a := range formulaAssets {
		want = append(want, "server/cerne_"+a)
	}
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("bundle files:\n got %v\nwant %v", names, want)
	}
	read := func(name string) []byte {
		rc, err := got[name].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, a := range formulaAssets {
		name := "server/cerne_" + a
		if string(read(name)) != "binary "+a {
			t.Errorf("%s is not cerne_v9.9.9_%s", name, a)
		}
		if got[name].Mode()&0o111 == 0 {
			t.Errorf("%s is stored without an execute bit: %v", name, got[name].Mode())
		}
	}
	manifest := read("manifest.json")
	validate(t, "mcpb-manifest-v0.3.schema.json", manifest)
	m := readManifest(t, manifest)
	if m.ManifestVersion != "0.3" || m.Version != "9.9.9" || m.Server.Type != "binary" {
		t.Errorf("manifest_version %q version %q server.type %q", m.ManifestVersion, m.Version, m.Server.Type)
	}
	sums, _ := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if wantSums := string(sumsBefore) + fileSHA(t, bundle) + "  cerne_v9.9.9.mcpb\n"; string(sums) != wantSums {
		t.Errorf("SHA256SUMS:\n got %q\nwant %q", sums, wantSums)
	}
}

// F-0455 criterion 2: dist/server.json is the template with the version, the
// release URL of the bundle and its SHA-256, valid against the registry schema.
func TestMCPB_RenderedServerJSON(t *testing.T) {
	dist := fakeDist(t, "v9.9.9")
	if stderr, err := runBundle(t, "v9.9.9", dist); err != nil {
		t.Fatalf("bundle: %v\n%s", err, stderr)
	}
	raw, err := os.ReadFile(filepath.Join(dist, "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	validate(t, "server.schema.json", raw)
	if strings.Contains(string(raw), "${") {
		t.Errorf("a marker survived the render:\n%s", raw)
	}
	s := readServerJSON(t, raw)
	if s.Version != "9.9.9" {
		t.Errorf("version = %q, want 9.9.9", s.Version)
	}
	p := s.Packages[0]
	if want := "https://github.com/henriquesakata/cerne/releases/download/v9.9.9/cerne_v9.9.9.mcpb"; p.Identifier != want {
		t.Errorf("identifier = %q, want %q", p.Identifier, want)
	}
	if want := fileSHA(t, filepath.Join(dist, "cerne_v9.9.9.mcpb")); p.FileSha256 != want {
		t.Errorf("fileSha256 = %q, want the bundle's %q", p.FileSha256, want)
	}
}

// F-0455 criterion 7: a binary missing from dist fails naming it, before any
// bundle or server.json is written.
func TestMCPB_MissingBinary(t *testing.T) {
	dist := fakeDist(t, "v9.9.9", "linux_arm64")
	stderr, err := runBundle(t, "v9.9.9", dist)
	if err == nil {
		t.Fatal("bundle succeeded with linux_arm64 missing")
	}
	if !strings.Contains(stderr, "cerne_v9.9.9_linux_arm64") {
		t.Errorf("stderr does not name the missing binary: %q", stderr)
	}
	for _, f := range []string{"cerne_v9.9.9.mcpb", "server.json"} {
		if _, err := os.Stat(filepath.Join(dist, f)); err == nil {
			t.Errorf("%s was written despite the missing binary", f)
		}
	}
}

// F-0455 criterion 2 (critic finding 1): a re-run of the release rebuilds the
// bundle and re-uploads it over the asset the registry entry already pins by
// hash, so the same binaries must give the same bundle, byte for byte, however
// late they were copied.
func TestMCPB_BundleReproducible(t *testing.T) {
	first := fakeDist(t, "v9.9.9")
	if stderr, err := runBundle(t, "v9.9.9", first); err != nil {
		t.Fatalf("bundle: %v\n%s", err, stderr)
	}
	// Zip stores times at a 2 s granularity: a run 2.1 s later always lands in
	// another slot, so a bundle that kept copy times would differ.
	time.Sleep(2100 * time.Millisecond)
	second := fakeDist(t, "v9.9.9")
	later := time.Now().Add(48 * time.Hour)
	for _, a := range formulaAssets {
		if err := os.Chtimes(filepath.Join(second, "cerne_v9.9.9_"+a), later, later); err != nil {
			t.Fatal(err)
		}
	}
	if stderr, err := runBundle(t, "v9.9.9", second); err != nil {
		t.Fatalf("bundle: %v\n%s", err, stderr)
	}
	a, b := fileSHA(t, filepath.Join(first, "cerne_v9.9.9.mcpb")), fileSHA(t, filepath.Join(second, "cerne_v9.9.9.mcpb"))
	if a != b {
		t.Errorf("same binaries, different bundles: %s vs %s", a, b)
	}
}

// F-0455 criterion 3 (critic finding 5): bundling the same dist twice leaves
// one bundle line in SHA256SUMS, and a dist without SHA256SUMS is refused.
func TestMCPB_SumsLine(t *testing.T) {
	dist := fakeDist(t, "v9.9.9")
	for i := 0; i < 2; i++ {
		if stderr, err := runBundle(t, "v9.9.9", dist); err != nil {
			t.Fatalf("bundle %d: %v\n%s", i, err, stderr)
		}
	}
	sums, _ := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if n := strings.Count(string(sums), "  cerne_v9.9.9.mcpb\n"); n != 1 {
		t.Errorf("SHA256SUMS has %d bundle lines, want 1:\n%s", n, sums)
	}

	bare := fakeDist(t, "v9.9.9")
	if err := os.Remove(filepath.Join(bare, "SHA256SUMS")); err != nil {
		t.Fatal(err)
	}
	stderr, err := runBundle(t, "v9.9.9", bare)
	if err == nil || !strings.Contains(stderr, "SHA256SUMS") {
		t.Errorf("no SHA256SUMS: err %v, stderr %q; want a refusal naming it", err, stderr)
	}
}

// F-0455 criteria 8 and 9: the bundle is built after the binaries and before
// the attestation and the upload of dist/*; the registry job runs after the
// release for stable tags only, and only its publish step, behind
// MCP_REGISTRY_PUBLISH, logs in.
func TestMCPB_ReleaseWorkflow(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	wf := string(raw)
	at := func(s string) int {
		i := strings.Index(wf, s)
		if i < 0 {
			t.Fatalf("release.yml has no %q", s)
		}
		return i
	}
	order := []string{
		`bash scripts/build-release.sh "$GITHUB_REF_NAME"`,
		`sh scripts/mcpb-bundle.sh "$GITHUB_REF_NAME"`,
		"actions/attest-build-provenance@",
		"subject-path: 'dist/*'",
		`sh scripts/release-publish.sh "$GITHUB_REF_NAME" "$notes" dist/*`,
	}
	for i := 1; i < len(order); i++ {
		if at(order[i-1]) > at(order[i]) {
			t.Errorf("%q comes after %q", order[i-1], order[i])
		}
	}
	job := wf[at("\n  mcp-registry:\n"):]
	if next := strings.Index(job[1:], "\n  quickstart:\n"); next > 0 {
		job = job[:next+1]
	}
	for _, want := range []string{
		"needs: release",
		"if: ${{ !contains(github.ref_name, '-') }}",
		"id-token: write",
		"cosign verify-blob",
		"./mcp-publisher validate",
		"if: ${{ vars.MCP_REGISTRY_PUBLISH == 'true' }}",
		"sh scripts/mcp-registry-publish.sh",
	} {
		if !strings.Contains(job, want) {
			t.Errorf("mcp-registry job lacks %q", want)
		}
	}
	gate := strings.Index(job, "if: ${{ vars.MCP_REGISTRY_PUBLISH == 'true' }}")
	if pub := strings.Index(job, "sh scripts/mcp-registry-publish.sh"); gate < 0 || pub < gate {
		t.Error("the publish script runs outside the MCP_REGISTRY_PUBLISH step")
	}
	// The login lives in the publish script alone, behind the gate.
	if strings.Contains(job, "login github") {
		t.Error("the job logs in outside scripts/mcp-registry-publish.sh")
	}
}

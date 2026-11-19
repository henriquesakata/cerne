package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henriquesakata/cerne/internal/mcp"
	"github.com/henriquesakata/cerne/internal/project"
)

// lockedBuf is a bytes.Buffer safe to fill from the client's read goroutine.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// teeReadCloser copies everything the client reads off the server's stdout.
type teeReadCloser struct {
	r   *os.File
	raw *lockedBuf
}

func (t teeReadCloser) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	_, _ = t.raw.Write(p[:n])
	return n, err
}

func (t teeReadCloser) Close() error { return t.r.Close() }

// startMCP runs `cerne mcp --dir projDir` (no --dir when projDir is "") on the
// REAL process stdin/stdout (swapped for pipes), so a byte written to os.Stdout
// by anything — not just the SDK — lands in raw. stop closes the client, waits for the command and
// restores the process streams; it returns the command's error.
func startMCP(t *testing.T, projDir string, flags ...string) (cs *sdk.ClientSession, raw *lockedBuf, stop func() error) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	restore := func() { os.Stdin, os.Stdout = oldIn, oldOut }

	done := make(chan error, 1)
	go func() {
		args := []string{"mcp"}
		if projDir != "" {
			args = append(args, "--dir", projDir)
		}
		out, err := execCerne(t, append(args, flags...)...)
		_ = outW.Close() // EOF for the client, even when mcp refuses to start
		if err != nil {
			err = fmt.Errorf("%w\n%s", err, out)
		}
		done <- err
	}()
	raw = &lockedBuf{}
	cs, err = sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "v0"}, nil).
		Connect(context.Background(), &sdk.IOTransport{Reader: teeReadCloser{outR, raw}, Writer: inW}, nil)
	if err != nil {
		_ = inW.Close()
		runErr := <-done
		restore()
		t.Fatalf("initialize: %v (cerne mcp: %v)", err, runErr)
	}
	var once sync.Once
	var runErr error
	stop = func() error {
		once.Do(func() {
			_ = cs.Close()
			runErr = <-done
			_ = inR.Close()
			restore()
		})
		return runErr
	}
	t.Cleanup(func() { _ = stop() })
	return cs, raw, stop
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s %v: %v", name, args, err)
	}
	if res.IsError || len(res.Content) != 1 {
		t.Fatalf("tools/call %s %v: %+v", name, args, res)
	}
	return res.Content[0].(*sdk.TextContent).Text
}

// mcpFixture is a project with a feature, a decision and a note.
func mcpFixture(t *testing.T) (projDir, origin, repoDir string) {
	t.Helper()
	projDir, origin, repoDir = setupProject(t)
	seedBriefEntities(t, projDir)
	return projDir, origin, repoDir
}

// allCalls exercises every tool, some twice with different arguments, paired
// with the CLI command whose --json each must equal byte for byte.
var allCalls = []struct {
	tool string
	args map[string]any
	cli  []string
}{
	{"status", map[string]any{}, []string{"status", "--json"}},
	{"status", map[string]any{"active": true}, []string{"status", "--json", "--active"}},
	{"brief", map[string]any{}, []string{"brief", "--json"}},
	{"next", map[string]any{}, []string{"next", "--json"}},
	{"search", map[string]any{"query": "Postgres"}, []string{"search", "Postgres", "--json"}},
	{"search", map[string]any{"query": "Conta", "limit": 1}, []string{"search", "Conta", "--json", "--limit", "1"}},
	{"show", map[string]any{"id": "F-0001"}, []string{"show", "F-0001", "--json"}},
	{"show", map[string]any{"id": "N-0001"}, []string{"show", "N-0001", "--json"}},
	{"ask", map[string]any{"question": "qual banco usar?"}, []string{"ask", "qual banco usar?", "--json"}},
	{"guide", map[string]any{"query": "settings"}, []string{"guide", "settings", "--json"}},
}

// TestMCP_ToolMatchesJSONCommand: criterion 2 — every tool answers exactly the
// bytes its command prints with --json, and a second call with other arguments
// is not polluted by the first (fresh cli.Root per call).
func TestMCP_ToolMatchesJSONCommand(t *testing.T) {
	projDir, _, _ := mcpFixture(t)
	cs, _, stop := startMCP(t, projDir)
	got := make([]string, len(allCalls))
	for i, c := range allCalls {
		got[i] = callTool(t, cs, c.tool, c.args)
	}
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp: %v", err)
	}
	for i, c := range allCalls {
		want, _, err := execCerneSplit(t, append(c.cli, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("cerne %v: %v", c.cli, err)
		}
		if !json.Valid([]byte(want)) {
			t.Fatalf("cerne %v printed non-JSON:\n%s", c.cli, want)
		}
		if got[i] != want {
			t.Errorf("tool %s %v differs from cerne %v:\n--- tool\n%s\n--- cli\n%s", c.tool, c.args, c.cli, got[i], want)
		}
	}
	// And as a user types it: from the project root, no --dir.
	t.Chdir(projDir)
	for i, c := range allCalls {
		want, _, err := execCerneSplit(t, c.cli...)
		if err != nil {
			t.Fatalf("cerne %v (cwd = project): %v", c.cli, err)
		}
		if got[i] != want {
			t.Errorf("tool %s %v differs from cerne %v run in the project:\n--- tool\n%s\n--- cli\n%s", c.tool, c.args, c.cli, got[i], want)
		}
	}
}

// TestMCP_StdoutOnlyProtocol: every byte the process writes to stdout during a
// session that calls every tool is a JSON-RPC 2.0 frame.
func TestMCP_StdoutOnlyProtocol(t *testing.T) {
	projDir, _, _ := mcpFixture(t)
	cs, raw, stop := startMCP(t, projDir)
	for _, c := range allCalls {
		callTool(t, cs, c.tool, c.args)
	}
	// A failing command (its cobra "Error:" line goes to stderr) and a refused
	// argument must not leak onto stdout either.
	for _, args := range []map[string]any{{"id": "F-9999"}, {"id": "--help"}} {
		if res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "show", Arguments: args}); err == nil && !res.IsError {
			t.Fatalf("show %v succeeded, want a failure", args)
		}
	}
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp: %v", err)
	}
	assertOnlyFrames(t, raw, len(allCalls)+1) // initialize + one response per call
}

// assertOnlyFrames fails unless every stdout line is a JSON-RPC 2.0 frame and
// there are at least min of them.
func assertOnlyFrames(t *testing.T, raw *lockedBuf, min int) {
	t.Helper()
	frames := 0
	sc := bufio.NewScanner(strings.NewReader(raw.String()))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var msg struct {
			JSONRPC string `json:"jsonrpc"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil || msg.JSONRPC != "2.0" {
			t.Fatalf("stdout line is not a JSON-RPC frame: %q", sc.Text())
		}
		frames++
	}
	if frames < min {
		t.Fatalf("only %d frames on stdout:\n%s", frames, raw.String())
	}
}

// TestMCP_CommandErrorIsToolError: a command that fails (show of a well-formed
// id that does not exist) reaches the client as a tool result with IsError and
// the command's own error message — not as a protocol error.
func TestMCP_CommandErrorIsToolError(t *testing.T) {
	projDir, _, _ := mcpFixture(t)
	_, _, cliErr := execCerneSplit(t, "show", "F-9999", "--json", "--dir", projDir)
	if cliErr == nil {
		t.Fatal("cerne show F-9999 succeeded")
	}
	cs, _, stop := startMCP(t, projDir)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "show", Arguments: map[string]any{"id": "F-9999"}})
	if err != nil {
		t.Fatalf("protocol error %v, want a tool error", err)
	}
	if !res.IsError || len(res.Content) != 1 {
		t.Fatalf("result = %+v, want one IsError content", res)
	}
	if text := res.Content[0].(*sdk.TextContent).Text; text != cliErr.Error() {
		t.Fatalf("tool error = %q, want the command's %q", text, cliErr.Error())
	}
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp: %v", err)
	}
}

// TestMCP_OutsideProject: F-0456 criteria 1 and 2 (they replace F-0363's
// criterion 3, which refused here) — outside a cerne project `cerne mcp` still
// speaks MCP, serves only the offline guide tool with the bytes of
// `cerne guide --json`, and its initialize instructions say the folder is not a
// cerne project and that `cerne init` starts one. Both ways in: --dir naming a
// folder without .cernerc, and no --dir from such a cwd.
func TestMCP_OutsideProject(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_ROOT", "")
	plain := t.TempDir()
	t.Chdir(plain)
	want, _, err := execCerneSplit(t, "guide", "settings", "--json")
	if err != nil {
		t.Fatalf("cerne guide: %v", err)
	}
	for _, dir := range []string{plain, ""} {
		cs, raw, stop := startMCP(t, dir)
		ins := cs.InitializeResult().Instructions
		for _, w := range []string{"living documentation", "not a cerne project", "guide", "cerne init"} {
			if !strings.Contains(ins, w) {
				t.Errorf("--dir %q: instructions lack %q: %q", dir, w, ins)
			}
		}
		res, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if len(res.Tools) != 1 || res.Tools[0].Name != "guide" {
			t.Fatalf("--dir %q: tools = %+v, want only guide", dir, res.Tools)
		}
		if got := callTool(t, cs, "guide", map[string]any{"query": "settings"}); got != want {
			t.Errorf("--dir %q: guide differs from cerne guide --json:\n--- tool\n%s\n--- cli\n%s", dir, got, want)
		}
		if err := stop(); err != nil {
			t.Fatalf("--dir %q: cerne mcp: %v", dir, err)
		}
		assertOnlyFrames(t, raw, 3) // initialize, tools/list, tools/call
	}
}

// TestMCP_DirSubfolderIsNotTheProject: F-0456 criterion 5 — mcp skips the
// root --dir gate but keeps its rule: a --dir naming a subfolder of a project
// gets the guide-only server, never the project above it.
func TestMCP_DirSubfolderIsNotTheProject(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cs, _, stop := startMCP(t, sub)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "guide" {
		t.Fatalf("tools = %+v, want only guide (the parent project must not be served)", res.Tools)
	}
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp: %v", err)
	}
}

// TestMCP_RefusalsKept: F-0456 criterion 4 — only a missing project falls back
// to the guide: a .cernerc that does not load refuses with the error `cerne
// status` gives, and --project-files still refuses a folder without .cernerc.
func TestMCP_RefusalsKept(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_ROOT", "")
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ".cernerc"), []byte("project_id = [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, statusErr := execCerneSplit(t, "status", "--dir", broken)
	mOut, _, mcpErr := execCerneSplit(t, "mcp", "--dir", broken)
	if statusErr == nil || mcpErr == nil || mcpErr.Error() != statusErr.Error() {
		t.Fatalf("broken .cernerc: mcp = %v, want the status refusal %v", mcpErr, statusErr)
	}
	if mOut != "" {
		t.Fatalf("broken .cernerc: stdout must stay empty, got %q", mOut)
	}
	fOut, _, filesErr := execCerneSplit(t, "mcp", "--project-files", "--dir", t.TempDir())
	if !errors.Is(filesErr, project.ErrNoProject) || fOut != "" {
		t.Fatalf("--project-files outside a project: err = %v, stdout = %q; want ErrNoProject and no output", filesErr, fOut)
	}
}

// TestMCP_NoWriteTools: criterion 4 — every listed tool is read-only, the set is
// exactly the six reading tools, and calling them all leaves the backend (the
// origin and the cached clone: HEAD and files) untouched.
func TestMCP_NoWriteTools(t *testing.T) {
	projDir, origin, repoDir := mcpFixture(t)
	headBefore, cloneBefore, originBefore := gitHead(t, repoDir), treeDigest(t, repoDir), gitHead(t, origin)
	cs, _, stop := startMCP(t, projDir)
	if ins := cs.InitializeResult().Instructions; ins != "" { // F-0456 criterion 3
		t.Errorf("inside a project the server sends no instructions, got %q", ins)
	}
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not read-only", tool.Name)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "ask,brief,guide,next,search,show,status" {
		t.Fatalf("tools = %v, want exactly the seven reading tools", names)
	}
	for _, c := range allCalls {
		callTool(t, cs, c.tool, c.args)
	}
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp: %v", err)
	}
	if after := gitHead(t, repoDir); after != headBefore {
		t.Fatalf("backend clone HEAD moved: %s -> %s", headBefore, after)
	}
	if treeDigest(t, repoDir) != cloneBefore {
		t.Fatal("backend clone files changed")
	}
	if after := gitHead(t, origin); after != originBefore {
		t.Fatalf("origin HEAD moved: %s -> %s", originBefore, after)
	}
}

// TestMCP_ToolCommandsNeverTouchProcessStdio: the six commands behind the MCP
// tools write only through cmd.OutOrStdout/ErrOrStderr. A direct os.Stdout,
// os.Stdin, fmt.Print* or os.Exit would corrupt the protocol channel (stdout)
// or kill the server, so any appearance in their sources fails here.
func TestMCP_ToolCommandsNeverTouchProcessStdio(t *testing.T) {
	banned := regexp.MustCompile(`os\.Stdout|os\.Stdin|os\.Exit\(|fmt\.Print(ln|f)?\(`)
	for _, name := range []string{"status", "brief", "next", "search", "show", "ask"} {
		src, err := os.ReadFile(name + "cmd.go")
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if m := banned.FindString(line); m != "" {
				t.Errorf("%scmd.go:%d uses %s — write through cmd.OutOrStdout()/ErrOrStderr() instead (cerne mcp serves it over stdio)", name, i+1, m)
			}
		}
	}
}

// TestMCP_DirFlagDocumented: MCP clients rarely honour a cwd for stdio
// servers, so a global config names the project with --dir: it is visible.
func TestMCP_DirFlagDocumented(t *testing.T) {
	out, err := execCerne(t, "mcp", "--help")
	if err != nil {
		t.Fatalf("mcp --help: %v", err)
	}
	if !strings.Contains(out, "--dir") {
		t.Fatalf("mcp --help does not document --dir:\n%s", out)
	}
}

// TestMCP_HelpNamesEveryTool: the help's tool list is the one the server
// registers, so a new tool cannot ship undocumented (guide did).
func TestMCP_HelpNamesEveryTool(t *testing.T) {
	out, err := execCerne(t, "mcp", "--help")
	if err != nil {
		t.Fatalf("mcp --help: %v", err)
	}
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	if _, err := mcp.NewServer("test", func([]string) ([]byte, error) { return nil, nil }).Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	line := regexp.MustCompile(`Tools: ([a-z, ]+)\.`).FindStringSubmatch(out)
	if line == nil {
		t.Fatalf("mcp --help has no \"Tools: …\" line:\n%s", out)
	}
	listed := map[string]bool{}
	for _, name := range strings.Split(line[1], ", ") {
		listed[name] = true
	}
	for _, tool := range res.Tools {
		if !listed[tool.Name] {
			t.Errorf("mcp --help does not name the %q tool; its list: %s", tool.Name, line[1])
		}
	}
	if len(listed) != len(res.Tools) {
		t.Errorf("mcp --help names %d tools, the server registers %d", len(listed), len(res.Tools))
	}
}

// TestMCP_AskMakesNoModelCall: with a byo-key [llm] pointing at an endpoint
// nobody listens on, the plain `ask` tries the model and reports the failure
// on stderr (control: the config is live; F-0346 falls back to the sources),
// while the MCP ask tool answers with the sources and no such attempt.
func TestMCP_AskMakesNoModelCall(t *testing.T) {
	projDir, _, _ := mcpFixture(t)
	rc := filepath.Join(projDir, ".cernerc")
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, rc, string(data)+"\n[llm]\nmode = \"byo-key\"\nbase_url = \"http://127.0.0.1:1\"\nkey_env = \"CERNE_TEST_KEY\"\n")
	t.Setenv("CERNE_TEST_KEY", "test-key")
	if _, stderr, _ := execCerneSplit(t, "ask", "qual banco usar?", "--dir", projDir); !strings.Contains(stderr, "the model failed") {
		t.Fatalf("control: plain ask did not try the unreachable model; stderr:\n%s", stderr)
	}
	if _, stderr, err := execCerneSplit(t, "ask", "qual banco usar?", "--json", "--dir", projDir); err != nil || strings.Contains(stderr, "model") {
		t.Fatalf("ask --json touched the model: err=%v stderr=%q", err, stderr)
	}

	cs, _, stop := startMCP(t, projDir)
	text := callTool(t, cs, "ask", map[string]any{"question": "qual banco usar?"})
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp: %v", err)
	}
	var got struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil || len(got.Sources) == 0 {
		t.Fatalf("ask tool = %q (err %v), want sources", text, err)
	}
}

func TestMCPProjectFileSnapshot(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(".cernerc", "project_id = \"demo\"\nbackend = \"unused.git\"\n")
	write("Stage.yaml", "sky:\n  sunrise\n")
	write("credentials.json", "private")
	write("scene.bin", "binary\x00data")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "fixture")
	write("Stage.yaml", "uncommitted")
	write("local.yaml", "untracked")
	cs, _, _ := startMCP(t, dir, "--project-files")
	if got := callTool(t, cs, "find_files", map[string]any{"query": "YAML"}); got != "Stage.yaml" {
		t.Fatalf("paths = %q", got)
	}
	if got := callTool(t, cs, "read_file", map[string]any{"path": "Stage.yaml", "start": 2}); got != "2\t  sunrise\n" {
		t.Fatalf("excerpt = %q", got)
	}
	for _, path := range []string{"local.yaml", "credentials.json", ".cernerc", "../Stage.yaml", "scene.bin"} {
		res, err := cs.CallTool(t.Context(), &sdk.CallToolParams{Name: "read_file", Arguments: map[string]any{"path": path}})
		if err != nil || !res.IsError {
			t.Errorf("read %q: %v %v", path, res, err)
		}
	}
}

// TestCodexChatTurnWritesNothing (F-0439 criterion 4): a Codex chat turn gets
// the project-files server and cerne's read tools. Calling every tool of both
// leaves the project, the cache (clone and index) and the origin as they were.
func TestCodexChatTurnWritesNothing(t *testing.T) {
	projDir, origin, repoDir := mcpFixture(t)
	if err := os.WriteFile(filepath.Join(projDir, "Stage.yaml"), []byte("sky: sunrise\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, projDir, "init", "-q")
	gitRun(t, projDir, "add", "Stage.yaml")
	gitRun(t, projDir, "commit", "-qm", "stage")
	cache := os.Getenv("CERNE_CACHE_DIR")
	if cache == "" {
		t.Fatal("setupProject must isolate CERNE_CACHE_DIR")
	}
	proj, cch, clone, orig := treeDigest(t, projDir), treeDigest(t, cache), treeDigest(t, repoDir), gitHead(t, origin)
	headProj := gitHead(t, projDir)

	// A tool that fails early is a no-op the hashes cannot see: every tool the
	// turn is given must run for real (callTool fails on an error result) and
	// answer something.
	called := map[string]bool{}
	cs, _, stop := startMCP(t, projDir)
	for _, c := range allCalls {
		if strings.TrimSpace(callTool(t, cs, c.tool, c.args)) == "" {
			t.Errorf("tool %s %v answered nothing", c.tool, c.args)
		}
		called[c.tool] = true
	}
	for _, name := range mcp.ToolNames {
		if !called[name] {
			t.Errorf("tool %s is given to the turn but never called here", name)
		}
	}
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp: %v", err)
	}
	cs, _, stop = startMCP(t, projDir, "--project-files")
	if got := callTool(t, cs, "find_files", map[string]any{"query": "stage"}); got != "Stage.yaml" {
		t.Fatalf("find_files = %q", got)
	}
	callTool(t, cs, "read_file", map[string]any{"path": "Stage.yaml"})
	if err := stop(); err != nil {
		t.Fatalf("cerne mcp --project-files: %v", err)
	}
	if treeDigest(t, projDir) != proj || gitHead(t, projDir) != headProj {
		t.Error("the project changed")
	}
	if treeDigest(t, cache) != cch {
		t.Error("the cache changed")
	}
	if treeDigest(t, repoDir) != clone || gitHead(t, origin) != orig {
		t.Error("the backend clone or origin changed")
	}
}

func indexFiles(t *testing.T, cache string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(cache, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() && filepath.Base(p) == "index.db" {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestCodexChatColdIndexRebuildsOnlyTheDerivedCache (F-0439 criterion 4): the
// warm case is TestCodexChatTurnWritesNothing (the cache digest is unchanged).
// On a cold index `search` rebuilds index.db — a derived, rebuildable cache,
// accepted — and still leaves the project, the backend clone and the origin
// untouched.
func TestCodexChatColdIndexRebuildsOnlyTheDerivedCache(t *testing.T) {
	projDir, origin, repoDir := mcpFixture(t)
	cache := os.Getenv("CERNE_CACHE_DIR")
	for _, f := range indexFiles(t, cache) {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	proj, clone, orig := treeDigest(t, projDir), treeDigest(t, repoDir), gitHead(t, origin)
	cs, _, stop := startMCP(t, projDir)
	if got := callTool(t, cs, "search", map[string]any{"query": "Postgres"}); !strings.Contains(got, "Postgres") {
		t.Fatalf("search on a cold index = %q", got)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if len(indexFiles(t, cache)) == 0 {
		t.Error("expected the cold search to rebuild index.db")
	}
	if treeDigest(t, projDir) != proj || treeDigest(t, repoDir) != clone || gitHead(t, origin) != orig {
		t.Error("a cold search changed the project, the clone or the origin")
	}
}

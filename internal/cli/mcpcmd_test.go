package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

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

// startMCP runs `cerne mcp --dir projDir` on the REAL process stdin/stdout
// (swapped for pipes), so a byte written to os.Stdout by anything — not just
// the SDK — lands in raw. stop closes the client, waits for the command and
// restores the process streams; it returns the command's error.
func startMCP(t *testing.T, projDir string) (cs *sdk.ClientSession, raw *lockedBuf, stop func() error) {
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
		out, err := execCerne(t, "mcp", "--dir", projDir)
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
	if frames < len(allCalls)+1 { // initialize + one response per call
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

// TestMCP_OutsideProject: criterion 3 — outside a project `cerne mcp` refuses
// before speaking MCP, with the same error and stderr as `cerne status`.
func TestMCP_OutsideProject(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_ROOT", "")
	t.Chdir(t.TempDir())
	sOut, sErr, statusErr := execCerneSplit(t, "status")
	mOut, mErr, mcpErr := execCerneSplit(t, "mcp")
	if !errors.Is(statusErr, project.ErrNoProject) || !errors.Is(mcpErr, project.ErrNoProject) {
		t.Fatalf("want ErrNoProject from both: status=%v mcp=%v", statusErr, mcpErr)
	}
	if mcpErr.Error() != statusErr.Error() || mErr != sErr {
		t.Fatalf("mcp refusal differs from status:\nstatus: %v | %q\nmcp:    %v | %q", statusErr, sErr, mcpErr, mErr)
	}
	if mOut != "" || sOut != "" {
		t.Fatalf("stdout must stay empty: status=%q mcp=%q", sOut, mOut)
	}
}

// TestMCP_NoWriteTools: criterion 4 — every listed tool is read-only, the set is
// exactly the six reading tools, and calling them all leaves the backend (the
// origin and the cached clone: HEAD and files) untouched.
func TestMCP_NoWriteTools(t *testing.T) {
	projDir, origin, repoDir := mcpFixture(t)
	headBefore, cloneBefore, originBefore := gitHead(t, repoDir), treeDigest(t, repoDir), gitHead(t, origin)
	cs, _, stop := startMCP(t, projDir)
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
	if strings.Join(names, ",") != "ask,brief,next,search,show,status" {
		t.Fatalf("tools = %v, want exactly the six reading tools", names)
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

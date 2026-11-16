package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func projectPath(name string) bool {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\r\n\t:") {
		return false
	}
	for part := range strings.SplitSeq(strings.ToLower(name), "/") {
		if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "credentials") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") || part == "id_rsa" || part == "id_ed25519" {
			return false
		}
	}
	return true
}

func projectText(s string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}}
}

type findProjectFiles struct {
	Query string `json:"query" jsonschema:"case-insensitive path fragment"`
}

type readProjectFile struct {
	Path  string `json:"path" jsonschema:"exact relative path returned by find_files"`
	Start int    `json:"start,omitempty" jsonschema:"first line to return (1-based)"`
}

func NewProjectFileServer(version string, tree *object.Tree) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "cerne-project", Version: version}, nil)
	var mu sync.Mutex
	readOnly := &sdk.ToolAnnotations{ReadOnlyHint: true}
	sdk.AddTool(s, &sdk.Tool{Annotations: readOnly, Name: "find_files", Description: "Find up to 100 matching file paths in the project's Git commit. Narrow the query for more specific results."}, func(ctx context.Context, _ *sdk.CallToolRequest, in findProjectFiles) (*sdk.CallToolResult, any, error) {
		mu.Lock()
		defer mu.Unlock()
		w := object.NewTreeWalker(tree, true, nil)
		defer w.Close()
		query := strings.ToLower(in.Query)
		var matches []string
		for len(matches) < 100 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			name, entry, err := w.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			if (entry.Mode.IsRegular() || entry.Mode == filemode.Executable) && strings.Contains(strings.ToLower(name), query) && projectPath(name) {
				matches = append(matches, name)
			}
		}
		return projectText(strings.Join(matches, "\n")), nil, nil
	})
	sdk.AddTool(s, &sdk.Tool{Annotations: readOnly, Name: "read_file", Description: "Read up to 100 numbered lines from a committed UTF-8 project file (up to 4 MiB)."}, func(_ context.Context, _ *sdk.CallToolRequest, in readProjectFile) (*sdk.CallToolResult, any, error) {
		mu.Lock()
		defer mu.Unlock()
		if !projectPath(in.Path) || in.Start < 0 {
			return nil, nil, errors.New("invalid project path or start line")
		}
		entry, err := tree.FindEntry(in.Path)
		if err != nil || !(entry.Mode.IsRegular() || entry.Mode == filemode.Executable) {
			return nil, nil, errors.New("no such regular project file")
		}
		size, err := tree.Size(in.Path)
		if err != nil {
			return nil, nil, err
		}
		if size > 4<<20 {
			return nil, nil, errors.New("file exceeds 4 MiB")
		}
		f, err := tree.File(in.Path)
		if err != nil {
			return nil, nil, err
		}
		body, err := f.Contents()
		if err != nil {
			return nil, nil, err
		}
		if !utf8.ValidString(body) || strings.ContainsRune(body, 0) {
			return nil, nil, errors.New("binary file")
		}
		first := max(in.Start, 1)
		var out strings.Builder
		i := 0
		for line := range strings.SplitSeq(strings.TrimSuffix(body, "\n"), "\n") {
			i++
			if i < first {
				continue
			}
			if i-first >= 100 || out.Len() >= 16000 {
				break
			}
			if len(line) > 2000 {
				line = strings.ToValidUTF8(line[:2000], "") + "…"
			}
			fmt.Fprintf(&out, "%d\t%s\n", i, line)
		}
		return projectText(out.String()), nil, nil
	})
	return s
}

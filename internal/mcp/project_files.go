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
	"github.com/henriquesakata/cerne/internal/domain"
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

type searchProjectFiles struct {
	Query    string `json:"query" jsonschema:"case-insensitive text to look for inside files"`
	PathGlob string `json:"path_glob,omitempty" jsonschema:"optional path glob limiting the files, e.g. game/**"`
}

// walkProjectFiles calls fn for every readable regular file of the commit (the
// same exclusions as read_file) until fn returns false.
func walkProjectFiles(ctx context.Context, tree *object.Tree, fn func(name string) bool) error {
	w := object.NewTreeWalker(tree, true, nil)
	defer w.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, entry, err := w.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if (entry.Mode.IsRegular() || entry.Mode == filemode.Executable) && projectPath(name) && !fn(name) {
			return nil
		}
	}
}

func NewProjectFileServer(version string, tree *object.Tree) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "cerne-project", Version: version}, nil)
	var mu sync.Mutex
	readOnly := &sdk.ToolAnnotations{ReadOnlyHint: true}
	sdk.AddTool(s, &sdk.Tool{Annotations: readOnly, Name: "find_files", Description: "Find up to 100 file paths in the project's Git commit. The query is a case-insensitive path fragment, or a path glob when it has * or ? (e.g. **/*.js, game/**; ** alone lists the tree). Use it to explore the tree when names do not match your words; use search_files to look inside files."}, func(ctx context.Context, _ *sdk.CallToolRequest, in findProjectFiles) (*sdk.CallToolResult, any, error) {
		mu.Lock()
		defer mu.Unlock()
		match := func(name string) bool { return strings.Contains(strings.ToLower(name), strings.ToLower(in.Query)) }
		if strings.ContainsAny(in.Query, "*?") {
			re, err := domain.GlobRegexp(in.Query)
			if err != nil {
				return nil, nil, err
			}
			match = re.MatchString
		}
		var matches []string
		err := walkProjectFiles(ctx, tree, func(name string) bool {
			if match(name) {
				matches = append(matches, name)
			}
			return len(matches) < 100
		})
		if err != nil {
			return nil, nil, err
		}
		return projectText(strings.Join(matches, "\n")), nil, nil
	})
	sdk.AddTool(s, &sdk.Tool{Annotations: readOnly, Name: "search_files", Description: "Search the contents of the project's committed text files for a case-insensitive substring; returns up to 50 path:line:text matches. path_glob (e.g. game/**) narrows the files. Search code terms (stage, scene, backdrop), not only the user's words."}, func(ctx context.Context, _ *sdk.CallToolRequest, in searchProjectFiles) (*sdk.CallToolResult, any, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.TrimSpace(in.Query) == "" {
			return nil, nil, errors.New("query is empty")
		}
		filter := func(string) bool { return true }
		if in.PathGlob != "" {
			if !projectPath(in.PathGlob) {
				return nil, nil, errors.New("invalid path_glob")
			}
			re, err := domain.GlobRegexp(in.PathGlob)
			if err != nil {
				return nil, nil, err
			}
			filter = re.MatchString
		}
		query := strings.ToLower(in.Query)
		var out strings.Builder
		n := 0
		err := walkProjectFiles(ctx, tree, func(name string) bool {
			if !filter(name) {
				return true
			}
			if size, err := tree.Size(name); err != nil || size > 1<<20 {
				return true
			}
			f, err := tree.File(name)
			if err != nil {
				return true
			}
			body, err := f.Contents()
			if err != nil || !utf8.ValidString(body) || strings.ContainsRune(body, 0) {
				return true
			}
			for i, line := range strings.Split(body, "\n") {
				if !strings.Contains(strings.ToLower(line), query) {
					continue
				}
				if n == 50 {
					out.WriteString("(more matches; narrow the query or path_glob)\n")
					return false
				}
				if len(line) > 300 {
					line = strings.ToValidUTF8(line[:300], "") + "…"
				}
				fmt.Fprintf(&out, "%s:%d:%s\n", name, i+1, line)
				n++
			}
			return true
		})
		if err != nil {
			return nil, nil, err
		}
		return projectText(out.String()), nil, nil
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

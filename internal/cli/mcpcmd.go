package cli

import (
	"bytes"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/mcp"
	"github.com/henriquesakata/cerne/internal/version"
)

func newMCPCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the project's state, search and reading as read-only MCP tools over stdio",
		Long: "Speaks MCP on stdin/stdout for an MCP client (register it with command \"cerne\", args [\"mcp\"], cwd = the project).\n" +
			"Tools: status, brief, next, search, show, ask. Each runs the command of the same name with --json and returns its output unchanged; none writes the backend.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve before speaking MCP: outside a project this is the same
			// refusal every other command gives (criterion 3).
			ctx, err := resolvePaths(dir)
			if err != nil {
				return err
			}
			return mcp.NewServer(version.Short(), jsonExec(ctx.projDir)).Run(cmd.Context(), &sdk.StdioTransport{})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

// jsonExec runs one cerne command line in a fresh Root against projDir and
// returns its stdout. The command never touches the process stdout (the MCP
// channel) nor stdin. internal/mcp serializes the calls: Root keeps
// package-level state (lastProject, write notice) that concurrent runs would
// race on.
func jsonExec(projDir string) mcp.Exec {
	return func(args []string) ([]byte, error) {
		root := Root()
		var out, errOut bytes.Buffer
		root.SetIn(strings.NewReader(""))
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(append(append([]string(nil), args...), "--dir", projDir))
		if err := root.Execute(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
}

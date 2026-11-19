package cli

import (
	"bytes"
	"errors"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/mcp"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/version"
)

func newMCPCmd() *cobra.Command {
	var dir string
	var files bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the project's state, search and reading as read-only MCP tools over stdio",
		Long: "Speaks MCP on stdin/stdout for an MCP client. Register it with command \"cerne\" and args [\"mcp\"] in a\n" +
			"project-scoped config (the client starts it in the project root), or args [\"mcp\", \"--dir\", \"/path/to/project\"]\n" +
			"in a global one.\n" +
			"Tools: status, brief, next, search, show, ask, guide. Each runs the command of the same name with --json and returns its output unchanged; none writes the backend.\n" +
			"Outside a cerne project it serves only guide, and its instructions say that cerne init starts a project.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The root --dir gate skips mcp (F-0456): the checks are here, so a
			// --dir still names the project itself and never climbs.
			if files {
				if dir == "" {
					return errors.New("--project-files requires --dir")
				}
				if err := project.RequireConfigAt(dir); err != nil {
					return err
				}
				tree, err := gitback.ProjectTree(dir)
				if err != nil {
					return err
				}
				return mcp.NewProjectFileServer(version.Short(), tree).Run(cmd.Context(), &sdk.StdioTransport{})
			}
			var err error
			if dir != "" {
				err = project.RequireConfigAt(dir)
			}
			var ctx projCtx
			if err == nil {
				ctx, err = resolvePaths(dir)
			}
			if errors.Is(err, project.ErrNoProject) {
				// Outside a project, the offline guide instead of a refusal the
				// client shows as a failed server (F-0456, replacing F-0363's
				// criterion 3). Any other error still refuses.
				return mcp.NewGuideServer(version.Short(), jsonExec("")).Run(cmd.Context(), &sdk.StdioTransport{})
			}
			if err != nil {
				return err
			}
			return mcp.NewServer(version.Short(), jsonExec(ctx.projDir)).Run(cmd.Context(), &sdk.StdioTransport{})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory to serve (defaults to the current directory); use it in a global MCP client config")
	cmd.Flags().BoolVar(&files, "project-files", false, "serve only committed project file lookup and reading")
	return cmd
}

// jsonExec runs one cerne command line in a fresh Root against projDir (none
// when projDir is "") and
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
		args = append([]string(nil), args...)
		if projDir != "" { // "" = no project: only the guide runs, and it needs none
			args = append(args, "--dir", projDir)
		}
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
}

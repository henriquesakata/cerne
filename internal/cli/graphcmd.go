package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
)

func newGraphCmd() *cobra.Command {
	var dir, format string
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Export the entity relationship graph (nodes + edges)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraph(cmd, dir, format)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json or dot")
	return cmd
}

func runGraph(cmd *cobra.Command, dir, format string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	g, err := app.BuildGraph(ctx.repoDir)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(g)
	case "dot":
		return writeGraphDOT(out, g)
	default:
		return fmt.Errorf("invalid format: %q (use json or dot)", format)
	}
}

// writeGraphDOT renders the graph as Graphviz DOT: a node per entity (labeled
// "ID title") and a directed edge per relationship (labeled by type, with the
// supersession reason appended when present).
func writeGraphDOT(w io.Writer, g app.Graph) error {
	var b strings.Builder
	b.WriteString("digraph cerne {\n")
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  %q [label=%q];\n", n.ID, n.ID+" "+n.Title)
	}
	for _, e := range g.Edges {
		label := e.Type
		if e.Reason != "" {
			label += ": " + e.Reason
		}
		fmt.Fprintf(&b, "  %q -> %q [label=%q];\n", e.From, e.To, label)
	}
	b.WriteString("}\n")
	_, err := io.WriteString(w, b.String())
	return err
}

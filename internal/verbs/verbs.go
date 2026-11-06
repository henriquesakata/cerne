// Package verbs is the chat's single source of cerne verbs (F-0323): the
// catalog is derived from the cobra tree, the lifecycle class is an annotation
// on the command definition, and Run executes a verb under the chat's gate.
// No verb list lives here or anywhere else.
package verbs

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	lifecycleKey = "cerne/lifecycle"
	chatToolKey  = "cerne/chat-tool"
)

// Verb is one runnable, user-facing command of the tree.
type Verb struct {
	Path      string            // CommandPath without "cerne ", e.g. "reference new"
	Use       string            // cobra Use, e.g. "new <title>"
	Short     string            // cobra Short
	Flags     map[string]string // non-hidden flag name → pflag type (bool, string, stringArray, int…)
	Lifecycle bool              // marked by MarkLifecycle: never runnable through the chat
	ChatTool  bool              // marked by MarkChatTool: gets a typed chat tool
}

// MarkLifecycle tags cmd as a lifecycle verb: the chat executor refuses it
// before anything runs (F-0323 criterion 4). Returns cmd for `return` sites.
func MarkLifecycle(cmd *cobra.Command) *cobra.Command { return annotate(cmd, lifecycleKey) }

// MarkChatTool tags cmd as a high-traffic chat verb that gets a typed tool.
func MarkChatTool(cmd *cobra.Command) *cobra.Command { return annotate(cmd, chatToolKey) }

func annotate(cmd *cobra.Command, key string) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[key] = "true"
	return cmd
}

// From returns every runnable, non-hidden command under root (hidden subtrees
// excluded), in tree order. A verb added to the tree is in the catalog with no
// other edit (criterion 6).
func From(root *cobra.Command) []Verb {
	var out []Verb
	for _, c := range root.Commands() {
		if c.Hidden {
			continue
		}
		if c.Runnable() {
			out = append(out, verbOf(c))
		}
		out = append(out, From(c)...)
	}
	return out
}

func verbOf(c *cobra.Command) Verb {
	flags := map[string]string{}
	visit := func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" {
			flags[f.Name] = f.Value.Type()
		}
	}
	c.LocalFlags().VisitAll(visit)
	c.InheritedFlags().VisitAll(visit)
	return Verb{
		Path:      strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" "),
		Use:       c.Use,
		Short:     c.Short,
		Flags:     flags,
		Lifecycle: c.Annotations[lifecycleKey] == "true",
		ChatTool:  c.Annotations[chatToolKey] == "true",
	}
}

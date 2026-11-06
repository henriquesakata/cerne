// Package verbs is the chat's single source of cerne verbs (F-0323): the
// catalog is derived from the cobra tree, the lifecycle class is an annotation
// on the command definition, and Run executes a verb under the chat's gate.
// No verb list lives here or anywhere else.
package verbs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	classKey    = "cerne/chat-class"
	chatToolKey = "cerne/chat-tool"
)

// The chat classes of a verb (F-0323 C4). Every user-facing verb carries one on
// its own definition; a verb without one is denied, never offered.
const (
	ClassChat      = "chat"      // offered to the chat agent
	ClassLifecycle = "lifecycle" // a human gate: refused in every mode
	ClassLocal     = "local"     // operational (paths, ports, servers, sync, LLM batches): the human's terminal only
)

// Verb is one runnable, user-facing command of the tree.
type Verb struct {
	Path      string            // CommandPath without "cerne ", e.g. "reference new"
	Use       string            // cobra Use, e.g. "new <title>"
	Short     string            // cobra Short
	Flags     map[string]string // non-hidden flag name → pflag type (bool, string, stringArray, int…)
	Class     string            // ClassChat, ClassLifecycle, ClassLocal, or "" (unclassified: denied)
	Lifecycle bool              // Class == ClassLifecycle
	ChatTool  bool              // marked by MarkChatTool: gets a typed chat tool
}

// MarkChat classifies cmd as offered to the chat agent. Returns cmd so a
// definition or an AddCommand site marks it in place.
func MarkChat(cmd *cobra.Command) *cobra.Command { return annotate(cmd, classKey, ClassChat) }

// MarkLifecycle classifies cmd as a lifecycle verb: the chat executor refuses
// it before anything runs (F-0323 criterion 4).
func MarkLifecycle(cmd *cobra.Command) *cobra.Command { return annotate(cmd, classKey, ClassLifecycle) }

// MarkLocal classifies cmd — and, on a group, every verb under it — as
// operational: never offered, denied when called.
func MarkLocal(cmd *cobra.Command) *cobra.Command { return annotate(cmd, classKey, ClassLocal) }

// MarkChatTool marks a high-traffic chat verb that gets a typed tool; it is
// also its chat classification.
func MarkChatTool(cmd *cobra.Command) *cobra.Command {
	return annotate(MarkChat(cmd), chatToolKey, "true")
}

func annotate(cmd *cobra.Command, key, value string) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[key] = value
	return cmd
}

// classOf is the verb's own class; only ClassLocal flows down from a group, so
// a new leaf under a chat-bearing group stays unclassified (fail closed).
func classOf(c *cobra.Command) string {
	if class := c.Annotations[classKey]; class != "" {
		return class
	}
	for p := c.Parent(); p != nil; p = p.Parent() {
		if p.Annotations[classKey] == ClassLocal {
			return ClassLocal
		}
	}
	return ""
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
	class := classOf(c)
	return Verb{
		Path:      strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" "),
		Use:       c.Use,
		Short:     c.Short,
		Flags:     flags,
		Class:     class,
		Lifecycle: class == ClassLifecycle,
		ChatTool:  c.Annotations[chatToolKey] == "true",
	}
}

var positional = regexp.MustCompile(`^(<([a-z0-9_-]+)>|\[([a-z0-9_-]+)\])$`)

// Positionals are the argument names of the verb's Use, in order: each must be
// a single <x> or [x] (a variadic "..." has no typed-tool shape).
func (v Verb) Positionals() ([]string, error) {
	fields := strings.Fields(v.Use)
	var out []string
	for _, f := range fields[min(1, len(fields)):] {
		m := positional.FindStringSubmatch(f)
		if m == nil {
			return nil, fmt.Errorf("use %q: %q is not a single <arg> or [arg]", v.Use, f)
		}
		out = append(out, m[2]+m[3])
	}
	return out, nil
}
